package logging

import (
	"io"
	"log/slog"
	"strings"
)

func New(output io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
		switch attr.Key {
		case slog.TimeKey:
			attr.Key = "时间"
		case slog.MessageKey:
			attr.Key = "消息"
		case slog.LevelKey:
			attr.Key = "级别"
			level := attr.Value.Any().(slog.Level)
			label := "信息"
			switch {
			case level >= slog.LevelError:
				label = "错误"
			case level >= slog.LevelWarn:
				label = "警告"
			case level < slog.LevelInfo:
				label = "调试"
			}
			attr.Value = slog.StringValue(label)
		}
		return attr
	}}))
}

// net/http uses a separate logger for connection errors and internal panics.
// Route it through the same Chinese logger without exposing raw request data.
type HTTPErrorWriter struct{ Logger *slog.Logger }

func (w HTTPErrorWriter) Write(data []byte) (int, error) {
	typeName := "底层服务错误"
	switch {
	case strings.Contains(string(data), "Accept error"):
		typeName = "连接接收错误"
	case strings.Contains(string(data), "TLS handshake error"):
		typeName = "加密连接错误"
	case strings.Contains(string(data), "panic serving"):
		typeName = "请求处理异常"
	}
	w.Logger.Error("接口服务内部错误", "类型", typeName)
	return len(data), nil
}
