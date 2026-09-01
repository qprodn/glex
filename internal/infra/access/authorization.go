package accessinfra

import (
	"glex/internal/infra/store"

	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"

	"glex/internal/domain/access"
	accessentity "glex/internal/domain/access/entity"
	accessrepo "glex/internal/domain/access/repository"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/menu"
	"glex/internal/infra/ent/role"
	"glex/internal/infra/ent/rolemenu"
	"glex/internal/infra/ent/user"
	"glex/internal/infra/ent/userrole"
)

// superadminPermission is the wildcard Functional Permission only the
// Superadmin Role's resolution yields.
const superadminPermission = "*:*:*"

type authorizationResolver struct {
	data   *store.Data
	logger *slog.Logger
}

// NewAuthorizationResolver creates a new AuthorizationResolver instance — the
// storage-side implementation of access's Authorization Resolution.
func NewAuthorizationResolver(data *store.Data) accessrepo.AuthorizationResolver {
	return &authorizationResolver{
		data:   data,
		logger: data.Logger.With(slog.String("component", "infra.access.authorization")),
	}
}

// ResolveAuthorization freshly reads the System User's active Role Assignments
// and their Menu Grants and computes the Role Keys and Functional Permissions
// captured by an Authorization Snapshot. Reads join the transaction propagated
// through ctx when one is active.
func (r *authorizationResolver) ResolveAuthorization(ctx context.Context, systemUserID int64) (*accessentity.AuthorizationResolution, error) {
	resolution, err := resolveAuthorization(ctx, r.data.DB, systemUserID)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, access.ErrAuthorizationUnavailable
		}
		r.logger.ErrorContext(ctx, "authorization resolution database operation failed",
			slog.String("operation", "access.resolve_authorization"),
			slog.Any("error", err),
		)
		return nil, access.ErrAuthorizationUnavailable
	}
	return resolution, nil
}

func resolveAuthorization(ctx context.Context, db *ent.Database, systemUserID int64) (*accessentity.AuthorizationResolution, error) {
	roles, err := db.User(ctx).Query().
		Where(user.IDEQ(systemUserID), user.DeletedAtIsNil()).
		QueryRoleAssignments().Where(userrole.DeletedAtIsNil()).
		QueryRole().
		Where(role.DeletedAtIsNil(), role.StatusEQ(0)).
		Order(role.ByRoleKey(), role.ByID()).
		All(ctx)
	if err != nil {
		return nil, err
	}
	roleIDs := make([]int64, 0, len(roles))
	for _, assignedRole := range roles {
		roleIDs = append(roleIDs, assignedRole.ID)
	}
	menus := make([]*ent.Menu, 0)
	if len(roleIDs) > 0 {
		menus, err = db.Menu(ctx).Query().
			Where(
				menu.DeletedAtIsNil(),
				menu.StatusEQ(0),
				menu.HasRoleLinksWith(rolemenu.RoleIDIn(roleIDs...), rolemenu.DeletedAtIsNil()),
			).
			Order(menu.ByPerms(), menu.ByID()).
			All(ctx)
		if err != nil {
			return nil, err
		}
	}
	return toBizResolution(roles, menus), nil
}

// toBizResolution folds the assigned roles and granted menus into the
// resolution: blank Role keys are dropped, permission handles are deduped, and
// the wildcard `*:*:*` is added only through an exact superadmin Role key (a
// menu carrying it otherwise is ignored). Both sets are returned sorted.
func toBizResolution(roles []*ent.Role, menus []*ent.Menu) *accessentity.AuthorizationResolution {
	roleKeys := make([]string, 0, len(roles))
	superadmin := false
	for _, assignedRole := range roles {
		key := assignedRole.RoleKey
		if strings.TrimSpace(key) == "" {
			continue
		}
		roleKeys = append(roleKeys, key)
		if key == accessentity.SuperadminRoleKey {
			superadmin = true
		}
	}
	permissions := make([]string, 0, len(menus)+1)
	seen := make(map[string]struct{}, len(menus)+1)
	for _, grantedMenu := range menus {
		permission := strings.TrimSpace(grantedMenu.Perms)
		if permission == "" || permission == superadminPermission && !superadmin {
			continue
		}
		if _, ok := seen[permission]; ok {
			continue
		}
		seen[permission] = struct{}{}
		permissions = append(permissions, permission)
	}
	if superadmin {
		if _, ok := seen[superadminPermission]; !ok {
			permissions = append(permissions, superadminPermission)
		}
	}
	sort.Strings(roleKeys)
	sort.Strings(permissions)
	return &accessentity.AuthorizationResolution{
		RoleKeys:    roleKeys,
		Permissions: permissions,
	}
}
