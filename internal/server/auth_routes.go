package server

import (
	"context"

	accessv1 "glex/api/access/v1"
	authnv1 "glex/api/authn/v1"
	userv1 "glex/api/user/v1"
	authndomain "glex/internal/domain/authn"
	authmw "glex/pkg/middleware/auth"
)

// authRoutes is the single declaration of the operations the auth middleware
// enforces. Every operation absent from the table is public — see ADR 0013.
func authRoutes() map[string]authmw.Route {
	return map[string]authmw.Route{
		userv1.OperationUserServiceGetInfo:         {Login: true},
		userv1.OperationUserServiceCreateUser:      {Permission: "system:user:add"},
		userv1.OperationUserServiceGetUser:         {Permission: "system:user:query"},
		userv1.OperationUserServiceListUsers:       {Permission: "system:user:list"},
		userv1.OperationUserServiceUpdateUser:      {Permission: "system:user:edit"},
		userv1.OperationUserServiceDeleteUser:      {Permission: "system:user:remove"},
		accessv1.OperationMenuServiceGetRouters:    {Login: true},
		accessv1.OperationMenuServiceCreateMenu:    {Permission: "system:menu:add"},
		accessv1.OperationMenuServiceGetMenu:       {Permission: "system:menu:query"},
		accessv1.OperationMenuServiceListMenus:     {Permission: "system:menu:list"},
		accessv1.OperationMenuServiceUpdateMenu:    {Permission: "system:menu:edit"},
		accessv1.OperationMenuServiceDeleteMenu:    {Permission: "system:menu:remove"},
		accessv1.OperationRoleServiceCreateRole:    {Permission: "system:role:add"},
		accessv1.OperationRoleServiceGetRole:       {Permission: "system:role:query"},
		accessv1.OperationRoleServiceListRoles:     {Permission: "system:role:list"},
		accessv1.OperationRoleServiceUpdateRole:    {Permission: "system:role:edit"},
		accessv1.OperationRoleServiceDeleteRole:    {Permission: "system:role:remove"},
		accessv1.OperationRoleServiceListRoleMenus: {Permission: "system:role:query"},
		accessv1.OperationRoleServiceUpdateRoleMenus: {
			Permission: "system:role:edit",
		},

		accessv1.OperationRoleAssignmentServiceListUserRoles:   {Permission: "system:user:query"},
		accessv1.OperationRoleAssignmentServiceUpdateUserRoles: {Permission: "system:user:edit"},
		accessv1.OperationRoleAssignmentServiceListRoleUsers:   {Permission: "system:role:list"},

		authnv1.OperationOnlineSessionServiceListOnlineSessions:    {Permission: "monitor:online:list"},
		authnv1.OperationOnlineSessionServiceKickoutOnlineSession:  {Permission: "monitor:online:forceLogout"},
		authnv1.OperationOnlineSessionServiceListMyOnlineSessions:  {Login: true},
		authnv1.OperationOnlineSessionServiceDeleteMyOnlineSession: {Login: true},
	}
}

// authenticatorAdapter adapts authndomain.Authenticator to the middleware's
// interface; domain errors pass through with identical status and reason.
type authenticatorAdapter struct {
	authenticator authndomain.Authenticator
}

func (a authenticatorAdapter) Authenticate(ctx context.Context, bearerToken string) (authmw.Principal, error) {
	return a.authenticator.Authenticate(ctx, bearerToken)
}
