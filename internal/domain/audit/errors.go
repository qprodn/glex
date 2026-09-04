// Package audit owns the audit module: the monitoring history of what
// operators did (Oper Logs) and how Login Sessions began and ended (Login
// Logs). The package root holds only the typed errors; the objects live in
// entity, the ports in repository, and the module's own rules in service.
package audit

import (
	"github.com/go-kratos/kratos/v3/errors"
)

// Outcomes are the two recorded results (RuoYi status: 0成功 1失败).
const (
	StatusSuccess int8 = 0
	StatusFailure int8 = 1
)

var (
	// ErrOperLogNotFound: the requested oper log does not exist (or is
	// soft-deleted).
	ErrOperLogNotFound = errors.NotFound("OPER_LOG_NOT_FOUND", "oper log not found")
	// ErrLoginLogNotFound: the requested login log does not exist (or is
	// soft-deleted).
	ErrLoginLogNotFound = errors.NotFound("LOGIN_LOG_NOT_FOUND", "login log not found")
	// ErrOperLogInvalid: the record lacks a kratos operation name or a known
	// outcome — a writer bug, never a client input.
	ErrOperLogInvalid = errors.InternalServer("OPER_LOG_INVALID", "oper log record is invalid")
	// ErrLoginLogInvalid: the record lacks an account name or a known
	// outcome — a writer bug, never a client input.
	ErrLoginLogInvalid = errors.InternalServer("LOGIN_LOG_INVALID", "login log record is invalid")
)

var (
	// ErrAuditStoreUnavailable: an audit-store operation failed
	// unexpectedly — driver down, tx failure, serialization. Adapters log
	// the raw error once and return this sentinel (ADR 0007), so no driver
	// text crosses the repository boundary; clients see 503, never a driver
	// message.
	ErrAuditStoreUnavailable = errors.ServiceUnavailable("AUDIT_STORE_UNAVAILABLE", "audit store unavailable")
)
