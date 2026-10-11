package application

import (
	"context"
	"github.com/gofxq/caddy_admin/internal/domain"
	"time"
)

func trafficAlertState(t domain.Traffic, kind string) string {
	if !t.Complete || t.Coverage < .99 || t.Summary.Requests < 20 {
		return "unknown"
	}
	switch kind {
	case "five_xx":
		if t.Summary.FiveXX/t.Summary.Requests >= .1 {
			return "active"
		}
	case "latency":
		if t.Summary.P95 == nil {
			return "unknown"
		}
		if *t.Summary.P95 >= 2 {
			return "active"
		}
	}
	return "resolved"
}
func (o *Observer) Evaluate(ctx context.Context, now time.Time) {
	if o.store == nil {
		return
	}
	stored, err := o.store.Alerts(ctx)
	if err != nil {
		return
	}
	old := map[string]domain.Alert{}
	for _, a := range stored {
		old[a.ID] = a
	}
	seen := map[string]bool{}
	emit := func(id, service, kind, state, message string) {
		seen[id] = true
		at := now.UTC().Format(time.RFC3339Nano)
		a, exists := old[id]
		if !exists && state == "resolved" {
			return
		}
		if !exists {
			a = domain.Alert{ID: id, ServiceID: service, Kind: kind, FirstSeen: at}
		}
		if state == "active" && a.Status != "active" && (a.Status != "unknown" || a.ResolvedAt != "") {
			a.FirstSeen = at
			a.Acknowledged = false
			a.ResolvedAt = ""
		}
		if state == "resolved" && a.Status != "resolved" {
			a.ResolvedAt = at
		}
		a.Status = state
		a.Message = message
		a.LastSeen = at
		_ = o.store.SaveAlert(ctx, a)
	}
	latest, err := o.service.repository.Latest(ctx)
	if err != nil {
		for _, a := range stored {
			emit(a.ID, a.ServiceID, a.Kind, "unknown", "无法读取当前发布，告警状态未知")
		}
		return
	}
	enabledIDs := map[string]bool{}
	for _, item := range latest.Services {
		if item.Enabled {
			enabledIDs[item.ID] = true
		}
	}
	for id := range o.probeFailures {
		if !enabledIDs[id] {
			delete(o.probeFailures, id)
			delete(o.probeVersion, id)
		}
	}
	settings := latest.Settings
	if !settings.AlertsEnabled {
		for _, a := range stored {
			emit(a.ID, a.ServiceID, a.Kind, "disabled", "站内告警已关闭，保留历史记录")
		}
		return
	}
	status := o.Status()
	end, parseErr := time.Parse(time.RFC3339Nano, status.LastSample)
	if parseErr == nil {
		end = time.Unix(end.Unix()/15*15, 0)
	}
	runtime, runtimeErr := o.runtime(ctx)
	fresh := parseErr == nil && now.Sub(end) >= 0 && now.Sub(end) < 45*time.Second && status.State == "ready" && status.DeploymentID == latest.ID && runtimeErr == nil && runtime.ID == latest.ID
	for _, s := range latest.Services {
		if !s.Enabled {
			continue
		}
		var traffic domain.Traffic
		if settings.MetricsEnabled && fresh {
			traffic, _ = o.store.Traffic(ctx, domain.TrafficQuery{From: end.Add(-5 * time.Minute), To: end, ServiceID: s.ID})
		}
		for _, rule := range []struct{ kind, message string }{{"five_xx", "最近 5 分钟 5xx 比例达到 10%；查看脱敏访问日志与上游检查"}, {"latency", "最近 5 分钟 P95 达到 2 秒；查看响应时长趋势与上游检查"}} {
			state := trafficAlertState(traffic, rule.kind)
			message := rule.message
			if state == "unknown" {
				message = "指标未启用、低于 20 请求或采集窗口不完整，无法判断"
			} else if state == "resolved" {
				message = "最近完整窗口未达到告警阈值"
			}
			emit(rule.kind+":"+s.ID, s.ID, rule.kind, state, message)
		}
	}
	// Certificate observations remain unknown when fixed-address TLS probing fails.
	for _, c := range o.service.Certificates(ctx) {
		state := "resolved"
		message := "证书未达到到期告警阈值"
		switch c.Status {
		case "unknown":
			state = "unknown"
			message = "无法核对当前呈现证书"
		case "invalid":
			state = "active"
			message = "当前呈现证书未通过信任或主体检查"
		default:
			if c.Days < 30 {
				state = "active"
				message = "证书将在 30 天内到期；查看证书诊断与签发事件"
			}
		}
		emit("certificate:"+c.Subject, "", "certificate", state, message)
	}
	if settings.UpstreamChecksEnabled {
		enabled := []domain.Service{}
		for _, s := range latest.Services {
			if s.Enabled {
				enabled = append(enabled, s)
			}
		}
		for n := 0; n < 4 && n < len(enabled) && ctx.Err() == nil; n++ {
			s := enabled[o.probeIndex%len(enabled)]
			o.probeIndex++
			if o.probeVersion[s.ID] != latest.ID {
				o.probeVersion[s.ID] = latest.ID
				o.probeFailures[s.ID] = 0
			}
			check, err := o.service.CheckUpstream(ctx, s.ID, latest.Hash, latest.ID)
			state := "unknown"
			message := "Manager 视角 TCP 检查未知；不代表 Caddy 或应用健康"
			if err == nil {
				switch check.Status {
				case "reachable":
					o.probeFailures[s.ID] = 0
					state = "resolved"
					message = "Manager 可连接已发布上游的 TCP 端口；不证明应用或 HTTPS 正常"
				case "unreachable":
					o.probeFailures[s.ID]++
					if o.probeFailures[s.ID] >= 3 {
						state = "active"
						message = "Manager 连续三次无法连接已发布上游；外部 Caddy 的网络视角可能不同"
					}
				default:
					o.probeFailures[s.ID] = 0
				}
			} else {
				o.probeFailures[s.ID] = 0
			}
			emit("upstream:"+s.ID, s.ID, "upstream", state, message)
		}
		// Preserve unvisited upstream alerts; each minute has a bounded probe budget.
		for _, s := range enabled {
			seen["upstream:"+s.ID] = true
		}
	} else {
		for _, a := range stored {
			if a.Kind == "upstream" {
				emit(a.ID, a.ServiceID, a.Kind, "disabled", "后台 TCP 检查未启用，保留上次检查记录")
			}
		}
	}
	for _, a := range stored {
		if !seen[a.ID] {
			emit(a.ID, a.ServiceID, a.Kind, "disabled", "对应服务已停用或删除，保留历史告警")
		}
	}
}
