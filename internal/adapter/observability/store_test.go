package observability

import (
	"context"
	"fmt"
	"github.com/gofxq/caddy_admin/internal/domain"
	"os"
	"testing"
	"time"
)

func TestStoreRollupsRetentionLogDedupAndPermissions(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/observability.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().Truncate(time.Minute)
	d := domain.ObservationDelta{ServiceID: "old-id", Hostname: "app.test", Requests: 5, Statuses: map[string]float64{"5xx": 1}, Duration: domain.Histogram{Count: 5, Sum: 3, Buckets: []domain.Bucket{{Upper: 1, Count: 5}}}}
	for i := 1; i <= 4; i++ {
		if err = s.Write(ctx, now.Add(time.Duration(i-1)*15*time.Second), now.Add(time.Duration(i)*15*time.Second), []domain.ObservationDelta{d}, false); err != nil {
			t.Fatal(err)
		}
	}
	q := domain.TrafficQuery{From: now, To: now.Add(time.Minute)}
	r, err := s.Traffic(ctx, q)
	if err != nil || r.Summary.Requests != 20 || r.Summary.FiveXX != 4 || r.Coverage < .99 {
		t.Fatalf("traffic=%+v error=%v", r, err)
	}
	q.ServiceID = "new-id"
	r, err = s.Traffic(ctx, q)
	if err != nil || r.Summary.Requests != 0 {
		t.Fatal("history reassigned")
	}
	e := domain.AccessEvent{Epoch: "e", Sequence: 1, Time: now.Format(time.RFC3339Nano), Kind: "access", ServiceID: "old-id", Path: "/<redacted>"}
	for i := 0; i < 2; i++ {
		if err = s.AppendLogs(ctx, domain.LogBatch{Epoch: e.Epoch, After: e.Sequence, Items: []domain.AccessEvent{e}}); err != nil {
			t.Fatal(err)
		}
	}
	logs, err := s.Logs(ctx, domain.LogQuery{From: now.Add(-time.Second), To: now.Add(time.Minute), Limit: 10})
	if err != nil || len(logs) != 1 {
		t.Fatal(logs, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("database permissions")
	}
	var n int
	s.db.QueryRow("SELECT COUNT(DISTINCT resolution) FROM samples").Scan(&n)
	if n != 5 {
		t.Fatal("missing rollups")
	}
}

func TestTrafficExcludesPartialHistoricalBuckets(t *testing.T) {
	s, err := Open(t.TempDir() + "/observation.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	at := time.Now().Add(-200 * 24 * time.Hour).Truncate(24 * time.Hour)
	d := domain.ObservationDelta{ServiceID: "s", Hostname: "app.test", Requests: 500}
	if err = s.Write(ctx, at, at.Add(15*time.Second), []domain.ObservationDelta{d}, false); err != nil {
		t.Fatal(err)
	}
	r, err := s.Traffic(ctx, domain.TrafficQuery{From: at.Add(time.Minute), To: at.Add(2 * time.Minute)})
	if err != nil || r.Summary.Requests != 0 || r.Coverage != 0 || r.Complete || r.To < r.From {
		t.Fatalf("partial day counted: %+v %v", r, err)
	}
	r, err = s.Traffic(ctx, domain.TrafficQuery{From: at, To: at.Add(24 * time.Hour)})
	if err != nil || r.Summary.Requests != 500 || r.Complete {
		t.Fatalf("full day: %+v %v", r, err)
	}
}
func TestCursorSurvivesRestartAndAcknowledgementSurvivesEvaluation(t *testing.T) {
	path := t.TempDir() + "/observation.db"
	ctx := context.Background()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	cursor := domain.LogBatch{Epoch: "persisted", After: 500, Gap: true, Dropped: 3}
	if err = s.AppendLogs(ctx, cursor); err != nil {
		t.Fatal(err)
	}
	a := domain.Alert{ID: "five_xx:s", FirstSeen: "episode1", Status: "active"}
	if err = s.SaveAlert(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err = s.Acknowledge(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveAlert(ctx, a); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.LogCursor(ctx)
	if err != nil || got.Epoch != cursor.Epoch || got.After != 500 || !got.Gap || got.Dropped != 3 {
		t.Fatal(got, err)
	}
	alerts, err := s.Alerts(ctx)
	if err != nil || len(alerts) != 1 || !alerts[0].Acknowledged {
		t.Fatal(alerts, err)
	}
	a.FirstSeen = "episode2"
	if err = s.SaveAlert(ctx, a); err != nil {
		t.Fatal(err)
	}
	alerts, err = s.Alerts(ctx)
	if err != nil || alerts[0].Acknowledged {
		t.Fatal("new episode kept acknowledgement", err)
	}
}

func TestRetentionKeepsCoarseHistoryAndRejectsUnsupportedSchema(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/observation.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Minute)
	d := domain.ObservationDelta{ServiceID: "s", Hostname: "app.test", Requests: 1}
	if err = s.Write(ctx, now.Add(-2*time.Hour), now.Add(-2*time.Hour+15*time.Second), []domain.ObservationDelta{d}, false); err != nil {
		t.Fatal(err)
	}
	if err = s.cleanup(ctx, now); err != nil {
		t.Fatal(err)
	}
	var fine, coarse int
	s.db.QueryRow("SELECT COUNT(*) FROM samples WHERE resolution=15").Scan(&fine)
	s.db.QueryRow("SELECT COUNT(*) FROM samples WHERE resolution=60").Scan(&coarse)
	if fine != 0 || coarse != 1 {
		t.Fatalf("retention fine=%d coarse=%d", fine, coarse)
	}
	if _, err = s.db.Exec("PRAGMA user_version=99"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if unsupported, err := Open(path); err == nil {
		unsupported.Close()
		t.Fatal("unsupported schema accepted")
	}
}
func TestFullObservationDatabaseRejectsWriteButRetainsReads(t *testing.T) {
	s, err := Open(t.TempDir() + "/observation.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	var pages int
	if err = s.db.QueryRow("PRAGMA page_count").Scan(&pages); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("PRAGMA max_page_count=" + fmt.Sprint(pages)); err != nil {
		t.Fatal(err)
	}
	d := domain.ObservationDelta{ServiceID: "s", Hostname: "app.test", Requests: 1}
	for i := 1; i <= 2000; i++ {
		d.Duration.Buckets = append(d.Duration.Buckets, domain.Bucket{Upper: float64(i), Count: 1})
	}
	now := time.Now().Truncate(time.Minute)
	if err = s.Write(ctx, now, now.Add(15*time.Second), []domain.ObservationDelta{d}, false); err == nil {
		t.Fatal("quota did not reject write")
	}
	if _, err = s.Traffic(ctx, domain.TrafficQuery{From: now, To: now.Add(time.Minute)}); err != nil {
		t.Fatalf("prior data unreadable after failed write: %v", err)
	}
}
