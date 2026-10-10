package observability

import (
	"fmt"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

// SafeEncoder intentionally discards all raw operational messages and context.
// Only a finite error classification may be persisted or returned to Manager.
type SafeEncoder struct{ zapcore.Encoder }

func (SafeEncoder) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{ID: "caddy.logging.encoders.admin_diagnostics", New: func() caddy.Module { return &SafeEncoder{Encoder: zapcore.NewJSONEncoder(zapcore.EncoderConfig{})} }}
}
func (e *SafeEncoder) Clone() zapcore.Encoder {
	return &SafeEncoder{Encoder: zapcore.NewJSONEncoder(zapcore.EncoderConfig{})}
}
func (e *SafeEncoder) EncodeEntry(entry zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	text := entry.Message
	for _, f := range fields {
		text += " " + f.String
		if f.Interface != nil {
			text += " " + fmt.Sprint(f.Interface)
		}
	}
	class := classify(text)
	if entry.Level >= zapcore.WarnLevel {
		if a := active.Load(); a != nil {
			a.record(Event{Time: entry.Time.UTC().Format(time.RFC3339Nano), Kind: "diagnostic", Class: class})
		}
	}
	safe := zapcore.NewJSONEncoder(zapcore.EncoderConfig{MessageKey: "class", TimeKey: "time", EncodeTime: zapcore.ISO8601TimeEncoder, LevelKey: "level", EncodeLevel: zapcore.LowercaseLevelEncoder})
	entry.Message = class
	entry.LoggerName = ""
	entry.Stack = ""
	entry.Caller = zapcore.EntryCaller{}
	return safe.EncodeEntry(entry, nil)
}
func classify(value string) string {
	v := strings.ToLower(value)
	switch {
	case strings.Contains(v, "429") || strings.Contains(v, "rate limit") || strings.Contains(v, "too many requests"):
		return "acme_rate_limit"
	case strings.Contains(v, "caa"):
		return "caa"
	case strings.Contains(v, "unauthorized") || strings.Contains(v, "authentication") || strings.Contains(v, "invalid access token") || strings.Contains(v, "permission"):
		return "dns_authorization"
	case strings.Contains(v, "dns") || strings.Contains(v, "nxdomain"):
		return "dns"
	default:
		return "acme_unknown"
	}
}
