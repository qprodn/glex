package entity

import (
	"time"

	"glex/internal/domain/audit"
)

// OperLog is one recorded mutating operation (操作日志), ported from RuoYi
// sys_oper_log. Rows are write-once: the transport middleware mints the
// record after the handler returns (ADR 0012) and the only later change a
// row takes is withdrawal. The operator is denormalized (id + name) so
// history survives account withdrawal.
type OperLog struct {
	ID           int64
	Operation    string
	HTTPMethod   string
	OperUserID   int64
	OperUserName string
	IPAddr       string
	// Request is the redacted request payload (protojson); a placeholder
	// when marshaling itself fails (ADR 0012).
	Request    string
	Status     int8
	Reason     string
	ErrMsg     string
	LatencyMS  int64
	RequestID  string
	CreateBy   int64
	UpdateBy   int64
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Validate enforces the invariants a recorded OperLog must satisfy: a
// non-blank kratos operation name and a known outcome.
func (o *OperLog) Validate() error {
	if o == nil || o.Operation == "" || (o.Status != audit.StatusSuccess && o.Status != audit.StatusFailure) {
		return audit.ErrOperLogInvalid
	}
	return nil
}

// LoginLog is one recorded sign-in or session end (登录日志), ported from
// RuoYi sys_logininfor (renamed sys_login_log). authn writes the row at the
// moment the outcome is known (ADR 0012); the account is denormalized (id +
// name) so a failed attempt for a nonexistent account still records the
// attempted name.
type LoginLog struct {
	ID        int64
	UserID    int64
	UserName  string
	IPAddr    string
	Browser   string
	OS        string
	Status    int8
	Msg       string
	CreateBy  int64
	UpdateBy  int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Validate enforces the invariants a recorded LoginLog must satisfy: an
// account name and a known outcome.
func (l *LoginLog) Validate() error {
	if l == nil || l.UserName == "" || (l.Status != audit.StatusSuccess && l.Status != audit.StatusFailure) {
		return audit.ErrLoginLogInvalid
	}
	return nil
}
