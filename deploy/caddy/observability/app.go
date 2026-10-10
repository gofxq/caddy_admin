// Package observability instruments only explicitly generated business routes.
package observability

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/prometheus/client_golang/prometheus"
)

var active atomic.Pointer[App]

type App struct {
	Metrics       bool   `json:"metrics,omitempty"`
	AccessLogs    bool   `json:"access_logs,omitempty"`
	LogFile       string `json:"log_file,omitempty"`
	registry      *prometheus.Registry
	duration      *prometheus.HistogramVec
	firstByte     *prometheus.HistogramVec
	requestBytes  *prometheus.CounterVec
	responseBytes *prometheus.CounterVec
	requests      *prometheus.CounterVec
	epoch         string
	queue         chan Event
	done          chan struct{}
	stopped       chan struct{}
	dropped       atomic.Uint64
	available     atomic.Bool
	mu            sync.Mutex
	logs          []Event
	sequence      uint64
}
type Event struct {
	Epoch         string  `json:"epoch"`
	Sequence      uint64  `json:"sequence"`
	Time          string  `json:"time"`
	Kind          string  `json:"kind"`
	ServiceID     string  `json:"service_id,omitempty"`
	Hostname      string  `json:"hostname,omitempty"`
	Method        string  `json:"method,omitempty"`
	Status        int     `json:"status,omitempty"`
	Path          string  `json:"path,omitempty"`
	IP            string  `json:"ip,omitempty"`
	Duration      float64 `json:"duration_seconds,omitempty"`
	RequestBytes  int64   `json:"request_bytes,omitempty"`
	ResponseBytes int64   `json:"response_bytes,omitempty"`
	Class         string  `json:"class,omitempty"`
}

