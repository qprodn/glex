package data

import (
	"errors"
	"testing"
	"time"

	"glex/internal/biz"
	"glex/internal/data/ent"
	"glex/internal/data/ent/menu"
	"glex/internal/data/ent/role"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
)

func TestMenuRepoLoadRouteAccessQueriesAssignmentsGrantsAndActiveMenus(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	repo := NewMenuRepo(&Data{db: client}, nil)
	now := time.Unix(1_700_000_000, 0)

	mock.ExpectQuery(`SELECT .* FROM "sys_role"`).
		WillReturnRows(sqlmock.NewRows(role.Columns).
			AddRow(int64(7), "Operator", "operator", 1, int8(0), true, "", int64(0), int64(0), now, now, nil))
	mock.ExpectQuery(`SELECT DISTINCT .*"sys_menu".*"role_menus"`).
		WillReturnRows(sqlmock.NewRows([]string{menu.FieldID}).AddRow(int64(10)).AddRow(int64(11)))
	mock.ExpectQuery(`SELECT .* FROM "sys_menu"`).
		WillReturnRows(sqlmock.NewRows(menu.Columns).
			AddRow(int64(9), "System", int64(0), 1, "system", "", "", int8(0), int8(1), int8(biz.MenuTypeDirectory), int8(0), int8(0), "", "system", "", "", int64(0), int64(0), now, now, nil).
			AddRow(int64(10), "Users", int64(9), 1, "users", "system/user/index", "", int8(0), int8(1), int8(biz.MenuTypeMenu), int8(0), int8(0), "system:user:list", "user", "", "", int64(0), int64(0), now, now, nil))

	access, err := repo.LoadRouteAccess(t.Context(), 42)
	if err != nil {
		t.Fatalf("LoadRouteAccess: %v", err)
	}
	if len(access.RoleKeys) != 1 || access.RoleKeys[0] != "operator" {
		t.Fatalf("active Role Assignments = %v", access.RoleKeys)
	}
	if len(access.GrantedMenuIDs) != 2 || access.GrantedMenuIDs[0] != 10 || access.GrantedMenuIDs[1] != 11 {
		t.Fatalf("Menu Grants = %v", access.GrantedMenuIDs)
	}
	if len(access.ActiveMenus) != 2 || access.ActiveMenus[0].ID != 9 || access.ActiveMenus[1].ID != 10 {
		t.Fatalf("active routeable Menus = %#v", access.ActiveMenus)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestMenuRepoLoadRouteAccessSkipsGrantQueryWithoutActiveAssignments(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	repo := NewMenuRepo(&Data{db: client}, nil)

	mock.ExpectQuery(`SELECT .* FROM "sys_role"`).WillReturnRows(sqlmock.NewRows(role.Columns))
	mock.ExpectQuery(`SELECT .* FROM "sys_menu"`).WillReturnRows(sqlmock.NewRows(menu.Columns))

	access, err := repo.LoadRouteAccess(t.Context(), 42)
	if err != nil {
		t.Fatalf("LoadRouteAccess: %v", err)
	}
	if len(access.RoleKeys) != 0 || len(access.GrantedMenuIDs) != 0 || len(access.ActiveMenus) != 0 {
		t.Fatalf("empty route access = %#v", access)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestMenuRepoLoadRouteAccessMapsDatabaseFailures(t *testing.T) {
	tests := []struct {
		name   string
		expect func(sqlmock.Sqlmock, time.Time)
	}{
		{
			name: "Role Assignments",
			expect: func(mock sqlmock.Sqlmock, _ time.Time) {
				mock.ExpectQuery(`SELECT .* FROM "sys_role"`).WillReturnError(errors.New("database unavailable"))
			},
		},
		{
			name: "Menu Grants",
			expect: func(mock sqlmock.Sqlmock, now time.Time) {
				mock.ExpectQuery(`SELECT .* FROM "sys_role"`).
					WillReturnRows(sqlmock.NewRows(role.Columns).
						AddRow(int64(7), "Operator", "operator", 1, int8(0), true, "", int64(0), int64(0), now, now, nil))
				mock.ExpectQuery(`SELECT DISTINCT .*"sys_menu".*"role_menus"`).WillReturnError(errors.New("database unavailable"))
			},
		},
		{
			name: "Menus",
			expect: func(mock sqlmock.Sqlmock, _ time.Time) {
				mock.ExpectQuery(`SELECT .* FROM "sys_role"`).WillReturnRows(sqlmock.NewRows(role.Columns))
				mock.ExpectQuery(`SELECT .* FROM "sys_menu"`).WillReturnError(errors.New("database unavailable"))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New: %v", err)
			}
			t.Cleanup(func() { _ = db.Close() })
			client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			repo := NewMenuRepo(&Data{db: client}, nil)
			tt.expect(mock, time.Unix(1_700_000_000, 0))

			_, err = repo.LoadRouteAccess(t.Context(), 42)
			if !errors.Is(err, biz.ErrAuthBackendUnavailable) {
				t.Fatalf("LoadRouteAccess database failure = %v, want AUTH_BACKEND_UNAVAILABLE", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("SQL expectations: %v", err)
			}
		})
	}
}
