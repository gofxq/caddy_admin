package domain

import "time"

const SessionLifetime = 12 * time.Hour

type Session struct {
	Username   string `json:"username"`
	CSRF       string `json:"csrf"`
	MustChange bool   `json:"must_change"`
	Token      string `json:"-"`
	Expires    int64  `json:"expires"`
}
