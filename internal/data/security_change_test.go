package data

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"glex/internal/biz"
	"glex/internal/data/ent"
	"glex/internal/data/ent/user"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
)

type recordingSecuritySessions struct {
	revoked []biz.SystemUserSessionRevocation
	calls   int
	err     error
}

func (r *recordingSecuritySessions) RevokeSystemUsers(_ context.Context, revocations []biz.SystemUserSessionRevocation) error {
	r.calls++
	r.revoked = append(r.revoked, revocations...)
	return r.err
}

func TestSecurityChangeRevokesAllAffectedUsersInOneAtomicCall(t *testing.T) {
	sessions := &recordingSecuritySessions{}
	coordinator, mock := newSecurityChangeSQLMock(t, sessions)
	mock.ExpectBegin()
	mock.ExpectCommit()

	if err := coordinator.Run(t.Context(), func(change *securityChangeTx) error {
		change.AffectSystemUsers(84, 42)
		return nil
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sessions.calls != 1 || len(sessions.revoked) != 2 || sessions.revoked[0].SystemUserID != 42 || sessions.revoked[1].SystemUserID != 84 {
		t.Fatalf("atomic sorted batch = calls:%d revocations:%#v", sessions.calls, sessions.revoked)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func newSecurityChangeSQLMock(t *testing.T, sessions securitySessionRevoker) (*securityChangeCoordinator, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	data := &Data{db: client}
	return NewSecurityChangeCoordinator(data, sessions), mock
}

func TestSecurityChangeRedisFailureRollsBackPostgres(t *testing.T) {
	redisFailure := errors.New("redis unavailable")
	sessions := &recordingSecuritySessions{err: redisFailure}
	coordinator, mock := newSecurityChangeSQLMock(t, sessions)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "sys_user" SET "status" = \$1, "updated_at" = \$2 WHERE "sys_user"\."user_id" = \$3`).
		WithArgs(int8(1), sqlmock.AnyArg(), int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	err := coordinator.Run(t.Context(), func(change *securityChangeTx) error {
		if _, err := change.tx.User.Update().Where(user.IDEQ(42)).SetStatus(1).Save(t.Context()); err != nil {
			return err
		}
		change.AffectSystemUsers(42)
		return nil
	})
	if !errors.Is(err, redisFailure) {
		t.Fatalf("Run error = %v, want Redis failure", err)
	}
	if len(sessions.revoked) != 1 || sessions.revoked[0].SystemUserID != 42 {
		t.Fatalf("revocation calls = %v", sessions.revoked)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestSecurityChangeCommitFailureKeepsSessionsRevoked(t *testing.T) {
	commitFailure := errors.New("commit unavailable")
	sessions := &recordingSecuritySessions{}
	coordinator, mock := newSecurityChangeSQLMock(t, sessions)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "sys_user" SET "status" = \$1, "updated_at" = \$2 WHERE "sys_user"\."user_id" = \$3`).
		WithArgs(int8(1), sqlmock.AnyArg(), int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit().WillReturnError(commitFailure)

	err := coordinator.Run(t.Context(), func(change *securityChangeTx) error {
		if _, err := change.tx.User.Update().Where(user.IDEQ(42)).SetStatus(1).Save(t.Context()); err != nil {
			return err
		}
		change.AffectSystemUsers(42)
		return nil
	})
	if !errors.Is(err, commitFailure) {
		t.Fatalf("Run error = %v, want commit failure", err)
	}
	if len(sessions.revoked) != 1 || sessions.revoked[0].SystemUserID != 42 {
		t.Fatalf("sessions were restored after commit failure: %v", sessions.revoked)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestAuthorizationIdentityLockKeysAreNamespacedAndOrdered(t *testing.T) {
	if got := sortedIDs([]int64{9, 2, 9, 4}); !reflect.DeepEqual(got, []int64{2, 4, 9}) {
		t.Fatalf("sorted IDs = %v", got)
	}
	keys := []string{
		authorizationLockKey(authorizationLockMenu, 7),
		authorizationLockKey(authorizationLockRole, 7),
		authorizationLockKey(authorizationLockSystemUser, 7),
	}
	if keys[0] == keys[1] || keys[1] == keys[2] || keys[0] == keys[2] {
		t.Fatalf("identity lock namespaces collide: %v", keys)
	}
}
