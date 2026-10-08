package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Yinlerens/transform/internal/cohere"
	"github.com/Yinlerens/transform/internal/config"
	"github.com/Yinlerens/transform/internal/logging"
)

func testHandler(cfg config.Config, output io.Writer) http.Handler {
	return New(cfg, cohere.New(cfg, nil), logging.New(output))
}

func request(handler http.Handler, method, path, body, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Request-ID", "contract-test")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func TestTranslationEndToEnd(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v2/chat" || r.Header.Get("Authorization") != "Bearer provider-key" {
			t.Error("bad upstream contract")
		}
		_, _ = w.Write([]byte(`{"id":"provider-result","finish_reason":"COMPLETE","message":{"content":[{"type":"text","text":"你好！"}]},"usage":{"tokens":{"input_tokens":12,"output_tokens":8},"billed_units":{"input_tokens":10,"output_tokens":8}}}`))
	}))
	defer upstream.Close()
	var logs bytes.Buffer
	handler := testHandler(config.Config{BaseURL: upstream.URL, Model: "north-small-translate-09-2026", APIKey: "provider-key", Version: "test-release", Timeout: time.Second}, &logs)
	response := request(handler, "POST", "/api/translate", `{"文本":"secret-source-text"}`, "application/json; charset=utf-8")
	if response.Code != 200 {
		t.Fatalf("status %d: %s", response.Code, response.Body)
	}
	var result cohere.Result
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.TranslatedText != "你好！" || result.TargetLanguage != "简体中文" || result.Model != "north-small-translate-09-2026" {
		t.Fatalf("bad result: %+v", result)
	}
	if result.FinishReason != "已完成" || result.Usage == nil || result.Usage.Tokens.InputTokens != 12 || result.Usage.BilledUnits.InputTokens != 10 {
		t.Fatalf("用量或完成状态丢失：%+v", result)
	}
	assertChineseFields(t, response.Body.Bytes())
	if calls.Load() != 1 {
		t.Fatalf("expected one upstream call, got %d", calls.Load())
	}
	if response.Header().Get("X-Request-ID") != "contract-test" || response.Header().Get("X-Release-Version") != "test-release" {
		t.Fatal("missing trace/release headers")
	}
	for _, secret := range []string{"secret-source-text", "provider-key", "你好！"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("sensitive data logged: %q", secret)
		}
	}
	metrics := request(handler, "GET", "/metrics", "", "")
	if !strings.Contains(metrics.Body.String(), `foundation_http_requests_total{method="POST",path="/api/translate",status="200"} 1`) {
		t.Fatal("missing translation metric")
	}
}

func TestRejectedRequestsDoNotCallProvider(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer upstream.Close()
	handler := testHandler(config.Config{BaseURL: upstream.URL, APIKey: "provider-key", Timeout: time.Second}, io.Discard)
	for _, test := range []struct {
		name, method, path, body, contentType, code string
		status                                      int
	}{
		{"bad_content_type", "POST", "/api/translate", `{}`, "text/plain", "不支持的内容类型", 415},
		{"bad_json", "POST", "/api/translate", `{`, "application/json", "请求格式错误", 400},
		{"null", "POST", "/api/translate", `null`, "application/json", "文本无效", 400},
		{"empty_text", "POST", "/api/translate", `{"text":"  ","target_language":"fr"}`, "application/json", "文本无效", 400},
		{"unknown_field", "POST", "/api/translate", `{"text":"hello","target_language":"fr","model":"other"}`, "application/json", "请求格式错误", 400},
		{"trailing_json", "POST", "/api/translate", `{"text":"hello","target_language":"fr"} {}`, "application/json", "请求格式错误", 400},
		{"文本过长", "POST", "/api/translate", `{"text":"` + strings.Repeat("中", MaxTextRunes+1) + `","target_language":"fr"}`, "application/json", "文本过长", 413},
		{"请求体过大", "POST", "/api/translate", `{"text":"` + strings.Repeat("x", maxBodyBytes) + `","target_language":"fr"}`, "application/json", "请求体过大", 413},
		{"wrong_method", "GET", "/api/translate", "", "", "请求方法不允许", 405},
		{"wrong_route", "POST", "/missing", "", "", "接口不存在", 404},
		{"conflicting_text", "POST", "/api/translate", `{"文本":"你好","text":"hello"}`, "application/json", "文本字段冲突", 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := request(handler, test.method, test.path, test.body, test.contentType)
			if response.Code != test.status || !strings.Contains(response.Body.String(), `"类型":"`+test.code+`"`) {
				t.Fatalf("status %d: %s", response.Code, response.Body)
			}
			assertChineseFields(t, response.Body.Bytes())
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("rejected requests reached provider %d times", calls.Load())
	}
}

func TestAutoSourceAndFixedChineseTarget(t *testing.T) {
	for _, test := range []struct{ name, text, target string }{
		{"english", "Hello, world!", ""},
		{"japanese", "こんにちは、世界！", ""},
		{"french", "Bonjour le monde !", ""},
		{"mixed", "Hello\nこんにちは\nBonjour", ""},
		{"legacy_target", "Hello, world!", "fr"},
		{"legacy_traditional_target", "Hello, world!", "zh-TW"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var payload struct {
					Messages []struct{ Role, Content string }
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if len(payload.Messages) != 1 || payload.Messages[0].Role != "user" || payload.Messages[0].Content != "Translate everything that follows into Chinese (Simplified):\n\n"+test.text {
					t.Errorf("unexpected translation prompt: %+v", payload.Messages)
				}
				_, _ = w.Write([]byte(`{"id":"fixed-target","finish_reason":"COMPLETE","message":{"content":[{"type":"text","text":"你好，世界！"}]}}`))
			}))
			defer upstream.Close()
			handler := testHandler(config.Config{BaseURL: upstream.URL, APIKey: "provider-key", Timeout: time.Second}, io.Discard)
			input := map[string]string{"文本": test.text}
			if test.name == "legacy_target" {
				input = map[string]string{"text": test.text}
			}
			if test.target != "" {
				input["target_language"] = test.target
			}
			body, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			response := request(handler, "POST", "/api/translate", string(body), "application/json")
			var result cohere.Result
			if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.TargetLanguage != "简体中文" {
				t.Fatalf("bad fixed-target response: %d %s", response.Code, response.Body)
			}
			if calls.Load() != 1 {
				t.Fatalf("expected one translation call, got %d", calls.Load())
			}
		})
	}
}

