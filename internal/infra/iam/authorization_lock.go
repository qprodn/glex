package iaminfra

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"

	"glex/internal/infra/ent"
	"glex/internal/infra/ent/menu"
	"glex/internal/infra/ent/role"
	"glex/internal/infra/ent/user"

	entsql "entgo.io/ent/dialect/sql"
)

// Authorization lock order is global and must never be inverted:
//
//  1. Menu identities, ascending ID.
//  2. Role identities, ascending ID.
//  3. System User identities, ascending ID.
//
// A Menu Grant seam (lockMenuGrant) is first within the Menu phase; a Role
// Assignment seam (lockRoleAssignment) is first within the Role phase.
// Relationship replacement then locks both old and new identity sets and
// re-reads under the concrete locks. Queries that discover downstream
// relationships run again after upstream locks are held. Login takes the
// exact same System User lock key (AuthLoginRepo.LockAndRevalidate).
//
// Locks are transaction-scoped (pg_advisory_xact_lock): they resolve their
// entity client from ctx, so they always execute on the caller's transaction
// and are held until the OUTERMOST InTx commits or rolls back — security
// changes finish their upstream locks before login shares the same identity
// lock, and before the paired Redis revocation runs.

// lockMenus takes the authorization lock of each Menu identity, ascending.
func lockMenus(ctx context.Context, db *ent.Database, ids ...int64) error {
	for _, id := range sortedIDs(ids) {
		_, err := db.Menu(ctx).Query().
			Where(menu.IDEQ(id)).
			Select(menu.FieldID).
			Order(menuAuthorizationLock(id)).
			Only(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

// lockRoles takes the authorization lock of each Role identity, ascending.
func lockRoles(ctx context.Context, db *ent.Database, ids ...int64) error {
	for _, id := range sortedIDs(ids) {
		_, err := db.Role(ctx).Query().
			Where(role.IDEQ(id)).
			Select(role.FieldID).
			Order(roleAuthorizationLock(id)).
			Only(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

// lockSystemUsers takes the authorization lock of each System User identity,
// ascending.
func lockSystemUsers(ctx context.Context, db *ent.Database, ids ...int64) error {
	for _, id := range sortedIDs(ids) {
		_, err := db.User(ctx).Query().
			Where(user.IDEQ(id)).
			Select(user.FieldID).
			Order(systemUserAuthorizationLock(id)).
			Only(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

// lockMenuGrant serializes replacements/deletion of one Role's grant before
// its old Menu identities are read. It belongs to the Menu phase and is always
// acquired before concrete Menu locks.
func lockMenuGrant(ctx context.Context, db *ent.Database, roleID int64) error {
	_, err := db.Role(ctx).Query().
		Where(role.IDEQ(roleID)).
		Select(role.FieldID).
		Order(advisoryAuthorizationLock(authorizationLockKey(authorizationLockMenuGrant, roleID))).
		Only(ctx)
	return err
}

// lockRoleAssignment serializes replacements/deletion of one System User's
// assignment before its old Role identities are read. It belongs to the Role
// phase and is always acquired before concrete Role locks.
func lockRoleAssignment(ctx context.Context, db *ent.Database, systemUserID int64) error {
	_, err := db.User(ctx).Query().
		Where(user.IDEQ(systemUserID)).
		Select(user.FieldID).
		Order(advisoryAuthorizationLock(authorizationLockKey(authorizationLockRoleAssignment, systemUserID))).
		Only(ctx)
	return err
}

type authorizationLockKind string

const (
	authorizationLockMenuGrant      authorizationLockKind = "menu-grant"
	authorizationLockMenu           authorizationLockKind = "menu"
	authorizationLockRoleAssignment authorizationLockKind = "role-assignment"
	authorizationLockRole           authorizationLockKind = "role"
	authorizationLockSystemUser     authorizationLockKind = "system-user"
)

func authorizationLockKey(kind authorizationLockKind, id int64) string {
	return "glex:authorization:" + string(kind) + ":" + strconv.FormatInt(id, 10)
}

// advisoryLockHash maps a namespaced lock key onto the positive int64 space
// PostgreSQL advisory locks accept. It is computed in Go because an ORDER BY
// expression cannot reliably bind arguments through ent's builder: a raw "?"
// placeholder is invalid PostgreSQL syntax, and the builder's parameter merge
// silently renumbers expression arguments onto the statement's earlier
// placeholders. The function must stay stable across processes and releases —
// every service instance must derive the same integer for the same key, or
// the locks stop excluding each other.
func advisoryLockHash(key string) int64 {
	sum := fnv.New64a()
	_, _ = sum.Write([]byte(key))
	return int64(sum.Sum64() & (1<<63 - 1))
}

func advisoryAuthorizationLock(key string) func(*entsql.Selector) {
	return func(selector *entsql.Selector) {
		// ::text keeps the ORDER BY expression orderable: the lock function
		// returns void.
		selector.OrderExpr(entsql.Expr(fmt.Sprintf("pg_advisory_xact_lock(%d)::text", advisoryLockHash(key))))
	}
}

func menuAuthorizationLock(id int64) menu.OrderOption {
	return advisoryAuthorizationLock(authorizationLockKey(authorizationLockMenu, id))
}

func roleAuthorizationLock(id int64) role.OrderOption {
	return advisoryAuthorizationLock(authorizationLockKey(authorizationLockRole, id))
}

func systemUserAuthorizationLock(id int64) user.OrderOption {
	return advisoryAuthorizationLock(authorizationLockKey(authorizationLockSystemUser, id))
}

func sortedIDs(ids []int64) []int64 {
	set := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id > 0 {
			set[id] = struct{}{}
		}
	}
	return sortedSet(set)
}

func sortedSet(set map[int64]struct{}) []int64 {
	ids := make([]int64, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func appendIDs(left, right []int64) []int64 {
	return append(append([]int64(nil), left...), right...)
}
