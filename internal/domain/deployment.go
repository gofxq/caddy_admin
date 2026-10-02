package domain

import (
	"encoding/json"
	"time"
)

type Deployment struct {
	ID          string          `json:"id"`
	Version     int64           `json:"version"`
	Revision    int64           `json:"revision"`
	Status      string          `json:"status"`
	Config      json.RawMessage `json:"-"`
	Services    []Service       `json:"services"`
	BaseHash    string          `json:"base_hash"`
	Hash        string          `json:"hash"`
	Actor       string          `json:"actor"`
	Created     string          `json:"created"`
	Finished    string          `json:"finished"`
	Error       string          `json:"error"`
	RollbackID  string          `json:"rollback_id"`
	Changes     []Change        `json:"changes"`
	Idempotency string          `json:"-"`
	RequestHash string          `json:"-"`
}

type Preview struct {
	ValidationExpiresAt time.Time       `json:"validation_expires_at"`
	RollbackConfig      json.RawMessage `json:"rollback_config,omitempty"`
	RollbackHash        string          `json:"rollback_hash,omitempty"`
	Revision            int64           `json:"revision"`
	Services            []Service       `json:"services"`
	Changes             []Change        `json:"changes"`
	Config              json.RawMessage `json:"config"`
	Hash                string          `json:"hash"`
	RuntimeHash         string          `json:"runtime_hash"`
	ExpectedHash        string          `json:"expected_hash"`
	Drift               bool            `json:"drift"`
	ValidationID        string          `json:"validation_id"`
	RollbackID          string          `json:"rollback_id"`
}

type PublishRequest struct {
	ValidationID string `json:"validation_id"`
	Revision     int64  `json:"revision"`
	ExpectedHash string `json:"expected_hash"`
	Idempotency  string `json:"idempotency_key"`
	ConfirmDrift bool   `json:"confirm_drift"`
}

type AuditEvent struct {
	RollbackID string `json:"rollback_id"`
	ErrorClass string `json:"error_class"`
	ID         int64  `json:"id"`
	Time       string `json:"time"`
	Actor      string `json:"actor"`
	Action     string `json:"action"`
	Object     string `json:"object"`
	Result     string `json:"result"`
	Revision   int64  `json:"revision"`
	Version    int64  `json:"version"`
}
