package domain

import (
	"testing"
	"time"
)

func TestObservationDeltaResetAndHistogramQuantiles(t *testing.T) {
	now := time.Now()
	before := MetricSnapshot{Epoch: "one", Series: []MetricSeries{{Key: "count", ServiceID: "id", Hostname: "app.test", Kind: "duration_count", Value: 10}, {Key: "sum", ServiceID: "id", Hostname: "app.test", Kind: "duration_sum", Value: 5}}}
	after := before
	after.Series = append([]MetricSeries{}, before.Series...)
	after.Series[0].Value = 15
	after.Series[1].Value = 8
	delta, gap := MetricDeltas(before, after, now.Add(-15*time.Second), now)
	if gap || len(delta) != 1 || delta[0].Duration.Count != 5 || delta[0].Duration.Sum != 3 {
		t.Fatal(delta, gap)
	}
	after.Epoch = "two"
	if _, gap = MetricDeltas(before, after, now.Add(-15*time.Second), now); !gap {
		t.Fatal("epoch reset not detected")
	}
	after.Epoch = "one"
	after.Series[0].Value = 1
	if _, gap = MetricDeltas(before, after, now.Add(-15*time.Second), now); !gap {
		t.Fatal("counter reset not detected")
	}
	h := Histogram{Count: 100, Buckets: []Bucket{{Upper: 1, Count: 50}, {Upper: 2, Count: 100}}}
	v, lower := h.Quantile(.95)
	if v == nil || *v != 1.9 || lower {
		t.Fatal(v, lower)
	}
	h.Count = 200
	v, lower = h.Quantile(.95)
	if v == nil || *v != 2 || !lower {
		t.Fatal("overflow must be lower bound")
	}
}
func TestObservationMissingSeriesAndGapDoNotFabricateTraffic(t *testing.T) {
	now := time.Now()
	a := MetricSnapshot{Epoch: "a", Series: []MetricSeries{{Key: "x", Value: 2}}}
	b := MetricSnapshot{Epoch: "a"}
	if _, gap := MetricDeltas(a, b, now.Add(-15*time.Second), now); !gap {
		t.Fatal("disappearing series should be gap")
	}
	if _, gap := MetricDeltas(a, a, now.Add(-time.Hour), now); !gap {
		t.Fatal("disconnected hour treated as complete")
	}
}
