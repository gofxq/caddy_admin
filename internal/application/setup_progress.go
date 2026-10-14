package application

type SetupProgressCheck struct {
	SetupCheck
	NextStep string `json:"next_step,omitempty"`
}

// The IP progress interface is public. Only known checks and fixed safe text
// cross this boundary; raw probe errors, settings and addresses stay private.
func publicSetupChecks(checks []SetupCheck) []SetupProgressCheck {
	var result []SetupProgressCheck
	for _, check := range checks {
		if check.Status != "pass" && check.Status != "info" && check.Status != "warning" && check.Status != "block" {
			continue
		}
		item := SetupProgressCheck{SetupCheck: SetupCheck{ID: check.ID, Status: check.Status}}
		switch check.ID {
		case "setup_doh":
			item.Message = "DoH 返回的控制台与随机一层子域地址均与目标 IP 一致"
			if check.Status != "pass" {
				item.Message = "DoH 查询未通过：解析尚未生效、地址不符或查询服务暂不可用"
				item.NextStep = "核对通配符与控制台记录、缓存和网络后点击「重新检查 DNS 与预检」；需要 DNS 变更时进入向导预览并明确确认。"
			}
		case "settings_valid":
			item.Message = "设置格式和安全边界有效"
			if check.Status != "pass" {
				item.Message = "初始化设置未通过格式或安全校验"
				item.NextStep = "进入向导核对域名和服务器 DNS 设置，修正后重新预检。"
			}
		case "resolver_reachable":
			item.Message = "至少一个服务器 DNS 解析器可响应"
			if check.Status != "pass" {
				item.Message = "服务器 DNS 解析器暂不可用，可能影响证书 DNS 校验"
				item.NextStep = "检查服务器到解析器的网络；在向导「高级：服务器 DNS」中选择可达解析器，再重新预检。"
			}
		case "admin_dns":
			item.Message = "服务器 DNS 已返回控制台地址"
			if check.Status != "pass" {
				item.Message = "服务器 DNS 未返回控制台地址；此项仅作提示，不阻止初始化。证书与正式入口就绪状态将在初始化后核对。"
				item.NextStep = "若正式入口尚未就绪，可检查解析器缓存或内网地址过滤，并在交接页面重新核对证书与入口状态。"
			}
		default:
			continue
		}
		result = append(result, item)
	}
	return result
}
