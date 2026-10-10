package domain

import (
	"math"
	"sort"
	"time"
)

type MetricSeries struct {
	Key, ServiceID, Hostname, Kind, Status string
	Upper, Value                           float64
}
type MetricSnapshot struct {
	Epoch  string
	Series []MetricSeries
}
type Bucket struct {
	Upper float64 `json:"upper"`
	Count float64 `json:"count"`
}
type Histogram struct {
	Count   float64  `json:"count"`
	Sum     float64  `json:"sum"`
	Buckets []Bucket `json:"buckets"`
}

func (h *Histogram) Add(other Histogram) {
	h.Count += other.Count
	h.Sum += other.Sum
	for _, b := range other.Buckets {
		found := false
		for i := range h.Buckets {
			if h.Buckets[i].Upper == b.Upper {
				h.Buckets[i].Count += b.Count
				found = true
				break
			}
		}
		if !found {
			h.Buckets = append(h.Buckets, b)
		}
	}
	sort.Slice(h.Buckets, func(i, j int) bool { return h.Buckets[i].Upper < h.Buckets[j].Upper })
}
func (h Histogram) Quantile(q float64) (*float64, bool) {
	if h.Count <= 0 || len(h.Buckets) == 0 {
		return nil, false
	}
	target := q * h.Count
	low, count := 0.0, 0.0
	for _, b := range h.Buckets {
		if b.Count >= target {
			v := low
			if b.Count > count {
				v += (b.Upper - low) * (target - count) / (b.Count - count)
			}
			return &v, false
		}
		low, count = b.Upper, b.Count
	}
	return &low, true
}

type ObservationDelta struct {
	ServiceID     string             `json:"service_id"`
	Hostname      string             `json:"hostname"`
	Requests      float64            `json:"requests"`
	RequestBytes  float64            `json:"request_bytes"`
	ResponseBytes float64            `json:"response_bytes"`
	Statuses      map[string]float64 `json:"statuses"`
	Duration      Histogram          `json:"duration"`
	FirstByte     Histogram          `json:"first_byte"`
}

func (d *ObservationDelta) Add(v ObservationDelta) {
	d.Requests += v.Requests
	d.RequestBytes += v.RequestBytes
	d.ResponseBytes += v.ResponseBytes
	if d.Statuses == nil {
		d.Statuses = map[string]float64{}
	}
	for k, n := range v.Statuses {
		d.Statuses[k] += n
	}
	d.Duration.Add(v.Duration)
	d.FirstByte.Add(v.FirstByte)
}
func MetricDeltas(before, after MetricSnapshot, from, to time.Time) ([]ObservationDelta, bool) {
	if before.Epoch == "" || before.Epoch != after.Epoch || !to.After(from) || to.Sub(from) > 45*time.Second {
		return nil, true
	}
	prev := map[string]float64{}
	for _, s := range before.Series {
		prev[s.Key] = s.Value
	}
	current := map[string]bool{}
	out := map[string]*ObservationDelta{}
	for _, s := range after.Series {
		current[s.Key] = true
		v := s.Value - prev[s.Key]
		if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, true
		}
		key := s.ServiceID + "\x00" + s.Hostname
		d := out[key]
		if d == nil {
			d = &ObservationDelta{ServiceID: s.ServiceID, Hostname: s.Hostname, Statuses: map[string]float64{}}
			out[key] = d
		}
		switch s.Kind {
		case "requests":
			d.Statuses[s.Status] += v
		case "request_bytes":
			d.RequestBytes += v
		case "response_bytes":
			d.ResponseBytes += v
		case "duration_count":
			d.Duration.Count += v
			d.Requests += v
		case "duration_sum":
			d.Duration.Sum += v
		case "duration_bucket":
			d.Duration.Buckets = append(d.Duration.Buckets, Bucket{Upper: s.Upper, Count: v})
		case "first_byte_count":
			d.FirstByte.Count += v
		case "first_byte_sum":
			d.FirstByte.Sum += v
		case "first_byte_bucket":
			d.FirstByte.Buckets = append(d.FirstByte.Buckets, Bucket{Upper: s.Upper, Count: v})
		}
	}
	for key := range prev {
		if !current[key] {
			return nil, true
		}
	}
	result := []ObservationDelta{}
	for _, v := range out {
		sort.Slice(v.Duration.Buckets, func(i, j int) bool { return v.Duration.Buckets[i].Upper < v.Duration.Buckets[j].Upper })
		sort.Slice(v.FirstByte.Buckets, func(i, j int) bool { return v.FirstByte.Buckets[i].Upper < v.FirstByte.Buckets[j].Upper })
		result = append(result, *v)
	}
	return result, false
}

