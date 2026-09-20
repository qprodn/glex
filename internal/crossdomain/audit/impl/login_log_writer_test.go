package impl

import (
	"context"
	"errors"
	"testing"

	"glex/internal/crossdomain/audit/model"

	domainaudit "glex/internal/domain/audit"
	auditentity "glex/internal/domain/audit/entity"
	"glex/pkg/listq"
)

// The login-log crossdomain adapter tests: the flat model snapshot maps onto
// the audit module's write-once row (ADR 0012), and a canceled caller
// context cannot cancel the recording.

type fakeAuditService struct {
	created *auditentity.LoginLog
	err     error
}

func (s *fakeAuditService) CreateOperLog(context.Context, *auditentity.OperLog) (*auditentity.OperLog, error) {
	return nil, errors.New("not used")
}

func (s *fakeAuditService) GetOperLog(context.Context, int64) (*auditentity.OperLog, error) {
	return nil, errors.New("not used")
}

func (s *fakeAuditService) ListOperLogs(context.Context, listq.Query) ([]*auditentity.OperLog, error) {
	return nil, errors.New("not used")
}

func (s *fakeAuditService) DeleteOperLog(context.Context, int64, int64) error {
	return errors.New("not used")
}

func (s *fakeAuditService) CreateLoginLog(_ context.Context, l *auditentity.LoginLog) (*auditentity.LoginLog, error) {
	s.created = l
	return l, s.err
}

func (s *fakeAuditService) ListLoginLogs(context.Context, listq.Query) ([]*auditentity.LoginLog, error) {
	return nil, errors.New("not used")
}

func (s *fakeAuditService) DeleteLoginLog(context.Context, int64, int64) error {
	return errors.New("not used")
}

func TestRecordLoginLogMapsTheSnapshot(t *testing.T) {
	svc := &fakeAuditService{}
	w := NewLoginLogWriter(svc)

	err := w.RecordLoginLog(context.Background(), &model.LoginLog{
		UserID:   42,
		UserName: "alice",
		IPAddr:   "192.0.2.10",
		Browser:  "Chrome",
		OS:       "macOS",
		Success:  true,
		Msg:      "登录成功",
		ActorID:  42,
	})
	if err != nil {
		t.Fatalf("RecordLoginLog: %v", err)
	}
	row := svc.created
	if row.Status != domainaudit.StatusSuccess || row.UserName != "alice" || row.CreateBy != 42 {
		t.Fatalf("row = %+v, want a success row for alice created by 42", row)
	}

	err = w.RecordLoginLog(context.Background(), &model.LoginLog{UserName: "mallory"})
	if err != nil {
		t.Fatalf("RecordLoginLog failure: %v", err)
	}
	if svc.created.Status != domainaudit.StatusFailure {
		t.Fatalf("row status = %v, want failure", svc.created.Status)
	}
}

func TestRecordLoginLogRejectsAnonymousRows(t *testing.T) {
	w := NewLoginLogWriter(&fakeAuditService{})
	if err := w.RecordLoginLog(context.Background(), &model.LoginLog{UserName: ""}); err == nil {
		t.Fatal("blank user_name must be refused")
	}
	if err := w.RecordLoginLog(context.Background(), nil); err == nil {
		t.Fatal("nil request must be refused")
	}
}

func TestRecordLoginLogSurvivesACanceledCallerContext(t *testing.T) {
	svc := &fakeAuditService{}
	w := NewLoginLogWriter(svc)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := w.RecordLoginLog(ctx, &model.LoginLog{UserName: "alice", Success: true}); err != nil {
		t.Fatalf("detached record failed: %v", err)
	}
	if svc.created == nil {
		t.Fatal("canceled caller context canceled its own audit trail")
	}
}
