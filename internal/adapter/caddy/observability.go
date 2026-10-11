package caddy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gofxq/caddy_admin/internal/domain"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

func (client *Client) observationRead(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", client.endpoint(path), nil)
	if err != nil {
		return nil, fmt.Errorf("观测地址无效")
	}
	req.Header.Set("Accept", "text/plain; version=0.0.4")
	resp, err := client.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Caddy 观测数据不可达")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Caddy 观测模块不可用")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil || len(raw) > 8<<20 {
		return nil, fmt.Errorf("观测响应超过限制或读取失败")
	}
	return raw, nil
}
func (client *Client) Metrics(ctx context.Context) (domain.MetricSnapshot, error) {
	raw, err := client.observationRead(ctx, "/metrics")
	if err != nil {
		return domain.MetricSnapshot{}, err
	}
	return ParseMetrics(raw)
}
func ParseMetrics(raw []byte) (domain.MetricSnapshot, error) {
	result := domain.MetricSnapshot{}
	if len(raw) > 8<<20 {
		return result, fmt.Errorf("观测响应超过限制")
	}
	sampleLines := 0
	for _, line := range strings.Split(string(raw), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			sampleLines++
			if sampleLines > 30000 {
				return result, fmt.Errorf("观测样本超过限制")
			}
		}
		if strings.HasPrefix(line, "caddy_admin_") {
			tail := line
			if i := strings.LastIndex(line, "}"); i >= 0 {
				tail = line[i+1:]
			} else {
				if i := strings.IndexByte(line, ' '); i >= 0 {
					tail = line[i:]
				}
			}
			parts := strings.Fields(tail)
			if len(parts) == 0 {
				return result, fmt.Errorf("无效指标值")
			}
			v, e := strconv.ParseFloat(parts[0], 64)
			if e != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
				return result, fmt.Errorf("无效指标值")
			}
		}
	}
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(bytes.NewReader(raw))
	if err != nil {
		return result, fmt.Errorf("观测指标格式无效")
	}
	if epoch := families["caddy_admin_observation_epoch"]; epoch != nil && len(epoch.Metric) == 1 {
		for _, label := range epoch.Metric[0].Label {
			if label.GetName() == "epoch" && len(label.GetValue()) <= 128 {
				result.Epoch = label.GetValue()
			}
		}
	}
	if result.Epoch == "" {
		return result, fmt.Errorf("Caddy 固定标签观测模块未启用")
	}
	seriesCount := 0
	for _, family := range families {
		seriesCount += len(family.Metric)
		if seriesCount > 30000 {
			return result, fmt.Errorf("观测 series 超过限制")
		}
	}
	for name, family := range families {
		kind := ""
		hist := false
		switch name {
		case "caddy_admin_duration_seconds":
			kind = "duration"
			hist = true
		case "caddy_admin_first_byte_seconds":
			kind = "first_byte"
			hist = true
		case "caddy_admin_requests_total":
			kind = "requests"
		case "caddy_admin_request_body_bytes_total":
			kind = "request_bytes"
		case "caddy_admin_response_body_bytes_total":
			kind = "response_bytes"
		default:
			continue
		}
		for _, m := range family.Metric {
			s := domain.MetricSeries{Kind: kind}
			for _, label := range m.Label {
				if len(label.GetValue()) > 253 {
					return result, fmt.Errorf("指标标签超过限制")
				}
				switch label.GetName() {
				case "service_id":
					s.ServiceID = label.GetValue()
				case "host":
					s.Hostname = label.GetValue()
				case "status":
					s.Status = label.GetValue()
				default:
					return result, fmt.Errorf("非预期指标标签")
				}
			}
			if s.ServiceID == "" || len(s.ServiceID) > 128 || s.Hostname == "" {
				return result, fmt.Errorf("无效业务指标标签")
			}
			if kind == "requests" && (len(s.Status) != 3 || !strings.HasSuffix(s.Status, "xx") || s.Status[0] < '1' || s.Status[0] > '5') {
				return result, fmt.Errorf("无效状态标签")
			}
			add := func(kind string, v, upper float64) error {
				if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
					return fmt.Errorf("无效指标计数")
				}
				item := s
				item.Kind = kind
				item.Value = v
				item.Upper = upper
				item.Key = s.ServiceID + "/" + s.Hostname + "/" + kind + "/" + s.Status + "/" + strconv.FormatFloat(upper, 'g', -1, 64)
				result.Series = append(result.Series, item)
				if len(result.Series) > 30000 {
					return fmt.Errorf("观测 series 超过限制")
				}
				return nil
			}
			if hist {
				if m.Histogram == nil {
					return result, fmt.Errorf("无效 histogram")
				}
				if err = add(kind+"_count", float64(m.Histogram.GetSampleCount()), 0); err != nil {
					return result, err
				}
				if err = add(kind+"_sum", m.Histogram.GetSampleSum(), 0); err != nil {
					return result, err
				}
				lastBound, lastCount := -1.0, uint64(0)
				for _, b := range m.Histogram.Bucket {
					upper := b.GetUpperBound()
					if math.IsInf(upper, 1) {
						continue
					}
					if math.IsNaN(upper) || upper <= lastBound || b.GetCumulativeCount() < lastCount || b.GetCumulativeCount() > m.Histogram.GetSampleCount() {
						return result, fmt.Errorf("无效 histogram bucket")
					}
					lastBound, lastCount = upper, b.GetCumulativeCount()
					if err = add(kind+"_bucket", float64(lastCount), upper); err != nil {
						return result, err
					}
				}
			} else {
				if m.Counter == nil {
					return result, fmt.Errorf("无效 counter")
				}
				if err = add(kind, m.Counter.GetValue(), 0); err != nil {
					return result, err
				}
			}
		}
	}
	return result, nil
}
func (client *Client) Logs(ctx context.Context, epoch string, after uint64) (domain.LogBatch, error) {
	path := "/caddy-admin/observability/logs?epoch=" + url.QueryEscape(epoch) + "&after=" + strconv.FormatUint(after, 10)
	raw, err := client.observationRead(ctx, path)
	if err != nil {
		return domain.LogBatch{}, err
	}
	var batch domain.LogBatch
	if err = json.Unmarshal(raw, &batch); err != nil || len(batch.Items) > 500 || len(batch.Epoch) > 128 {
		return domain.LogBatch{}, fmt.Errorf("无效观测日志响应")
	}
	return batch, nil
}
