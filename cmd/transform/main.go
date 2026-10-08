package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Yinlerens/transform/internal/cohere"
	"github.com/Yinlerens/transform/internal/config"
	"github.com/Yinlerens/transform/internal/httpapi"
	"github.com/Yinlerens/transform/internal/logging"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func main() {
	logger := logging.New(os.Stdout)
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(_ error) {
		logger.Error("遥测处理失败", "说明", "请检查遥测收集器连接和配置")
	}))
	if err := run(logger); err != nil {
		logger.Error("服务运行失败", "错误", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" || os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != "" {
		exporter, err := otlptracehttp.New(ctx)
		if err != nil {
			return errors.New("遥测导出器初始化失败，请检查遥测地址配置")
		}
		provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithResource(resource.NewSchemaless(
			attribute.String("service.name", cfg.AppName+"-"+cfg.Role), attribute.String("service.version", cfg.Version),
			attribute.String("deployment.environment.name", cfg.Environment))))
		otel.SetTracerProvider(provider)
		defer func() {
			flush, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = provider.Shutdown(flush)
		}()
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = 32
	defer transport.CloseIdleConnections()
	client := cohere.New(cfg, otelhttp.NewTransport(transport))
	server := &http.Server{Addr: cfg.Addr, Handler: httpapi.New(cfg, client, logger),
		ErrorLog:          log.New(logging.HTTPErrorWriter{Logger: logger}, "", 0),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 75 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	if !client.Ready() {
		logger.Warn("翻译服务尚未配置", "说明", "请设置 COHERE_API_KEY 以启用翻译接口和就绪检查")
	}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	logger.Info("服务已启动", "监听地址", cfg.Addr, "应用", cfg.AppLabel(), "模型", cfg.Model, "版本", cfg.VersionLabel())
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("接口服务监听失败（地址：%s），请检查监听地址和端口占用", cfg.Addr)
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 75*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return errors.New("服务关闭失败，未能在宽限期内完成请求")
		}
	}
	return nil
}
