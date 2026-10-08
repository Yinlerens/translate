package cohere

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Yinlerens/transform/internal/config"
)

func TestTranslateContract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v2/chat" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-provider-key" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("missing provider headers")
		}
		var payload struct {
			Model     string
			Stream    bool
			MaxTokens int `json:"max_tokens"`
			Messages  []struct{ Role, Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload.Model != "north-small-translate-09-2026" || payload.Stream || payload.MaxTokens != 8192 {
			t.Errorf("bad model settings: %+v", payload)
		}
		if len(payload.Messages) != 1 || payload.Messages[0].Role != "user" || payload.Messages[0].Content != "Translate everything that follows into Chinese (Simplified):\n\nHello\n world! " {
			t.Errorf("bad prompt: %+v", payload.Messages)
		}
		_, _ = w.Write([]byte(`{"id":"generation-1","finish_reason":"COMPLETE","message":{"content":[{"type":"thinking","thinking":"hidden"},{"type":"text","text":"你好"},{"type":"text","text":"，世界！\n"}]},"usage":{"tokens":{"input_tokens":12,"output_tokens":8},"billed_units":{"input_tokens":10,"output_tokens":8}}}`))
	}))
	defer srv.Close()
	client := New(config.Config{BaseURL: srv.URL, APIKey: "test-provider-key", Model: "north-small-translate-09-2026", Timeout: time.Second, MaxTokens: 8192}, nil)
	result, err := client.Translate(context.Background(), "Hello\n world! ")
	if err != nil {
		t.Fatal(err)
	}
	if result.TranslatedText != "你好，世界！\n" || result.TargetLanguage != "简体中文" || result.ID != "generation-1" || result.Usage.Tokens.InputTokens != 12 {
		t.Fatalf("bad result: %+v", result)
	}
}

func TestProviderFailures(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		body, code string
		want       int
	}{
		{"rate_limit", 429, "secret", "模型请求限流", 503},
		{"authentication", 401, "secret", "模型认证失败", 502},
		{"forbidden", 403, "secret", "模型认证失败", 502},
		{"invalid_input", 400, "secret", "翻译请求被拒绝", 422},
		{"unavailable", 503, "secret", "模型服务错误", 502},
		{"timeout", 504, "secret", "模型请求超时", 504},
		{"invalid_json", 200, "not JSON", "模型响应无效", 502},
		{"empty_text", 200, `{"finish_reason":"COMPLETE","message":{"content":[{"type":"text","text":" "}]}}`, "模型响应无效", 502},
		{"truncated", 200, `{"finish_reason":"MAX_TOKENS","message":{"content":[{"type":"text","text":"partial"}]}}`, "译文不完整", 502},
		{"error_finish", 200, `{"finish_reason":"ERROR"}`, "模型响应无效", 502},
		{"oversize", 200, strings.Repeat("x", (1<<20)+1), "模型响应无效", 502},
	} {
		t.Run(test.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "10")
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer srv.Close()
			client := New(config.Config{BaseURL: srv.URL, APIKey: "test-provider-key", Timeout: time.Second}, nil)
			_, err := client.Translate(context.Background(), "hello")
			var upstream *Error
			if !errors.As(err, &upstream) || upstream.Status != test.want || upstream.Code != test.code || strings.Contains(upstream.Message, "secret") {
				t.Fatalf("unexpected error: %#v", err)
			}
			if test.status == 429 && upstream.RetryAfter != "10" {
				t.Fatal("Retry-After lost")
			}
		})
	}
}

func TestTimeoutAndCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(200 * time.Millisecond):
		}
	}))
	defer srv.Close()
	client := New(config.Config{BaseURL: srv.URL, APIKey: "test-provider-key", Timeout: 20 * time.Millisecond}, nil)
	_, err := client.Translate(context.Background(), "hello")
	var upstream *Error
	if !errors.As(err, &upstream) || upstream.Status != 504 {
		t.Fatalf("expected timeout, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Translate(ctx, "hello")
	if !errors.As(err, &upstream) || upstream.Status != 499 {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestRedirectDoesNotForwardKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/stolen", 307) }))
	defer srv.Close()
	client := New(config.Config{BaseURL: srv.URL, APIKey: "test-provider-key", Timeout: time.Second}, nil)
	_, err := client.Translate(context.Background(), "hello")
	var upstream *Error
	if !errors.As(err, &upstream) || upstream.Status != 502 {
		t.Fatalf("expected redirect rejection, got %v", err)
	}
}

func TestSupportedLanguages(t *testing.T) {
	seen := make(map[string]bool)
	for _, language := range Languages() {
		if seen[language.Code] {
			t.Fatal("duplicate language")
		}
		seen[language.Code] = true
	}
	if len(seen) != 52 {
		t.Fatalf("expected 52 documented variants, got %d", len(seen))
	}
}
