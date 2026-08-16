package data

import (
	"context"
	"sort"
	"strconv"

	"glex/internal/biz"
	"glex/internal/data/ent"
	"glex/internal/data/ent/menu"
	"glex/internal/data/ent/role"
	"glex/internal/data/ent/user"

	entsql "entgo.io/ent/dialect/sql"
)

// securityChangeCoordinator is the single data-layer seam for writes that can
// invalidate an Authorization Snapshot. It owns PostgreSQL transaction and
// Redis-revocation mechanics; callers only describe locks, affected users, and
// the database mutation.
type securityChangeCoordinator struct {
	data    *Data
	revoker securitySessionRevoker
	commit  func(*ent.Tx) error
}

type securitySessionRevoker interface {
	RevokeSystemUsers(context.Context, []biz.SystemUserSessionRevocation) error
}

// NewSecuritySessionRevoker narrows the full Login Session repository to the
// one operation required by the transaction coordinator.
func NewSecuritySessionRevoker(sessions biz.LoginSessionRepo) securitySessionRevoker {
	return sessions
}

// NewSecurityChangeCoordinator constructs the shared security-change seam.
func NewSecurityChangeCoordinator(data *Data, revoker securitySessionRevoker) *securityChangeCoordinator {
	return &securityChangeCoordinator{data: data, revoker: revoker}
}

type securityChangeTx struct {
	tx            *ent.Tx
	affectedUsers map[int64]biz.SessionRevocation
}

// Run enforces the fail-safe ordering: PostgreSQL mutation remains
// uncommitted while affected Login Sessions are revoked, and commit is last.
// A Redis failure therefore rolls PostgreSQL back. A commit failure after a
// successful revocation deliberately does not restore Login Sessions.
func (c *securityChangeCoordinator) Run(ctx context.Context, mutate func(*securityChangeTx) error) error {
	tx, err := c.data.db.Tx(ctx)
	if err != nil {
		return err
	}
	change := &securityChangeTx{
		tx:            tx,
		affectedUsers: make(map[int64]biz.SessionRevocation),
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := mutate(change); err != nil {
		return err
	}
	if len(change.affectedUsers) > 0 {
		ids := make(map[int64]struct{}, len(change.affectedUsers))
		for id := range change.affectedUsers {
			ids[id] = struct{}{}
		}
		revocations := make([]biz.SystemUserSessionRevocation, 0, len(ids))
		for _, systemUserID := range sortedSet(ids) {
			revocations = append(revocations, biz.SystemUserSessionRevocation{
				SystemUserID: systemUserID,
				Policy:       change.affectedUsers[systemUserID],
			})
		}
		if err := c.revoker.RevokeSystemUsers(ctx, revocations); err != nil {
			return err
		}
	}
	commit := c.commit
	if commit == nil {
		commit = func(tx *ent.Tx) error { return tx.Commit() }
	}
	if err := commit(tx); err != nil {
		return err
	}
	committed = true
	return nil
}

// Authorization lock order is global and must never be inverted:
//
//  1. Menu identities, ascending ID.
//  2. Role identities, ascending ID.
//  3. System User identities, ascending ID.
//
// A Menu Grant seam is first within the Menu phase; a Role Assignment seam is
// first within the Role phase. Relationship replacement then locks both old
// and new identity sets and re-reads under the concrete locks. Queries that
// discover downstream relationships run again after upstream locks are held.
// Login uses the exact same System User lock key.
func (s *securityChangeTx) LockMenus(ctx context.Context, ids ...int64) error {
	for _, id := range sortedIDs(ids) {
		_, err := s.tx.Menu.Query().
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

func (s *securityChangeTx) LockRoles(ctx context.Context, ids ...int64) error {
	for _, id := range sortedIDs(ids) {
		_, err := s.tx.Role.Query().
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

func (s *securityChangeTx) LockSystemUsers(ctx context.Context, ids ...int64) error {
	for _, id := range sortedIDs(ids) {
		_, err := s.tx.User.Query().
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

func (s *securityChangeTx) AffectSystemUsers(ids ...int64) {
	for _, id := range ids {
		if id > 0 {
			s.affectedUsers[id] = biz.RevokeAllLoginSessions()
		}
	}
}

func (s *securityChangeTx) AffectSystemUser(systemUserID int64, policy biz.SessionRevocation) {
	if systemUserID > 0 && policy.Required() {
		s.affectedUsers[systemUserID] = policy
	}
}

// LockMenuGrant serializes replacements/deletion of one Role's grant before
// its old Menu identities are read. It belongs to the Menu phase and is always
// acquired before concrete Menu locks.
func (s *securityChangeTx) LockMenuGrant(ctx context.Context, roleID int64) error {
	_, err := s.tx.Role.Query().
		Where(role.IDEQ(roleID)).
		Select(role.FieldID).
		Order(advisoryAuthorizationLock(authorizationLockKey(authorizationLockMenuGrant, roleID))).
		Only(ctx)
	return err
}

// LockRoleAssignment serializes replacements/deletion of one System User's
// assignment before its old Role identities are read. It belongs to the Role
// phase and is always acquired before concrete Role locks.
func (s *securityChangeTx) LockRoleAssignment(ctx context.Context, systemUserID int64) error {
	_, err := s.tx.User.Query().
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

func advisoryAuthorizationLock(key string) func(*entsql.Selector) {
	return func(selector *entsql.Selector) {
		selector.OrderExpr(entsql.Expr("pg_advisory_xact_lock(hashtextextended(?::text, 0))::text", key))
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
