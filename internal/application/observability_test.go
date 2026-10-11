package application

import (
	"context"
	"github.com/gofxq/caddy_admin/internal/domain"
	"testing"
	"time"
)

type observationSourceFake struct {
	snapshot       domain.MetricSnapshot
	read           func()
	batch          domain.LogBatch
	requestedEpoch string
	requestedAfter uint64
}

func (f *observationSourceFake) Metrics(context.Context) (domain.MetricSnapshot, error) {
	if f.read != nil {
		f.read()
	}
	return f.snapshot, nil
}
func (f *observationSourceFake) Logs(_ context.Context, epoch string, after uint64) (domain.LogBatch, error) {
	f.requestedEpoch, f.requestedAfter = epoch, after
	return f.batch, nil
}

type observationStoreFake struct {
	deltas  []domain.ObservationDelta
	gap     bool
	alerts  []domain.Alert
	batch   domain.LogBatch
	cursor  domain.LogBatch
	traffic domain.Traffic
}

func (f *observationStoreFake) Write(_ context.Context, _, _ time.Time, d []domain.ObservationDelta, g bool) error {
	f.deltas = d
	f.gap = g
	return nil
}
func (f *observationStoreFake) Traffic(context.Context, domain.TrafficQuery) (domain.Traffic, error) {
	return f.traffic, nil
}
func (f *observationStoreFake) AppendLogs(_ context.Context, batch domain.LogBatch) error {
	f.batch = batch
	return nil
}
func (f *observationStoreFake) Logs(context.Context, domain.LogQuery) ([]domain.AccessEvent, error) {
	return nil, nil
}
func (f *observationStoreFake) Alerts(context.Context) ([]domain.Alert, error) { return f.alerts, nil }
func (f *observationStoreFake) SaveAlert(_ context.Context, a domain.Alert) error {
	f.alerts = append(f.alerts, a)
	return nil
}
func (f *observationStoreFake) Acknowledge(context.Context, string) error { return nil }
func (f *observationStoreFake) Close() error                              { return nil }
func TestObserverBindsToPublishedIDAndResetsAcrossPublish(t *testing.T) {
	svc, repo, _ := detailFixture()
	repo.latest.Settings.MetricsEnabled = true
	id := repo.latest.Services[0].ID
	host := repo.latest.Services[0].Hostname
	source := &observationSourceFake{snapshot: domain.MetricSnapshot{Epoch: "e", Series: []domain.MetricSeries{{Key: "count", ServiceID: id, Hostname: host, Kind: "duration_count", Value: 2}}}}
	store := &observationStoreFake{}
	o := NewObserver(svc, source, store, nil)
	now := time.Now()
	o.Collect(context.Background(), now)
	source.snapshot.Series[0].Value = 4
	o.Collect(context.Background(), now.Add(15*time.Second))
	if store.gap || len(store.deltas) != 1 || store.deltas[0].Requests != 2 {
		t.Fatal(store.deltas, store.gap)
	}
	source.read = func() { repo.latest.ID = "new-release" }
	o.Collect(context.Background(), now.Add(30*time.Second))
	if o.Status().State != "unavailable" {
		t.Fatal("publish during scrape was accepted")
	}
}
func TestObservationAlertsRequireCompleteTrafficAndMinimumRequests(t *testing.T) {
	r := domain.Traffic{Complete: true, Coverage: 1, Summary: domain.TrafficStats{Requests: 19, FiveXX: 19}}
	if state := trafficAlertState(r, "five_xx"); state != "unknown" {
		t.Fatal(state)
	}
	r.Summary.Requests = 20
	if state := trafficAlertState(r, "five_xx"); state != "active" {
		t.Fatal(state)
	}
	r.Complete = false
	if state := trafficAlertState(r, "five_xx"); state != "unknown" {
		t.Fatal(state)
	}
}

func (f *observationStoreFake) LogCursor(context.Context) (domain.LogBatch, error) {
	return f.cursor, nil
}

type observationHistoryRepository struct{ *detailRepository }

func (r *observationHistoryRepository) Deployments(_ context.Context, offset, limit int) ([]domain.Deployment, error) {
	if offset != 0 || limit != 100 {
		panic("unbounded history")
	}
	return r.history, nil
}
func TestLogsRestoreHistoricalIdentityAndDurableCursor(t *testing.T) {
	svc, repo, _ := detailFixture()
	old := repo.latest
	old.Services = append([]domain.Service{}, old.Services...)
	old.Services[0].ID = "deleted-id"
	old.Services[0].Hostname = "old.example.com"
	repo.history = []domain.Deployment{old}
	svc.repository = &observationHistoryRepository{repo}
	source := &observationSourceFake{batch: domain.LogBatch{Available: true, Epoch: "e", After: 8, Dropped: 2, Items: []domain.AccessEvent{{Epoch: "e", Sequence: 8, Time: time.Now().UTC().Format(time.RFC3339Nano), Kind: "access", ServiceID: "deleted-id", Hostname: "old.example.com", Method: "GET", Status: 200, Path: "/<redacted>", IP: "unknown"}}}}
	store := &observationStoreFake{cursor: domain.LogBatch{Epoch: "e", After: 7, Gap: true}}
	o := NewObserver(svc, source, store, nil)
	o.collectLogs(context.Background())
	if source.requestedEpoch != "e" || source.requestedAfter != 7 || len(store.batch.Items) != 1 || store.batch.Items[0].ServiceID != "deleted-id" || !store.batch.Gap {
		t.Fatalf("history/cursor lost: %+v %+v", source, store.batch)
	}
}

func TestMetricAlertsRejectOldReleaseAndRuntimeDrift(t *testing.T) {
	svc, repo, caddy := detailFixture()
	repo.latest.Settings.AlertsEnabled = true
	repo.latest.Settings.MetricsEnabled = true
	now := time.Now().Truncate(15 * time.Second)
	store := &observationStoreFake{traffic: domain.Traffic{Complete: true, Coverage: 1, Summary: domain.TrafficStats{Requests: 20, FiveXX: 5}}}
	o := NewObserver(svc, nil, store, nil)
	o.status = domain.ObservationStatus{State: "ready", LastSample: now.Format(time.RFC3339Nano), DeploymentID: "previous-release"}
	o.Evaluate(context.Background(), now)
	for _, a := range store.alerts {
		if a.Kind == "five_xx" && a.Status != "unknown" {
			t.Fatal("old release used for alert", a)
		}
	}
	store.alerts = nil
	o.status.DeploymentID = repo.latest.ID
	caddy.raw = []byte(`{"apps":{}}`)
	o.Evaluate(context.Background(), now)
	for _, a := range store.alerts {
		if a.Kind == "five_xx" && a.Status != "unknown" {
			t.Fatal("drift used for alert", a)
		}
	}
	store.alerts = nil
	caddy.raw = repo.latest.Config
	o.Evaluate(context.Background(), now)
	found := false
	for _, a := range store.alerts {
		if a.Kind == "five_xx" {
			found = true
			if a.Status != "active" {
				t.Fatal(a)
			}
		}
	}
	if !found {
		t.Fatal("expected metric alert")
	}
}
