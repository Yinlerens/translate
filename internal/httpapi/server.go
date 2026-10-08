package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Yinlerens/transform/internal/cohere"
	"github.com/Yinlerens/transform/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"
)

const MaxTextRunes = 8000
const maxBodyBytes = 65536

type Server struct {
	cfg    config.Config
	client *cohere.Client
}

func New(cfg config.Config, client *cohere.Client, logger *slog.Logger) http.Handler {
	s := &Server{cfg: cfg, client: client}
	registry := prometheus.NewRegistry()
	count := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "foundation_http_requests_total", Help: "请求数量"}, []string{"method", "path", "status"})
	latency := prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "foundation_http_request_seconds", Help: "请求耗时（秒）", Buckets: prometheus.DefBuckets}, []string{"path"})
	registry.MustRegister(count, latency, prometheus.NewGoCollector(), prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"状态": "正常", "角色": cfg.RoleLabel(), "版本": cfg.VersionLabel()})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if !client.Ready() {
			writeError(w, r, 503, "未配置", "尚未配置 COHERE_API_KEY")
			return
		}
		writeJSON(w, 200, map[string]string{"状态": "已就绪"})
	})
	mux.HandleFunc("GET /api", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"服务": cfg.AppLabel(), "版本": cfg.VersionLabel(), "环境": cfg.EnvironmentLabel(), "模型": cfg.Model, "目标语言": cohere.TargetLanguageName})
	})
	mux.HandleFunc("GET /api/languages", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"支持语言": cohere.Languages(), "目标语言": cohere.TargetLanguageName})
	})
	mux.HandleFunc("POST /api/translate", s.translate)
	mux.Handle("GET /metrics", promhttp.HandlerFor(chineseMetrics(registry), promhttp.HandlerOpts{ErrorHandling: promhttp.PanicOnError}))
	// Make method and route failures follow the same JSON error contract.
	router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed := "GET, HEAD"
		switch r.URL.Path {
		case "/api/translate":
			allowed = "POST"
		case "/healthz", "/readyz", "/api", "/api/languages", "/metrics":
		default:
			writeError(w, r, 404, "接口不存在", "请求的接口不存在")
			return
		}
		if (allowed == "POST" && r.Method != "POST") || (allowed != "POST" && r.Method != "GET" && r.Method != "HEAD") {
			w.Header().Set("Allow", allowed)
			writeError(w, r, 405, "请求方法不允许", "该接口不支持此请求方法")
			return
		}
		mux.ServeHTTP(w, r)
	})
	observed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		path := routeLabel(r.URL.Path)
		method := r.Method
		switch method {
		case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
		default:
			method = "other"
		}
		id := r.Header.Get("X-Request-ID")
		if !requestIDPattern.MatchString(id) {
			id = newRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("X-Release-Version", cfg.Version)
		if sc := trace.SpanContextFromContext(r.Context()); sc.IsValid() {
			w.Header().Set("X-Trace-ID", sc.TraceID().String())
		}
		w.Header().Set("Cache-Control", "no-store")
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if recovered := recover(); recovered != nil {
				writeError(rw, r, 500, "服务内部错误", "服务发生内部错误")
			}
			duration := time.Since(start).Seconds()
			count.WithLabelValues(method, path, strconv.Itoa(rw.status)).Inc()
			latency.WithLabelValues(path).Observe(duration)
			sc := trace.SpanContextFromContext(r.Context())
			logPath := path
			if logPath == "other" {
				logPath = "其他"
			}
			logMethod := method
			if logMethod == "other" {
				logMethod = "其他"
			}
			logger.Info("请求处理完成", "请求编号", id, "链路编号", sc.TraceID().String(), "应用", cfg.AppLabel(),
				"环境", cfg.EnvironmentLabel(), "版本", cfg.VersionLabel(), "请求方法", logMethod, "请求路径", logPath, "状态码", rw.status, "耗时秒", duration)
		}()
		router.ServeHTTP(rw, r)
	})
	return otelhttp.NewHandler(observed, "http.request", otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string { return routeLabel(r.URL.Path) }))
}

func (s *Server) translate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	defer func() {
		// Drain a bounded body before closing it, including invalid requests.
		// Otherwise a real TCP client can receive a reset instead of the JSON error.
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
	}()
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, r, 415, "不支持的内容类型", "请求内容类型必须为 application/json")
		return
	}
	var input struct {
		Text       *string `json:"文本"`
		LegacyText *string `json:"text"`
		// Accepted for existing clients; the translation target always stays zh-CN.
		TargetLanguage        string `json:"target_language"`
		ChineseTargetLanguage string `json:"目标语言"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		decodeError(w, r, err)
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = errors.New("请求体包含多余内容")
		}
		decodeError(w, r, err)
		return
	}
	if input.Text != nil && input.LegacyText != nil && *input.Text != *input.LegacyText {
		writeError(w, r, 400, "文本字段冲突", "文本与旧版 text 字段的内容不一致")
		return
	}
	text := ""
	if input.Text != nil {
		text = *input.Text
	} else if input.LegacyText != nil {
		text = *input.LegacyText
	}
	if strings.TrimSpace(text) == "" {
		writeError(w, r, 400, "文本无效", "文本不能为空")
		return
	}
	if utf8.RuneCountInString(text) > MaxTextRunes {
		writeError(w, r, 413, "文本过长", "文本最多包含 8000 个字符")
		return
	}
	result, err := s.client.Translate(r.Context(), text)
	if err != nil {
		var upstream *cohere.Error
		if errors.As(err, &upstream) {
			if upstream.RetryAfter != "" {
				w.Header().Set("Retry-After", upstream.RetryAfter)
			}
			writeError(w, r, upstream.Status, upstream.Code, upstream.Message)
		} else {
			writeError(w, r, 502, "模型服务错误", "模型服务调用失败")
		}
		return
	}
	writeJSON(w, 200, result)
}

func decodeError(w http.ResponseWriter, r *http.Request, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, r, 413, "请求体过大", "请求体不能超过 65536 字节")
		return
	}
	writeError(w, r, 400, "请求格式错误", "请求体必须为一个包含“文本”字段的 JSON 对象")
}

func writeError(w http.ResponseWriter, _ *http.Request, status int, code, message string) {
	writeJSON(w, status, map[string]any{"错误": map[string]string{"类型": code, "说明": message, "请求编号": w.Header().Get("X-Request-ID")}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func routeLabel(path string) string {
	switch path {
	case "/healthz", "/readyz", "/api", "/api/languages", "/api/translate", "/metrics":
		return path
	}
	return "other"
}

var requestIDPattern = regexp.MustCompile(`^[a-zA-Z0-9._:-]{1,128}$`)

func newRequestID() string {
	var id [16]byte
	_, _ = rand.Read(id[:])
	return hex.EncodeToString(id[:])
}

type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *responseWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.status = status
		w.wroteHeader = true
		w.ResponseWriter.WriteHeader(status)
	}
}

func (w *responseWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
