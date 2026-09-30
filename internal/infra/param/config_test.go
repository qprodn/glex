package paraminfra

import (
	"log/slog"
	"testing"
	"time"

	"glex/internal/domain/param"
	paramrepo "glex/internal/domain/param/repository"
	"glex/internal/infra/ent"
	entparam "glex/internal/infra/ent/param"
	"glex/internal/infra/store"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
)

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func newTestRepo(t *testing.T) (*configRepo, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	repo := NewConfigRepo(&store.Data{DB: ent.NewDatabaseFromClient(client), Logger: testLogger()})
	return repo.(*configRepo), mock
}

func TestConfigRepoFindByKeyResolvesActiveRow(t *testing.T) {
	repo, mock := newTestRepo(t)
	now := time.Unix(1_700_000_000, 0)

	mock.ExpectQuery(`SELECT .* FROM "sys_config"`).
		WithArgs("sys.account.registerUser").
		WillReturnRows(sqlmock.NewRows(entparam.Columns).
			AddRow(int64(7), int64(0), int64(0), now, now, nil,
				"注册开关", "sys.account.registerUser", "false", int16(1), "备注"))

	c, err := repo.FindByKey(t.Context(), "sys.account.registerUser")
	if err != nil {
		t.Fatalf("FindByKey: %v", err)
	}
	// The smallint switch converts to the entity's bool.
	if !c.BuiltIn || c.ConfigValue != "false" || c.ConfigKey != "sys.account.registerUser" {
		t.Fatalf("config = %#v", c)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestConfigRepoFindByKeyMapsMissingAndFailures(t *testing.T) {
	repo, mock := newTestRepo(t)

	mock.ExpectQuery(`SELECT .* FROM "sys_config"`).
		WithArgs("sys.absent").
		WillReturnRows(sqlmock.NewRows(entparam.Columns))
	if _, err := repo.FindByKey(t.Context(), "sys.absent"); !errors.Is(err, param.ErrConfigNotFound) {
		t.Fatalf("missing key: got %v, want ErrConfigNotFound", err)
	}

	mock.ExpectQuery(`SELECT .* FROM "sys_config"`).
		WithArgs("sys.boom").
		WillReturnError(errors.New("database unavailable"))
	if _, err := repo.FindByKey(t.Context(), "sys.boom"); !errors.Is(err, param.ErrConfigStoreUnavailable) {
		t.Fatalf("store failure: got %v, want ErrConfigStoreUnavailable", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestConfigRepoDeleteSoftDeletesWithActor(t *testing.T) {
	repo, mock := newTestRepo(t)
	now := time.Unix(1_700_000_000, 0)

	// The ent layer wraps the write in its own transaction and reloads the
	// row after the update; the expectation pins the table, the actor, and
	// the id predicate without hard-coding the rest.
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "sys_config" SET "update_by" = \$1, "updated_at" = \$2, "deleted_at" = \$3 WHERE "id" = \$4`).
		WithArgs(int64(42), sqlmock.AnyArg(), sqlmock.AnyArg(), int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT .* FROM "sys_config" WHERE "id" = \$1`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows(entparam.Columns).
			AddRow(int64(7), int64(0), int64(0), now, now, nil,
				"分页大小", "ui.page.size", "20", int16(0), ""))
	mock.ExpectCommit()

	if err := repo.Delete(t.Context(), &paramrepo.DeleteRequest{ID: 7, ActorID: 42}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}
