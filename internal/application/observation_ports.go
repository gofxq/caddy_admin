package application

import (
	"context"
	"github.com/gofxq/caddy_admin/internal/domain"
	"time"
)

type ObservationSource interface {
	Metrics(context.Context) (domain.MetricSnapshot, error)
	Logs(context.Context, string, uint64) (domain.LogBatch, error)
}
type ObservationStore interface {
	Write(context.Context, time.Time, time.Time, []domain.ObservationDelta, bool) error
	Traffic(context.Context, domain.TrafficQuery) (domain.Traffic, error)
	AppendLogs(context.Context, domain.LogBatch) error
	LogCursor(context.Context) (domain.LogBatch, error)
	Logs(context.Context, domain.LogQuery) ([]domain.AccessEvent, error)
	Alerts(context.Context) ([]domain.Alert, error)
	SaveAlert(context.Context, domain.Alert) error
	Acknowledge(context.Context, string) error
	Close() error
}
type DiagnosticProbe interface {
	Diagnose(context.Context, domain.ManagedSettings, domain.ManagedDomain) []domain.DiagnosticCheck
}
