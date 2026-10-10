package application

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/gofxq/caddy_admin/internal/domain"
)

type Observer struct {
	historyLoaded bool
	cursorLoaded  bool
	service       *Service
	source        ObservationSource
	store         ObservationStore
	diagnostics   DiagnosticProbe
	mu            sync.Mutex
	collectMu     sync.Mutex
	status        domain.ObservationStatus
	previous      domain.MetricSnapshot
	previousTime  time.Time
	deploymentID  string
	logEpoch      string
	logAfter      uint64
	known         map[string]bool
	diagnosticsMu sync.Mutex
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	probeFailures map[string]int
	probeVersion  map[string]string
	probeIndex    int
}

func NewObserver(s *Service, source ObservationSource, store ObservationStore, diagnostics DiagnosticProbe) *Observer {
	return &Observer{service: s, source: source, store: store, diagnostics: diagnostics, known: map[string]bool{}, probeFailures: map[string]int{}, probeVersion: map[string]string{}, status: domain.ObservationStatus{State: "disabled", Message: "观测尚未启用；请在设置中保存草稿并发布", LogsState: "disabled", IntervalSeconds: 15, External: s.externalCaddy()}}
}
func (s *Service) StartObservability(ctx context.Context, source ObservationSource, store ObservationStore, probe DiagnosticProbe) {
	s.observer = NewObserver(s, source, store, probe)
	s.observer.Start(ctx)
}
func (o *Observer) Start(ctx context.Context) {
	life, cancel := context.WithCancel(ctx)
	o.cancel = cancel
	o.wg.Add(2)
	go func() {
		defer o.wg.Done()
		initial, cancel := context.WithTimeout(life, 10*time.Second)
		o.Collect(initial, time.Now())
		cancel()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-life.Done():
				return
			case now := <-ticker.C:
				ctx, cancel := context.WithTimeout(life, 10*time.Second)
				o.Collect(ctx, now)
				cancel()
			}
		}
	}()
	go func() {
		defer o.wg.Done()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-life.Done():
				return
			case now := <-ticker.C:
				ctx, cancel := context.WithTimeout(life, 20*time.Second)
				o.Evaluate(ctx, now)
				cancel()
			}
		}
	}()
}
func (s *Service) StopObservability() {
	if s.observer != nil {
		s.observer.Stop()
	}
}
func (o *Observer) Stop() {
	if o.cancel != nil {
		o.cancel()
		o.wg.Wait()
	}
	if o.store != nil {
		o.store.Close()
	}
}
func (o *Observer) Status() domain.ObservationStatus {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.status
}
func (o *Observer) change(fn func(*domain.ObservationStatus)) {
	o.mu.Lock()
	defer o.mu.Unlock()
	fn(&o.status)
}
func (o *Observer) runtime(ctx context.Context) (domain.Deployment, error) {
	d, err := o.service.repository.Latest(ctx)
	if err != nil {
		return d, err
	}
	if _, err = o.service.repository.Pending(ctx); err == nil {
		return d, fmt.Errorf("publish pending")
	} else if !domain.IsMissing(err) {
		return d, err
	}
	raw, err := o.service.caddy.Read(ctx)
	if err != nil || domain.Fingerprint(raw) != d.Hash {
		return d, fmt.Errorf("runtime unverified")
	}
	return d, nil
}
func (o *Observer) fail(now time.Time, message string) {
	o.previous = domain.MetricSnapshot{}
	o.previousTime = now
	o.change(func(s *domain.ObservationStatus) { s.State = "unavailable"; s.Message = message })
}
func (o *Observer) Collect(ctx context.Context, now time.Time) {
	o.collectMu.Lock()
	defer o.collectMu.Unlock()
	o.change(func(s *domain.ObservationStatus) { s.LastAttempt = now.UTC().Format(time.RFC3339Nano) })
	if o.store == nil || o.source == nil {
		o.fail(now, "观测存储不可用；管理与代理仍可使用，检查数据目录空间和权限后重启 Manager")
		return
	}
	latest, err := o.service.repository.Latest(ctx)
	if err != nil {
		if domain.IsMissing(err) {
			o.change(func(s *domain.ObservationStatus) { s.State = "disabled"; s.Message = "尚无已发布观测配置" })
			o.previous = domain.MetricSnapshot{}
			return
		}
		o.fail(now, "无法读取观测对应的已发布版本")
		return
	}
	settings := latest.Settings
	if !settings.MetricsEnabled && !settings.AccessLogsEnabled {
		o.previous = domain.MetricSnapshot{}
		o.change(func(s *domain.ObservationStatus) {
			s.State = "disabled"
			s.LogsState = "disabled"
			s.Message = "观测已关闭；已有历史仍可查询"
		})
		return
	}
	before, err := o.runtime(ctx)
	if err != nil {
		o.fail(now, "运行配置不可达、存在漂移或发布待核对；暂停归属统计并保留历史")
		return
	}
	if len(o.known) > 2000 {
		o.known = map[string]bool{}
		o.change(func(s *domain.ObservationStatus) { s.LogGap = true })
	}
	for _, item := range before.Services {
		if item.Enabled {
			o.known[item.ID+"/"+item.Hostname] = true
		}
	}
	if settings.MetricsEnabled {
		snapshot, err := o.source.Metrics(ctx)
		if err != nil {
			o.fail(now, "指标读取失败；检查 Caddy 观测模块和运行配置，采集将自动重试")
		} else {
			after, err := o.runtime(ctx)
			if err != nil || after.ID != before.ID || after.Hash != before.Hash {
				o.fail(now, "采集期间发布或运行配置变化，本次统计已丢弃")
				return
			}
			allowed := map[string]bool{}
			for _, item := range before.Services {
				if item.Enabled {
					allowed[item.ID+"/"+item.Hostname] = true
				}
			}
			filtered := domain.MetricSnapshot{Epoch: snapshot.Epoch}
			for _, v := range snapshot.Series {
				if allowed[v.ServiceID+"/"+v.Hostname] {
					filtered.Series = append(filtered.Series, v)
				}
			}
			previous := o.previous
			if o.deploymentID != before.ID {
				previous = domain.MetricSnapshot{}
			}
			delta, gap := domain.MetricDeltas(previous, filtered, o.previousTime, now)
			from := o.previousTime
			if from.IsZero() {
				from = now.Add(-15 * time.Second)
			}
			if err = o.store.Write(ctx, from, now, delta, gap); err != nil {
				o.fail(now, "观测写入失败；历史可能不完整，检查观测数据库容量、空间和权限")
			} else {
				o.previous = filtered
				o.previousTime = now
				o.deploymentID = before.ID
				o.change(func(s *domain.ObservationStatus) {
					s.State = "ready"
					s.DeploymentID = before.ID
					s.LastSample = now.UTC().Format(time.RFC3339Nano)
					s.Message = "指标采集中，历史窗口按实际采集覆盖率显示"
					if gap {
						s.State = "baseline"
						s.Message = "正在重新建立计数基线；重启、发布或断采边界保留为缺口"
					}
				})
			}
		}
	} else {
		o.previous = domain.MetricSnapshot{}
		o.change(func(s *domain.ObservationStatus) {
			s.State = "disabled"
			s.Message = "指标未启用；日志独立采集"
		})
	}
	if settings.AccessLogsEnabled {
		o.collectLogs(ctx)
	} else {
		o.change(func(s *domain.ObservationStatus) { s.LogsState = "disabled" })
	}
}
func safeEvent(e domain.AccessEvent) bool {
	if len(e.Epoch) == 0 || len(e.Epoch) > 128 || e.Sequence == 0 || len(e.ServiceID) > 128 || len(e.Hostname) > 253 {
		return false
	}
	at, err := time.Parse(time.RFC3339Nano, e.Time)
	if err != nil || at.After(time.Now().Add(time.Minute)) || at.Before(time.Now().Add(-7*24*time.Hour)) {
		return false
	}
	if e.Kind == "diagnostic" {
		switch e.Class {
		case "acme_rate_limit", "caa", "dns_authorization", "dns", "acme_unknown":
			return e.ServiceID == "" && e.Path == "" && e.IP == "" && e.Hostname == "" && e.Method == ""
		default:
			return false
		}
	}
	if e.Kind != "access" || e.Status < 100 || e.Status > 599 || e.Duration < 0 || e.Duration > 7*24*3600 || e.RequestBytes < 0 || e.ResponseBytes < 0 {
		return false
	}
	switch e.Path {
	case "/", "/<redacted>", "/api/*", "/assets/*", "/static/*", "/health/*", "/login/*", "/auth/*", "/public/*":
	default:
		return false
	}
	switch e.Method {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE", "OTHER":
	default:
		return false
	}
	if e.IP != "unknown" {
		p, err := netip.ParsePrefix(e.IP)
		if err != nil || p != p.Masked() || (p.Addr().Is4() && p.Bits() != 24) || (!p.Addr().Is4() && p.Bits() != 48) {
			return false
		}
	}
	return e.Class == ""
}
func (o *Observer) collectLogs(ctx context.Context) {
	if !o.cursorLoaded {
		cursor, err := o.store.LogCursor(ctx)
		if err != nil {
			o.change(func(s *domain.ObservationStatus) { s.LogsState = "unavailable" })
			return
		}
		o.logEpoch, o.logAfter = cursor.Epoch, cursor.After
		o.cursorLoaded = true
		o.change(func(s *domain.ObservationStatus) { s.LogGap = s.LogGap || cursor.Gap; s.DroppedLogs = cursor.Dropped })
	}
	if !o.historyLoaded {
		history, err := o.service.repository.Deployments(ctx, 0, 100)
		if err != nil {
			o.change(func(s *domain.ObservationStatus) { s.LogsState = "unavailable" })
			return
		}
		for _, d := range history {
			if d.Status != "success" {
				continue
			}
			for _, s := range d.Services {
				if s.Enabled {
					o.known[s.ID+"/"+s.Hostname] = true
				}
				if len(o.known) >= 2000 {
					break
				}
			}
			if len(o.known) >= 2000 {
				break
			}
		}
		o.historyLoaded = true
	}
	// At most four pages per tick; preserve cursor only after a durable write.
	for i := 0; i < 4; i++ {
		batch, err := o.source.Logs(ctx, o.logEpoch, o.logAfter)
		if err != nil || !batch.Available {
			o.change(func(s *domain.ObservationStatus) { s.LogsState = "unavailable" })
			return
		}
		pageLength := len(batch.Items)
		events := []domain.AccessEvent{}
		for _, e := range batch.Items {
			if !safeEvent(e) {
				o.change(func(s *domain.ObservationStatus) { s.LogGap = true })
				continue
			}
			if e.Kind == "access" && !o.known[e.ServiceID+"/"+e.Hostname] {
				o.change(func(s *domain.ObservationStatus) { s.LogGap = true })
				continue
			}
			events = append(events, e)
		}
		batch.Items = events
		previousStatus := o.Status()
		batch.Gap = batch.Gap || previousStatus.LogGap || batch.Dropped > previousStatus.DroppedLogs
		if err = o.store.AppendLogs(ctx, batch); err != nil {
			o.change(func(s *domain.ObservationStatus) { s.LogsState = "unavailable" })
			return
		}
		o.logEpoch, o.logAfter = batch.Epoch, batch.After
		o.change(func(s *domain.ObservationStatus) {
			s.LogsState = "ready"
			s.LogGap = s.LogGap || batch.Gap || batch.Dropped > s.DroppedLogs
			s.DroppedLogs = batch.Dropped
		})
		if pageLength < 500 {
			return
		}
	}
}
func (s *Service) ObservationStatus(context.Context) domain.ObservationStatus {
	if s.observer == nil {
		return domain.ObservationStatus{State: "unavailable", LogsState: "unavailable", Message: "观测尚未启动", IntervalSeconds: 15}
	}
	return s.observer.Status()
}
func (s *Service) observationStore() (ObservationStore, error) {
	if s.observer == nil || s.observer.store == nil {
		return nil, &domain.AppError{Status: 503, Code: "observation_unavailable", Message: "观测存储不可用；请检查空间和权限后重试"}
	}
	return s.observer.store, nil
}
func ValidateTrafficQuery(q domain.TrafficQuery) error {
	if !q.To.After(q.From) || q.From.Before(time.Now().Add(-365*24*time.Hour)) || q.To.After(time.Now().Add(time.Minute)) || len(q.ServiceID) > 128 {
		return domain.Invalid("时间范围需位于最近 365 天且开始早于结束")
	}
	return nil
}
func (s *Service) Traffic(ctx context.Context, q domain.TrafficQuery) (domain.Traffic, error) {
	if err := ValidateTrafficQuery(q); err != nil {
		return domain.Traffic{}, err
	}
	store, err := s.observationStore()
	if err != nil {
		return domain.Traffic{}, err
	}
	return store.Traffic(ctx, q)
}
func (s *Service) AccessLogs(ctx context.Context, q domain.LogQuery) ([]domain.AccessEvent, error) {
	if err := ValidateTrafficQuery(domain.TrafficQuery{From: q.From, To: q.To, ServiceID: q.ServiceID}); err != nil {
		return nil, err
	}
	if q.From.Before(time.Now().Add(-7*24*time.Hour)) || q.Limit < 1 || q.Limit > 100 || q.Offset < 0 || q.Offset > 100000 || len(q.Method) > 16 || len(q.Path) > 64 || len(q.IP) > 64 || q.Status != 0 && (q.Status < 100 || q.Status > 599) {
		return nil, domain.Invalid("日志查询参数无效；支持最近 7 天，每页至多 100 条")
	}
	if q.Kind != "" && q.Kind != "access" && q.Kind != "diagnostic" {
		return nil, domain.Invalid("日志种类无效")
	}
	store, err := s.observationStore()
	if err != nil {
		return nil, err
	}
	return store.Logs(ctx, q)
}
func (s *Service) Alerts(ctx context.Context) ([]domain.Alert, error) {
	store, err := s.observationStore()
	if err != nil {
		return nil, err
	}
	return store.Alerts(ctx)
}
func (s *Service) AcknowledgeAlert(ctx context.Context, id, actor string) error {
	if len(id) > 512 || strings.TrimSpace(id) == "" {
		return domain.Invalid("告警 ID 无效")
	}
	store, err := s.observationStore()
	if err != nil {
		return err
	}
	if err = store.Acknowledge(ctx, id); err != nil {
		return err
	}
	return s.repository.Audit(ctx, actor, "alert.acknowledge", id, "acknowledged", 0, 0)
}
