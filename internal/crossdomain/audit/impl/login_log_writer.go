package impl

import (
	"context"
	"fmt"
	"time"

	"glex/internal/crossdomain/audit"
	"glex/internal/crossdomain/audit/model"
	domainaudit "glex/internal/domain/audit"
	auditentity "glex/internal/domain/audit/entity"
	auditservice "glex/internal/domain/audit/service"
)

// recordTimeout bounds one audit insert on a detached context: the caller's
// request may already be canceled when the outcome is recorded (a login
// response racing a disconnect), and ADR 0012 keeps a disconnected client
// from canceling its own audit trail.
const recordTimeout = 3 * time.Second

// loginLogWriter satisfies the audit login-log contract by delegating to the
// provider's audit domain service: validation and persistence happen there,
// beside the provider's domain policy.
type loginLogWriter struct {
	audit auditservice.Audit
}

// NewLoginLogWriter adapts the audit domain service to the crossdomain
// login-log contract.
func NewLoginLogWriter(audit auditservice.Audit) audit.LoginLogWriter {
	return &loginLogWriter{audit: audit}
}

// RecordLoginLog stores the requested event as one write-once Login Log row
// (ADR 0012). The write runs on a detached, timeout-bounded context — the
// outcome is already known, so a canceled request must not cancel its own
// record.
func (w *loginLogWriter) RecordLoginLog(ctx context.Context, request *model.LoginLog) error {
	if request == nil || request.UserName == "" {
		return fmt.Errorf("record login log: user_name is required")
	}
	l := &auditentity.LoginLog{
		UserID:   request.UserID,
		UserName: request.UserName,
		IPAddr:   request.IPAddr,
		Browser:  request.Browser,
		OS:       request.OS,
		Msg:      request.Msg,
		CreateBy: request.ActorID,
		UpdateBy: request.ActorID,
	}
	if request.Success {
		l.Status = domainaudit.StatusSuccess
	} else {
		l.Status = domainaudit.StatusFailure
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
	defer cancel()
	_, err := w.audit.CreateLoginLog(recordCtx, l)
	return err
}