type TrafficQuery struct {
	From, To  time.Time
	ServiceID string
}
type TrafficStats struct {
	Requests           float64            `json:"requests"`
	RequestBytes       float64            `json:"request_bytes"`
	ResponseBytes      float64            `json:"response_bytes"`
	FiveXX             float64            `json:"five_xx"`
	Statuses           map[string]float64 `json:"statuses"`
	P50                *float64           `json:"p50"`
	P95                *float64           `json:"p95"`
	P99                *float64           `json:"p99"`
	FirstByteP95       *float64           `json:"first_byte_p95"`
	QuantileLowerBound bool               `json:"quantile_lower_bound"`
}

func (d ObservationDelta) Stats() TrafficStats {
	a, l1 := d.Duration.Quantile(.5)
	b, l2 := d.Duration.Quantile(.95)
	c, l3 := d.Duration.Quantile(.99)
	f, l4 := d.FirstByte.Quantile(.95)
	return TrafficStats{Requests: d.Requests, RequestBytes: d.RequestBytes, ResponseBytes: d.ResponseBytes, FiveXX: d.Statuses["5xx"], Statuses: d.Statuses, P50: a, P95: b, P99: c, FirstByteP95: f, QuantileLowerBound: l1 || l2 || l3 || l4}
}

type TrafficPoint struct {
	Time int64 `json:"time"`
	TrafficStats
}
type Traffic struct {
	From     int64             `json:"from"`
	To       int64             `json:"to"`
	Step     int64             `json:"step"`
	Coverage float64           `json:"coverage"`
	Complete bool              `json:"complete"`
	Summary  TrafficStats      `json:"summary"`
	Points   []TrafficPoint    `json:"points"`
	Services []ObservedService `json:"services"`
}
type ObservedService struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
}
type ObservationStatus struct {
	DeploymentID    string `json:"deployment_id"`
	State           string `json:"state"`
	Message         string `json:"message"`
	LastAttempt     string `json:"last_attempt"`
	LastSample      string `json:"last_sample"`
	LogsState       string `json:"logs_state"`
	LogGap          bool   `json:"log_gap"`
	DroppedLogs     uint64 `json:"dropped_logs"`
	IntervalSeconds int    `json:"interval_seconds"`
	External        bool   `json:"external_caddy"`
}
type AccessEvent struct {
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
type LogBatch struct {
	Epoch     string        `json:"epoch"`
	After     uint64        `json:"after"`
	Items     []AccessEvent `json:"items"`
	Gap       bool          `json:"gap"`
	Dropped   uint64        `json:"dropped"`
	Available bool          `json:"available"`
}
type LogQuery struct {
	From, To                          time.Time
	ServiceID, Kind, Method, Path, IP string
	Status, Offset, Limit             int
}
type Alert struct {
	ID           string `json:"id"`
	ServiceID    string `json:"service_id"`
	Kind         string `json:"kind"`
	Status       string `json:"status"`
	Message      string `json:"message"`
	FirstSeen    string `json:"first_seen"`
	LastSeen     string `json:"last_seen"`
	ResolvedAt   string `json:"resolved_at"`
	Acknowledged bool   `json:"acknowledged"`
}
type DiagnosticCheck struct {
	Name    string   `json:"name"`
	Status  string   `json:"status"`
	Message string   `json:"message"`
	Values  []string `json:"values"`
}
type Diagnostics struct {
	DomainID  string            `json:"domain_id"`
	CheckedAt string            `json:"checked_at"`
	Vantage   string            `json:"vantage"`
	Checks    []DiagnosticCheck `json:"checks"`
	Events    []AccessEvent     `json:"events"`
}
