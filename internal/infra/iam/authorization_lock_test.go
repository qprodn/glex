package iaminfra

import (
	"glex/internal/infra/store"

	"context"
	"reflect"
	"testing"

	"glex/internal/infra/ent"
	"glex/internal/infra/ent/user"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
)

func TestAuthorizationIdentityLockKeysAreNamespacedAndOrdered(t *testing.T) {
	if got := sortedIDs([]int64{9, 2, 9, 4}); !reflect.DeepEqual(got, []int64{2, 4, 9}) {
		t.Fatalf("sorted IDs = %v, got %v", []int64{2, 4, 9}, got)
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

// TestSystemUserAuthorizationLockUsesTransactionScopedPostgresAdvisoryLock
// pins the lock statement's SQL shape: the advisory lock is smuggled through
// an ORDER BY expression and resolves its client from ctx, so it always runs
// on the caller's transaction.
func TestSystemUserAuthorizationLockUsesTransactionScopedPostgresAdvisoryLock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	data := &store.Data{DB: ent.NewDatabaseFromClient(client), Logger: testLogger()}

	mock.ExpectBegin()
	hash := advisoryLockHash(authorizationLockKey(authorizationLockSystemUser, 42))
	mock.ExpectQuery(`SELECT .* FROM "sys_user" WHERE "sys_user"\."user_id" = \$1 ORDER BY pg_advisory_xact_lock\(`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{user.FieldID}).AddRow(int64(42)))
	mock.ExpectCommit()

	err = data.DB.InTx(t.Context(), func(ctx context.Context) error {
		return lockSystemUsers(ctx, data.DB, 42)
	})
	if err != nil {
		t.Fatalf("lockSystemUsers: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
	if hash <= 0 {
		t.Fatalf("advisory lock hash must be positive, got %d", hash)
	}
}
