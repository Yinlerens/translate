package cohere

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/Yinlerens/transform/internal/config"
)

type Error struct {
	Status                    int
	Code, Message, RetryAfter string
}

func (e *Error) Error() string { return e.Code }

type Usage struct {
	Tokens      *TokenUsage `json:"词元数量,omitempty"`
	BilledUnits *TokenUsage `json:"计费用量,omitempty"`
}

type TokenUsage struct {
	InputTokens  float64 `json:"输入词元"`
	OutputTokens float64 `json:"输出词元"`
}

type Result struct {
	ID             string `json:"生成编号"`
	TranslatedText string `json:"译文"`
	TargetLanguage string `json:"目标语言"`
	Model          string `json:"模型"`
	FinishReason   string `json:"完成状态"`
	Usage          *Usage `json:"用量,omitempty"`
}

type Client struct {
	cfg  config.Config
	http *http.Client
}

const TargetLanguageName = "简体中文"

func New(cfg config.Config, transport http.RoundTripper) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: cfg.Timeout, Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *Client) Ready() bool { return c.cfg.APIKey != "" }

func (c *Client) Translate(ctx context.Context, text string) (Result, error) {
	if !c.Ready() {
		return Result{}, &Error{Status: 503, Code: "未配置", Message: "尚未配置 COHERE_API_KEY"}
	}
	// Keep the prompt format recommended in the model's official getting-started guide.
	payload := struct {
		Model     string              `json:"model"`
		Stream    bool                `json:"stream"`
		Messages  []map[string]string `json:"messages"`
		MaxTokens int                 `json:"max_tokens"`
	}{c.cfg.Model, false, []map[string]string{{"role": "user", "content": "Translate everything that follows into Chinese (Simplified):\n\n" + text}}, c.cfg.MaxTokens}
	body, err := json.Marshal(payload)
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/v2/chat", bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Client-Name", c.cfg.AppName)
	resp, err := c.http.Do(req)
	if err != nil {
		return Result{}, requestFailure(err)
	}
	defer resp.Body.Close()
	// Never return the upstream body: it may contain source text or credentials.
	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case 429:
			retry := resp.Header.Get("Retry-After")
			if len(retry) > 128 {
				retry = ""
			}
			return Result{}, &Error{Status: 503, Code: "模型请求限流", Message: "模型服务请求频率已达到限制，请稍后重试", RetryAfter: retry}
		case 400, 422:
			return Result{}, &Error{Status: 422, Code: "翻译请求被拒绝", Message: "模型服务拒绝了输入，请尝试缩短文本"}
		case 401, 403, 498:
			return Result{}, &Error{Status: 502, Code: "模型认证失败", Message: "模型服务认证失败，请检查密钥和账号权限"}
		case 504:
			return Result{}, &Error{Status: 504, Code: "模型请求超时", Message: "模型服务响应超时，请稍后重试"}
		default:
			return Result{}, &Error{Status: 502, Code: "模型服务错误", Message: "模型服务返回错误，请稍后重试"}
		}
	}
	const maxResponse = 1 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return Result{}, requestFailure(err)
	}
	if len(data) > maxResponse {
		return Result{}, invalidResponse()
	}
	var response struct {
		ID           string `json:"id"`
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content []struct{ Type, Text string } `json:"content"`
		} `json:"message"`
		Usage *providerUsage `json:"usage"`
	}
	if json.Unmarshal(data, &response) != nil {
		return Result{}, invalidResponse()
	}
	if response.FinishReason == "MAX_TOKENS" {
		return Result{}, &Error{Status: 502, Code: "译文不完整", Message: "译文超出输出词元限制，请尝试缩短文本"}
	}
	if response.FinishReason != "COMPLETE" {
		return Result{}, invalidResponse()
	}
	var translated strings.Builder
	for _, block := range response.Message.Content {
		if block.Type == "text" {
			translated.WriteString(block.Text)
		}
	}
	if strings.TrimSpace(translated.String()) == "" {
		return Result{}, invalidResponse()
	}
	return Result{ID: response.ID, TranslatedText: translated.String(), TargetLanguage: TargetLanguageName,
		Model: c.cfg.Model, FinishReason: "已完成", Usage: response.Usage.localized()}, nil
}

func invalidResponse() error {
	return &Error{Status: 502, Code: "模型响应无效", Message: "模型服务返回了无效响应"}
}

func requestFailure(err error) error {
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return &Error{Status: 504, Code: "模型请求超时", Message: "模型服务响应超时，请稍后重试"}
	}
	if errors.Is(err, context.Canceled) {
		return &Error{Status: 499, Code: "请求已取消", Message: "客户端已取消请求"}
	}
	return &Error{Status: 502, Code: "模型服务不可用", Message: "无法连接模型服务，请稍后重试"}
}

// Cohere's wire format stays separate from our Chinese response format.
type providerTokenUsage struct {
	InputTokens  float64 `json:"input_tokens"`
	OutputTokens float64 `json:"output_tokens"`
}

type providerUsage struct {
	Tokens      *providerTokenUsage `json:"tokens"`
	BilledUnits *providerTokenUsage `json:"billed_units"`
}

func (usage *providerUsage) localized() *Usage {
	if usage == nil {
		return nil
	}
	convert := func(tokens *providerTokenUsage) *TokenUsage {
		if tokens == nil {
			return nil
		}
		return &TokenUsage{InputTokens: tokens.InputTokens, OutputTokens: tokens.OutputTokens}
	}
	return &Usage{Tokens: convert(usage.Tokens), BilledUnits: convert(usage.BilledUnits)}
}
