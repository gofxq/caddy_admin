package observability

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/gofxq/caddy_admin/internal/domain"
	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

var resolutions = []int64{15, 60, 300, 3600, 86400}
var retention = []time.Duration{time.Hour, 7 * 24 * time.Hour, 30 * 24 * time.Hour, 180 * 24 * time.Hour, 365 * 24 * time.Hour}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	secure, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	err = secure.Chmod(0600)
	secure.Close()
	if err != nil {
		return nil, err
	}
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(500)&_pragma=max_page_count(65536)&_pragma=journal_size_limit(8388608)&_pragma=wal_autocheckpoint(1000)"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	fail := func(e error) (*Store, error) { db.Close(); return nil, e }
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var version int
	if err = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fail(err)
	}
	if version != 0 && version != 1 {
		return fail(fmt.Errorf("unsupported observability schema"))
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fail(err)
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS samples (resolution INTEGER NOT NULL, time INTEGER NOT NULL, service_id TEXT NOT NULL, hostname TEXT NOT NULL, value TEXT NOT NULL, PRIMARY KEY(resolution,time,service_id,hostname)) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS samples_service ON samples(service_id,resolution,time)`,
		`CREATE TABLE IF NOT EXISTS coverage(resolution INTEGER NOT NULL,time INTEGER NOT NULL,seconds REAL NOT NULL,gap INTEGER NOT NULL,PRIMARY KEY(resolution,time)) WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS logs(epoch TEXT NOT NULL,sequence INTEGER NOT NULL,time INTEGER NOT NULL,service_id TEXT NOT NULL,kind TEXT NOT NULL,method TEXT NOT NULL,status INTEGER NOT NULL,path TEXT NOT NULL,ip TEXT NOT NULL,value TEXT NOT NULL,PRIMARY KEY(epoch,sequence)) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS logs_time ON logs(time DESC)`,
		`CREATE INDEX IF NOT EXISTS logs_service ON logs(service_id,time DESC)`,
		`CREATE TABLE IF NOT EXISTS alerts(id TEXT PRIMARY KEY,value TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS metadata(key TEXT PRIMARY KEY,value TEXT NOT NULL)`,
		`PRAGMA user_version=1`,
	} {
		if _, err = tx.ExecContext(ctx, stmt); err != nil {
			return fail(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fail(err)
	}
	if err = os.Chmod(path, 0600); err != nil {
		return fail(err)
	}
	return &Store{db: db}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Write(ctx context.Context, from, to time.Time, deltas []domain.ObservationDelta, gap bool) error {
	if err := s.cleanup(ctx, to); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	seconds := to.Sub(from).Seconds()
	if gap || seconds < 0 || seconds > 45 {
		seconds = 0
	}
	g := 0
	if gap {
		g = 1
	}
	for _, resolution := range resolutions {
		bucket := (to.Unix() - 1) / resolution * resolution
		if _, err = tx.ExecContext(ctx, `INSERT INTO coverage VALUES(?,?,?,?) ON CONFLICT(resolution,time) DO UPDATE SET seconds=MIN(?,seconds+excluded.seconds),gap=MAX(gap,excluded.gap)`, resolution, bucket, seconds, g, resolution); err != nil {
			return err
		}
		if gap {
			continue
		}
		for _, delta := range deltas {
			var prev string
			var combined domain.ObservationDelta
			err = tx.QueryRowContext(ctx, `SELECT value FROM samples WHERE resolution=? AND time=? AND service_id=? AND hostname=?`, resolution, bucket, delta.ServiceID, delta.Hostname).Scan(&prev)
			if err == nil {
				if err = json.Unmarshal([]byte(prev), &combined); err != nil {
					return err
				}
			} else if err != sql.ErrNoRows {
				return err
			}
			combined.ServiceID, combined.Hostname = delta.ServiceID, delta.Hostname
			combined.Add(delta)
			raw, err := json.Marshal(combined)
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO samples VALUES(?,?,?,?,?) ON CONFLICT(resolution,time,service_id,hostname) DO UPDATE SET value=excluded.value`, resolution, bucket, delta.ServiceID, delta.Hostname, string(raw)); err != nil {
				return err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}
func (s *Store) cleanup(ctx context.Context, now time.Time) error {
	for i, res := range resolutions {
		cutoff := now.Add(-retention[i]).Unix()
		for _, table := range []string{"samples", "coverage"} {
			_, err := s.db.ExecContext(ctx, `DELETE FROM `+table+` WHERE (resolution,time) IN (SELECT resolution,time FROM `+table+` WHERE resolution=? AND time<? LIMIT 1000)`, res, cutoff)
			if err != nil {
				return err
			}
		}
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM logs WHERE (epoch,sequence) IN (SELECT epoch,sequence FROM logs WHERE time<? ORDER BY time LIMIT 5000)`, now.Add(-7*24*time.Hour).Unix())
	return err
}
func (s *Store) Traffic(ctx context.Context, q domain.TrafficQuery) (domain.Traffic, error) {
	r := domain.Traffic{From: q.From.Unix(), To: q.To.Unix(), Points: []domain.TrafficPoint{}, Services: []domain.ObservedService{}}
	age := time.Since(q.From)
	idx := 0
	for idx < len(retention)-1 && age > retention[idx] {
		idx++
	}
	res := resolutions[idx]
	step := res
	for (q.To.Unix()-q.From.Unix())/step > 999 {
		step += res
	}
	r.Step = step
	r.From = (q.From.Unix() + res - 1) / res * res
	r.To = q.To.Unix() / res * res
	if r.To <= r.From {
		r.From, r.To = q.From.Unix(), q.To.Unix()
		return r, nil
	}
	args := []any{res, r.From, r.To}
	filter := ""
	if q.ServiceID != "" {
		filter = " AND service_id=?"
		args = append(args, q.ServiceID)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT time,service_id,hostname,value FROM samples WHERE resolution=? AND time>=? AND time<?`+filter+` ORDER BY time LIMIT 200001`, args...)
	if err != nil {
		return r, err
	}
	points := map[int64]*domain.ObservationDelta{}
	services := map[string]domain.ObservedService{}
	var total domain.ObservationDelta
	count := 0
	for rows.Next() {
		count++
		if count > 200000 {
			rows.Close()
			return r, domain.Invalid("查询数据过多，请缩短时间或选择单个服务")
		}
		var at int64
		var id, host, value string
		if err = rows.Scan(&at, &id, &host, &value); err != nil {
			rows.Close()
			return r, err
		}
		var d domain.ObservationDelta
		if err = json.Unmarshal([]byte(value), &d); err != nil {
			rows.Close()
			return r, err
		}
		key := r.From + (at-r.From)/step*step
		if points[key] == nil {
			points[key] = &domain.ObservationDelta{}
		}
		points[key].Add(d)
		total.Add(d)
		services[id+"/"+host] = domain.ObservedService{ID: id, Hostname: host}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return r, err
	}
	for at, d := range points {
		r.Points = append(r.Points, domain.TrafficPoint{Time: at, TrafficStats: d.Stats()})
	}
	sort.Slice(r.Points, func(i, j int) bool { return r.Points[i].Time < r.Points[j].Time })
	for _, v := range services {
		r.Services = append(r.Services, v)
	}
	sort.Slice(r.Services, func(i, j int) bool { return r.Services[i].Hostname < r.Services[j].Hostname })
	r.Summary = total.Stats()
	var covered float64
	var gaps int
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(seconds),0),COALESCE(SUM(gap),0) FROM coverage WHERE resolution=? AND time>=? AND time<?`, res, r.From, r.To).Scan(&covered, &gaps)
	if err != nil {
		return r, err
	}
	span := float64(r.To - r.From)
	if span > 0 {
		r.Coverage = covered / span
		if r.Coverage > 1 {
			r.Coverage = 1
		}
	}
	r.Complete = r.Coverage >= .999 && gaps == 0
	return r, nil
}
func (s *Store) AppendLogs(ctx context.Context, batch domain.LogBatch) error {
	events := batch.Items
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, e := range events {
		at, err := time.Parse(time.RFC3339Nano, e.Time)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO logs VALUES(?,?,?,?,?,?,?,?,?,?)`, e.Epoch, e.Sequence, at.Unix(), e.ServiceID, e.Kind, e.Method, e.Status, e.Path, e.IP, string(raw)); err != nil {
			return err
		}
	}
	var previous string
	if e := tx.QueryRowContext(ctx, `SELECT value FROM metadata WHERE key='logs_cursor'`).Scan(&previous); e == nil {
		var old domain.LogBatch
		if json.Unmarshal([]byte(previous), &old) == nil {
			batch.Gap = batch.Gap || old.Gap
		}
	}
	batch.Items = nil
	cursor, err := json.Marshal(batch)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO metadata VALUES('logs_cursor',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(cursor)); err != nil {
		return err
	}
	// Bound indexed records even when metrics are disabled.
	_, err = tx.ExecContext(ctx, `DELETE FROM logs WHERE (epoch,sequence) IN (SELECT epoch,sequence FROM logs ORDER BY time DESC LIMIT 5000 OFFSET 100000)`)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return s.cleanup(ctx, time.Now())
}
func (s *Store) Logs(ctx context.Context, q domain.LogQuery) ([]domain.AccessEvent, error) {
	out := []domain.AccessEvent{}
	stmt := `SELECT value FROM logs WHERE time>=? AND time<?`
	args := []any{q.From.Unix(), q.To.Unix()}
	for _, f := range []struct{ column, value string }{{"service_id", q.ServiceID}, {"kind", q.Kind}, {"method", q.Method}, {"path", q.Path}, {"ip", q.IP}} {
		if f.value != "" {
			stmt += " AND " + f.column + "=?"
			args = append(args, f.value)
		}
	}
	if q.Status != 0 {
		stmt += " AND status=?"
		args = append(args, q.Status)
	}
	stmt += " ORDER BY time DESC,sequence DESC LIMIT ? OFFSET ?"
	args = append(args, q.Limit, q.Offset)
	rows, err := s.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var value string
		if err = rows.Scan(&value); err != nil {
			return nil, err
		}
		var e domain.AccessEvent
		if err = json.Unmarshal([]byte(value), &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
func (s *Store) Alerts(ctx context.Context) ([]domain.Alert, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT value FROM alerts ORDER BY CASE json_extract(value,'$.status') WHEN 'active' THEN 0 WHEN 'unknown' THEN 1 ELSE 2 END, json_extract(value,'$.last_seen') DESC,id LIMIT 2000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Alert{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var a domain.Alert
		if err = json.Unmarshal([]byte(raw), &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) SaveAlert(ctx context.Context, a domain.Alert) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current string
	err = tx.QueryRowContext(ctx, `SELECT value FROM alerts WHERE id=?`, a.ID).Scan(&current)
	if err == nil {
		var old domain.Alert
		if err = json.Unmarshal([]byte(current), &old); err != nil {
			return err
		}
		if old.FirstSeen == a.FirstSeen {
			a.Acknowledged = a.Acknowledged || old.Acknowledged
		}
	} else if err != sql.ErrNoRows {
		return err
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO alerts VALUES(?,?) ON CONFLICT(id) DO UPDATE SET value=excluded.value`, a.ID, string(raw)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM alerts WHERE id IN (SELECT id FROM alerts WHERE json_extract(value,'$.status') IN ('resolved','disabled') ORDER BY json_extract(value,'$.last_seen') DESC LIMIT 100 OFFSET 1500)`); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Acknowledge(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	if err = tx.QueryRowContext(ctx, `SELECT value FROM alerts WHERE id=?`, id).Scan(&raw); err == sql.ErrNoRows {
		return domain.NotFound("告警不存在")
	} else if err != nil {
		return err
	}
	var a domain.Alert
	if err = json.Unmarshal([]byte(raw), &a); err != nil {
		return err
	}
	a.Acknowledged = true
	b, _ := json.Marshal(a)
	if _, err = tx.ExecContext(ctx, `UPDATE alerts SET value=? WHERE id=?`, string(b), id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) LogCursor(ctx context.Context) (domain.LogBatch, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM metadata WHERE key='logs_cursor'`).Scan(&value)
	if err == sql.ErrNoRows {
		return domain.LogBatch{}, nil
	}
	if err != nil {
		return domain.LogBatch{}, err
	}
	var cursor domain.LogBatch
	err = json.Unmarshal([]byte(value), &cursor)
	return cursor, err
}
