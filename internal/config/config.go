package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr, AppName, Environment, Version, Role string
	APIKey, BaseURL, Model                    string
	Timeout                                   time.Duration
	MaxTokens                                 int
}

func Load() (Config, error) {
	c := Config{
		Addr: env("HTTP_ADDR", ":8080"), AppName: env("APP_NAME", "transform"),
		Environment: env("APP_ENVIRONMENT", "development"), Version: env("RELEASE_VERSION", "development"),
		Role: env("WORKLOAD_ROLE", "api"), APIKey: strings.TrimSpace(os.Getenv("COHERE_API_KEY")),
		BaseURL: strings.TrimRight(env("COHERE_BASE_URL", "https://api.cohere.com"), "/"),
		Model:   env("COHERE_MODEL", "north-small-translate-1-0"),
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return c, fmt.Errorf("COHERE_BASE_URL 必须为 HTTP(S) 基础地址，且不能包含认证信息、查询参数或片段标识")
	}
	// Plain HTTP is useful for local mock servers, but must not carry a key remotely.
	if u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
		return c, fmt.Errorf("COHERE_BASE_URL 在非本机环境下必须使用 HTTPS")
	}
	c.Timeout, err = time.ParseDuration(env("COHERE_TIMEOUT", "60s"))
	if err != nil || c.Timeout <= 0 || c.Timeout > 60*time.Second {
		return c, fmt.Errorf("COHERE_TIMEOUT 必须为大于 0 且不超过 60 秒的时长")
	}
	c.MaxTokens, err = strconv.Atoi(env("COHERE_MAX_TOKENS", "8192"))
	if err != nil || c.MaxTokens < 1 || c.MaxTokens > 16384 {
		return c, fmt.Errorf("COHERE_MAX_TOKENS 必须为 1 至 16384 之间的整数")
	}
	return c, nil
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
