package domain

import (
	"time"
	"unicode/utf8"
)

const SessionLifetime = 12 * time.Hour

type Session struct {
	Username   string `json:"username"`
	CSRF       string `json:"csrf"`
	MustChange bool   `json:"must_change"`
	Token      string `json:"-"`
	Expires    int64  `json:"expires"`
}

// ValidatePassword is shared by setup, password changes and maintenance resets.
func ValidatePassword(password string) error {
	if utf8.RuneCountInString(password) < 8 || len(password) > 256 {
		return Invalid("密码至少 8 个字符，最多 256 字节")
	}
	return nil
}
