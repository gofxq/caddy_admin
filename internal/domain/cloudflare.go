package domain

import "regexp"

var cloudflareTokenPattern = regexp.MustCompile(`^(?:[A-Za-z0-9_-]{35,50}|cf(?:ut|at)_[A-Za-z0-9_-]{32,256})$`)

func ValidateCloudflareToken(token string) error {
	if !cloudflareTokenPattern.MatchString(token) {
		return Invalid("Cloudflare API Token 未配置或格式无效；请检查 secret 文件")
	}
	return nil
}
