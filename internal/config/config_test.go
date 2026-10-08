package config

import (
	"testing"
	"time"
)

func cleanEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"HTTP_ADDR", "APP_NAME", "APP_ENVIRONMENT", "RELEASE_VERSION", "WORKLOAD_ROLE", "COHERE_API_KEY", "COHERE_BASE_URL", "COHERE_MODEL", "COHERE_TIMEOUT", "COHERE_MAX_TOKENS"} {
		t.Setenv(key, "")
	}
}

func TestDefaults(t *testing.T) {
	cleanEnv(t)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Model != "north-small-translate-1-0" || c.BaseURL != "https://api.cohere.com" || c.Timeout != time.Minute || c.MaxTokens != 8192 {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestInvalidConfiguration(t *testing.T) {
	for _, test := range []struct{ key, value string }{
		{"COHERE_BASE_URL", "not-a-url"}, {"COHERE_BASE_URL", "ftp://example.com"}, {"COHERE_BASE_URL", "http://example.com"},
		{"COHERE_BASE_URL", "https://user:secret@example.com"}, {"COHERE_BASE_URL", "https://example.com?q=secret"},
		{"COHERE_TIMEOUT", "0s"}, {"COHERE_TIMEOUT", "61s"}, {"COHERE_TIMEOUT", "bad"},
		{"COHERE_MAX_TOKENS", "0"}, {"COHERE_MAX_TOKENS", "16385"}, {"COHERE_MAX_TOKENS", "bad"},
	} {
		t.Run(test.key+"/"+test.value, func(t *testing.T) {
			cleanEnv(t)
			t.Setenv(test.key, test.value)
			if _, err := Load(); err == nil {
				t.Fatal("expected invalid configuration")
			}
		})
	}
}

func TestProductionConfiguration(t *testing.T) {
	cleanEnv(t)
	t.Setenv("APP_ENVIRONMENT", "production")
	if _, err := Load(); err != nil {
		t.Fatalf("生产环境配置加载失败：%v", err)
	}
}
