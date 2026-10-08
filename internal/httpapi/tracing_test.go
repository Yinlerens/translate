package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Yinlerens/transform/internal/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestTraceHeaderCorrelatesResponseAndLog(t *testing.T) {
	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	otel.SetTracerProvider(sdktrace.NewTracerProvider())
	otel.SetTextMapPropagator(propagation.TraceContext{})
	defer otel.SetTracerProvider(previousProvider)
	defer otel.SetTextMapPropagator(previousPropagator)

	const incomingID = "0123456789abcdef0123456789abcdef"
	for _, test := range []struct {
		name, method, path, parent string
		status                     int
	}{
		{"success", "GET", "/api", "00-" + incomingID + "-0123456789abcdef-01", 200},
		{"error", "POST", "/api/translate", "00-" + incomingID + "-0123456789abcdef-01", 400},
		{"invalid_context", "GET", "/api", "invalid-parent", 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			handler := testHandler(config.Config{Version: "trace-test"}, &logs)
			r := httptest.NewRequest(test.method, test.path, strings.NewReader(`{"文本":""}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Traceparent", test.parent)
			r.Header.Set("X-Request-ID", "trace-header-test")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != test.status {
				t.Fatalf("unexpected status %d", w.Code)
			}
			id := w.Header().Get("X-Trace-ID")
			parsed, err := trace.TraceIDFromHex(id)
			if err != nil || !parsed.IsValid() {
				t.Fatalf("invalid response trace ID %q", id)
			}
			if test.name != "invalid_context" && id != incomingID {
				t.Fatalf("incoming trace context was not preserved: %s", id)
			}
			if w.Header().Get("X-Request-ID") != "trace-header-test" {
				t.Fatal("request ID changed")
			}
			var line map[string]any
			if err := json.Unmarshal(logs.Bytes(), &line); err != nil {
				t.Fatal(err)
			}
			if line["链路编号"] != id {
				t.Fatal("response and log trace IDs differ")
			}
		})
	}
}
