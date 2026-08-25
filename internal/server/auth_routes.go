package server

import (
	"context"

	authv1 "glex/api/iam/auth/v1"
	menuv1 "glex/api/iam/menu/v1"
	rolev1 "glex/api/iam/role/v1"
	userv1 "glex/api/iam/user/v1"
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
		menuv1.OperationMenuServiceGetRouters:      {Login: true},
		menuv1.OperationMenuServiceCreateMenu:      {Permission: "system:menu:add"},
		menuv1.OperationMenuServiceGetMenu:         {Permission: "system:menu:query"},
		menuv1.OperationMenuServiceListMenus:       {Permission: "system:menu:list"},
		menuv1.OperationMenuServiceUpdateMenu:      {Permission: "system:menu:edit"},
		menuv1.OperationMenuServiceDeleteMenu:      {Permission: "system:menu:remove"},
		rolev1.OperationRoleServiceCreateRole:      {Permission: "system:role:add"},
		rolev1.OperationRoleServiceGetRole:         {Permission: "system:role:query"},
		rolev1.OperationRoleServiceListRoles:       {Permission: "system:role:list"},
		rolev1.OperationRoleServiceUpdateRole:      {Permission: "system:role:edit"},
		rolev1.OperationRoleServiceDeleteRole:      {Permission: "system:role:remove"},
		rolev1.OperationRoleServiceListRoleMenus:   {Permission: "system:role:query"},
		rolev1.OperationRoleServiceUpdateRoleMenus: {Permission: "system:role:edit"},

		authv1.OperationAuthServiceListUserRoles:   {Permission: "system:user:query"},
		authv1.OperationAuthServiceUpdateUserRoles: {Permission: "system:user:edit"},
		authv1.OperationAuthServiceListRoleUsers:   {Permission: "system:role:list"},

		authv1.OperationOnlineSessionServiceListOnlineSessions:    {Permission: "monitor:online:list"},
		authv1.OperationOnlineSessionServiceKickoutOnlineSession:  {Permission: "monitor:online:forceLogout"},
		authv1.OperationOnlineSessionServiceListMyOnlineSessions:  {Login: true},
		authv1.OperationOnlineSessionServiceDeleteMyOnlineSession: {Login: true},
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
