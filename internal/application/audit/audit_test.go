package audit

import (
	"bytes"
	"context"
	"errors"
	"log/slog"

	"strings"
	"testing"

	auditv1 "glex/api/audit/v1"
	"glex/internal/domain/audit"
	auditentity "glex/internal/domain/audit/entity"
	"glex/pkg/listq"
)

// fakeAuditService stands in for the audit domain service: it answers list
// calls from preloaded rows and records deletions, so tests can assert the
// DTO conversion and the ordering defaults in isolation.
type fakeAuditService struct {
	operLogs  []*auditentity.OperLog
	loginLogs []*auditentity.LoginLog
	deleted   []int64
	err       error
}

func (s *fakeAuditService) CreateOperLog(context.Context, *auditentity.OperLog) (*auditentity.OperLog, error) {
	return nil, errors.New("not used")
}

func (s *fakeAuditService) GetOperLog(context.Context, int64) (*auditentity.OperLog, error) {
	return nil, errors.New("not used")
}

func (s *fakeAuditService) ListOperLogs(context.Context, listq.Query) ([]*auditentity.OperLog, error) {
	return s.operLogs, s.err
}

func (s *fakeAuditService) DeleteOperLog(_ context.Context, id int64, _ int64) error {
	s.deleted = append(s.deleted, id)
	return s.err
}

func (s *fakeAuditService) CreateLoginLog(context.Context, *auditentity.LoginLog) (*auditentity.LoginLog, error) {
	return nil, errors.New("not used")
}

func (s *fakeAuditService) ListLoginLogs(context.Context, listq.Query) ([]*auditentity.LoginLog, error) {
	return s.loginLogs, s.err
}

func (s *fakeAuditService) DeleteLoginLog(_ context.Context, id int64, _ int64) error {
	s.deleted = append(s.deleted, id)
	return s.err
}

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestServiceReceivesComponentLogger(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))

	app := NewAuditApplication(nil, logger)
	app.logger.Info("configured")

	if !strings.Contains(output.String(), `"component":"application.audit"`) {
		t.Fatalf("application log output %q does not contain component %q", output.String(), "application.audit")
	}
}

func TestListOperLogsMapsRowsAndOutcome(t *testing.T) {
	svc := &fakeAuditService{operLogs: []*auditentity.OperLog{
		{Operation: "/op", Status: audit.StatusSuccess},
		{Operation: "/op2", Status: audit.StatusFailure, Reason: "X", ErrMsg: "boom"},
	}}
	app := NewAuditApplication(svc, testLogger())

	set, err := app.ListOperLogs(t.Context(), &auditv1.ListOperLogsRequest{})
	if err != nil {
		t.Fatalf("ListOperLogs: %v", err)
	}
	if len(set.OperLogs) != 2 {
		t.Fatalf("rows = %d, want 2", len(set.OperLogs))
	}
	if set.OperLogs[0].Status != auditv1.OperStatus_OPER_STATUS_SUCCESS {
		t.Fatalf("row 0 status = %v, want SUCCESS", set.OperLogs[0].Status)
	}
	if set.OperLogs[1].Status != auditv1.OperStatus_OPER_STATUS_FAILURE || set.OperLogs[1].ErrorMsg != "boom" {
		t.Fatalf("row 1 = %+v, want FAILURE with its error text", set.OperLogs[1])
	}
}

func TestDeleteOperLogPropagatesDomainFailures(t *testing.T) {
	svc := &fakeAuditService{err: audit.ErrOperLogNotFound}
	app := NewAuditApplication(svc, testLogger())

	_, err := app.DeleteOperLog(t.Context(), &auditv1.DeleteOperLogRequest{OperLogId: 9})
	if !errors.Is(err, audit.ErrOperLogNotFound) {
		t.Fatalf("DeleteOperLog: err = %v, want ErrOperLogNotFound", err)
	}
}