func init() {
	caddy.RegisterModule(&App{})
	caddy.RegisterModule(Middleware{})
	caddy.RegisterModule(Admin{})
	caddy.RegisterModule(SafeEncoder{})
}
func (*App) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{ID: "caddy_admin_observability", New: func() caddy.Module { return new(App) }}
}
func newApp(reg *prometheus.Registry) *App {
	a := &App{Metrics: true, registry: reg, queue: make(chan Event, 1024), done: make(chan struct{}), stopped: make(chan struct{})}
	var id [16]byte
	_, _ = rand.Read(id[:])
	a.epoch = hex.EncodeToString(id[:])
	labels := []string{"service_id", "host"}
	a.duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "caddy_admin_duration_seconds", Help: "Completed business request duration.", Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2, 5, 10, 30, 60}}, labels)
	a.firstByte = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "caddy_admin_first_byte_seconds", Help: "Business response first byte duration.", Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2, 5, 10, 30, 60}}, labels)
	a.requestBytes = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "caddy_admin_request_body_bytes_total", Help: "Consumed request body bytes."}, labels)
	a.responseBytes = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "caddy_admin_response_body_bytes_total", Help: "Written response body bytes."}, labels)
	a.requests = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "caddy_admin_requests_total", Help: "Completed requests by status class."}, []string{"service_id", "host", "status"})
	return a
}
func (a *App) Provision(ctx caddy.Context) error {
	initial := newApp(ctx.GetMetricsRegistry())
	a.registry = initial.registry
	a.epoch = initial.epoch
	a.queue = initial.queue
	a.done = initial.done
	a.stopped = initial.stopped
	a.duration = initial.duration
	a.firstByte = initial.firstByte
	a.requestBytes = initial.requestBytes
	a.responseBytes = initial.responseBytes
	a.requests = initial.requests
	if a.Metrics {
		for _, c := range []prometheus.Collector{a.duration, a.firstByte, a.requestBytes, a.responseBytes, a.requests, prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "caddy_admin_observation_epoch", Help: "Configuration generation baseline.", ConstLabels: prometheus.Labels{"epoch": a.epoch}}, func() float64 { return 1 })} {
			if err := a.registry.Register(c); err != nil {
				return err
			}
		}
	}
	return nil
}
func (a *App) Start() error {
	active.Store(a)
	if a.AccessLogs {
		if a.LogFile == "" {
			a.LogFile = filepath.Join(caddy.AppDataDir(), "admin-observability", "access.jsonl")
		}
		a.startWriter()
	}
	return nil
}
func (a *App) Stop() error {
	active.CompareAndSwap(a, nil)
	if a.AccessLogs {
		a.stopWriter()
	}
	return nil
}
func (a *App) record(e Event) {
	if !a.AccessLogs {
		return
	}
	select {
	case a.queue <- e:
	default:
		a.dropped.Add(1)
	}
}
func (a *App) startWriter() {
	go func() {
		defer close(a.stopped)
		var f *os.File
		if os.MkdirAll(filepath.Dir(a.LogFile), 0700) == nil {
			// Recover only this bounded, sanitized spool; never scan arbitrary log paths.
			if existing, err := os.Open(a.LogFile); err == nil {
				scan := bufio.NewScanner(existing)
				scan.Buffer(make([]byte, 4096), 16<<10)
				for scan.Scan() {
					var e Event
					if json.Unmarshal(scan.Bytes(), &e) == nil {
						a.sequence = e.Sequence
						a.mu.Lock()
						a.logs = append(a.logs, e)
						if len(a.logs) > 2000 {
							a.logs = a.logs[1:]
						}
						a.mu.Unlock()
					}
				}
				existing.Close()
			}
			f, _ = os.OpenFile(a.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if f != nil {
				if os.Chmod(a.LogFile, 0600) != nil {
					f.Close()
					f = nil
				}
			}
		}
		a.available.Store(f != nil)
		if f != nil {
			defer func() {
				if f != nil {
					f.Sync()
					f.Close()
				}
			}()
		}
		write := func(e Event) {
			if f == nil {
				a.dropped.Add(1)
				return
			}
			a.sequence++
			e.Sequence = a.sequence
			e.Epoch = a.epoch
			raw, _ := json.Marshal(e)
			raw = append(raw, '\n')
			info, err := f.Stat()
			if err == nil && info.Size()+int64(len(raw)) > 16<<20 {
				f.Close()
				os.Remove(a.LogFile + ".1")
				err = os.Rename(a.LogFile, a.LogFile+".1")
				if err == nil {
					f, err = os.OpenFile(a.LogFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
				}
			}
			if err == nil {
				_, err = f.Write(raw)
			}
			if err != nil {
				a.available.Store(false)
				a.dropped.Add(1)
				return
			}
			a.mu.Lock()
			a.logs = append(a.logs, e)
			if len(a.logs) > 2000 {
				a.logs = a.logs[1:]
			}
			a.mu.Unlock()
		}
		for {
			select {
			case e := <-a.queue:
				write(e)
			case <-a.done:
				for {
					select {
					case e := <-a.queue:
						write(e)
					default:
						return
					}
				}
			}
		}
	}()
}
func (a *App) stopWriter() { close(a.done); <-a.stopped }

type Admin struct{}

func (Admin) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{ID: "admin.api.caddy_admin_observability", New: func() caddy.Module { return new(Admin) }}
}
func (Admin) Routes() []caddy.AdminRoute {
	return []caddy.AdminRoute{{Pattern: "/caddy-admin/observability/logs", Handler: caddy.AdminHandlerFunc(serveLogs)}}
}
func serveLogs(w http.ResponseWriter, r *http.Request) error {
	if r.Method != "GET" {
		w.WriteHeader(405)
		return nil
	}
	a := active.Load()
	if a == nil {
		w.WriteHeader(503)
		return nil
	}
	after, _ := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
	same := r.URL.Query().Get("epoch") == a.epoch
	a.mu.Lock()
	defer a.mu.Unlock()
	events := []Event{}
	gap := !same && after > 0
	if len(a.logs) > 0 && ((!same || after == 0) && a.logs[0].Sequence > 1 || same && after < a.logs[0].Sequence-1) {
		gap = true
	}
	if !same {
		after = 0
	}
	for _, e := range a.logs {
		if e.Sequence > after {
			events = append(events, e)
			if len(events) == 500 {
				break
			}
		}
	}
	if len(events) > 0 {
		after = events[len(events)-1].Sequence
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(map[string]any{"epoch": a.epoch, "after": after, "items": events, "gap": gap, "dropped": a.dropped.Load(), "available": a.AccessLogs && a.available.Load(), "checked_at": time.Now().UTC().Format(time.RFC3339Nano)})
}
