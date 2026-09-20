package service

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"glex/internal/domain/audit"
	"glex/internal/domain/audit/entity"
	"glex/pkg/listq"

	"github.com/go-kratos/kratos/v3/errors"
)

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestServiceReceivesComponentLogger(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))

	svc := NewAuditService(nil, nil, logger).(*auditImpl)
	svc.logger.Info("configured")

	if !strings.Contains(output.String(), `"component":"domain.audit.svc"`) {
		t.Fatalf("domain log output %q does not contain component %q", output.String(), "domain.audit.svc")
	}
}

// fakeOperLogRepo is an in-memory repository.OperLogRepo for testing the
// audit domain service in isolation from the infrastructure layer.
type fakeOperLogRepo struct {
	byID    map[int64]*entity.OperLog
	nextID  int64
	created []*entity.OperLog
	deleted []int64
	listErr error
}

func newFakeOperLogRepo() *fakeOperLogRepo {
	return &fakeOperLogRepo{byID: make(map[int64]*entity.OperLog), nextID: 1}
}

func (r *fakeOperLogRepo) FindByID(_ context.Context, id int64) (*entity.OperLog, error) {
	o, ok := r.byID[id]
	if !ok {
		return nil, audit.ErrOperLogNotFound
	}
	return o, nil
}

func (r *fakeOperLogRepo) List(context.Context, listq.Query) ([]*entity.OperLog, error) {
	return nil, r.listErr
}

func (r *fakeOperLogRepo) Create(_ context.Context, o *entity.OperLog) (*entity.OperLog, error) {
	o.ID = r.nextID
	r.nextID++
	r.byID[o.ID] = o
	r.created = append(r.created, o)
	return o, nil
}

func (r *fakeOperLogRepo) Delete(_ context.Context, id, _ int64) error {
	if _, ok := r.byID[id]; !ok {
		return audit.ErrOperLogNotFound
	}
	delete(r.byID, id)
	r.deleted = append(r.deleted, id)
	return nil
}

// fakeLoginLogRepo is an in-memory repository.LoginLogRepo.
type fakeLoginLogRepo struct {
	created []*entity.LoginLog
	deleted []int64
}

func (r *fakeLoginLogRepo) List(context.Context, listq.Query) ([]*entity.LoginLog, error) {
	return nil, nil
}

func (r *fakeLoginLogRepo) Create(_ context.Context, l *entity.LoginLog) (*entity.LoginLog, error) {
	l.ID = int64(len(r.created)) + 1
	r.created = append(r.created, l)
	return l, nil
}

func (r *fakeLoginLogRepo) Delete(_ context.Context, id, _ int64) error {
	r.deleted = append(r.deleted, id)
	return nil
}

func TestCreateOperLogValidatesTheRecord(t *testing.T) {
	operLogs := newFakeOperLogRepo()
	svc := NewAuditService(operLogs, &fakeLoginLogRepo{}, testLogger())

	_, err := svc.CreateOperLog(t.Context(), &entity.OperLog{Operation: "", Status: audit.StatusSuccess})
	if !errors.Is(err, audit.ErrOperLogInvalid) {
		t.Fatalf("blank operation: err = %v, want ErrOperLogInvalid", err)
	}
	_, err = svc.CreateOperLog(t.Context(), &entity.OperLog{Operation: "/x.y.Z/R", Status: 9})
	if !errors.Is(err, audit.ErrOperLogInvalid) {
		t.Fatalf("unknown outcome: err = %v, want ErrOperLogInvalid", err)
	}
	created, err := svc.CreateOperLog(t.Context(), &entity.OperLog{Operation: "/iam.v1.UserService/CreateUser", Status: audit.StatusSuccess})
	if err != nil {
		t.Fatalf("valid record: %v", err)
	}
	if created.ID == 0 || len(operLogs.created) != 1 {
		t.Fatalf("valid record not persisted: %+v", operLogs.created)
	}
}

func TestCreateLoginLogValidatesTheRecord(t *testing.T) {
	loginLogs := &fakeLoginLogRepo{}
	svc := NewAuditService(newFakeOperLogRepo(), loginLogs, testLogger())

	_, err := svc.CreateLoginLog(t.Context(), &entity.LoginLog{UserName: "", Status: audit.StatusSuccess})
	if !errors.Is(err, audit.ErrLoginLogInvalid) {
		t.Fatalf("blank account: err = %v, want ErrLoginLogInvalid", err)
	}
	created, err := svc.CreateLoginLog(t.Context(), &entity.LoginLog{UserName: "alice", Status: audit.StatusFailure, Msg: "密码错误"})
	if err != nil {
		t.Fatalf("valid record: %v", err)
	}
	if created.ID == 0 || len(loginLogs.created) != 1 {
		t.Fatalf("valid record not persisted: %+v", loginLogs.created)
	}
}

func TestDeleteOperLogWithdrawsAndPropagatesNotFound(t *testing.T) {
	operLogs := newFakeOperLogRepo()
	svc := NewAuditService(operLogs, &fakeLoginLogRepo{}, testLogger())
	created, _ := svc.CreateOperLog(t.Context(), &entity.OperLog{Operation: "/op", Status: audit.StatusSuccess})

	if err := svc.DeleteOperLog(t.Context(), created.ID, 7); err != nil {
		t.Fatalf("DeleteOperLog: %v", err)
	}
	if len(operLogs.deleted) != 1 || operLogs.deleted[0] != created.ID {
		t.Fatalf("deleted = %v, want [%d]", operLogs.deleted, created.ID)
	}
	if err := svc.DeleteOperLog(t.Context(), 999, 7); !errors.Is(err, audit.ErrOperLogNotFound) {
		t.Fatalf("missing id: err = %v, want ErrOperLogNotFound", err)
	}
}

func TestListOperLogsPropagatesStoreFailures(t *testing.T) {
	operLogs := newFakeOperLogRepo()
	operLogs.listErr = audit.ErrAuditStoreUnavailable
	svc := NewAuditService(operLogs, &fakeLoginLogRepo{}, testLogger())

	_, err := svc.ListOperLogs(t.Context(), listq.Query{Limit: 20})
	if !errors.Is(err, audit.ErrAuditStoreUnavailable) {
		t.Fatalf("ListOperLogs: err = %v, want ErrAuditStoreUnavailable", err)
	}
}