func TestHealthAndNotConfigured(t *testing.T) {
	handler := testHandler(config.Config{AppName: "transform", Model: "north-small-translate-09-2026", Version: "test"}, io.Discard)
	for path, status := range map[string]int{"/healthz": 200, "/readyz": 503, "/api": 200, "/api/languages": 200, "/metrics": 200} {
		response := request(handler, "GET", path, "", "")
		if response.Code != status {
			t.Errorf("%s status: %d", path, response.Code)
		}
		if path != "/metrics" {
			assertChineseFields(t, response.Body.Bytes())
		}
	}
	response := request(handler, "POST", "/api/translate", `{"text":"hello","target_language":"French"}`, "application/json")
	if response.Code != 503 || !strings.Contains(response.Body.String(), "未配置") {
		t.Fatalf("unexpected unconfigured response: %s", response.Body)
	}
	req := httptest.NewRequest("GET", "/healthz", nil)
	req.Header.Set("X-Request-ID", strings.Repeat("x", 129))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if len(recorder.Header().Get("X-Request-ID")) != 32 {
		t.Fatal("unsafe request ID was not replaced")
	}
}

func assertChineseFields(t *testing.T, data []byte) {
	t.Helper()
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	var check func(any)
	check = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			for key, child := range value {
				for _, char := range key {
					if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' {
						t.Errorf("响应仍有英文字段：%s", key)
						break
					}
				}
				check(child)
			}
		case []any:
			for _, child := range value {
				check(child)
			}
		}
	}
	check(decoded)
}

func TestTranslationWithoutAuthenticationOverHTTP(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer provider-key" {
			t.Error("模型调用缺少 Cohere 密钥")
		}
		_, _ = w.Write([]byte(`{"id":"no-auth","finish_reason":"COMPLETE","message":{"content":[{"type":"text","text":"你好！"}]}}`))
	}))
	defer upstream.Close()
	server := httptest.NewServer(testHandler(config.Config{BaseURL: upstream.URL, APIKey: "provider-key", Timeout: time.Second}, io.Discard))
	defer server.Close()
	response, err := server.Client().Post(server.URL+"/api/translate", "application/json", strings.NewReader(`{"文本":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || !strings.Contains(string(body), `"译文":"你好！"`) {
		t.Fatalf("无需认证的翻译响应不正确：%d %s", response.StatusCode, body)
	}
	if response.Header.Get("WWW-Authenticate") != "" {
		t.Fatal("响应仍包含认证要求")
	}
	assertChineseFields(t, body)
}

func TestUpstreamFailureResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "15")
		w.WriteHeader(429)
		_, _ = w.Write([]byte("private-upstream-error"))
	}))
	defer upstream.Close()
	handler := testHandler(config.Config{BaseURL: upstream.URL, APIKey: "provider-key", Timeout: time.Second}, io.Discard)
	response := request(handler, "POST", "/api/translate", `{"text":"hello","target_language":"fr"}`, "application/json")
	if response.Code != 503 || response.Header().Get("Retry-After") != "15" || !strings.Contains(response.Body.String(), "模型请求限流") {
		t.Fatalf("bad failure response: %d %s", response.Code, response.Body)
	}
	if strings.Contains(response.Body.String(), "private-upstream-error") {
		t.Fatal("upstream error leaked")
	}
}
