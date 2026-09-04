package server

import (
	"context"

	accessv1 "glex/api/access/v1"
	auditv1 "glex/api/audit/v1"
	authnv1 "glex/api/authn/v1"
	dictv1 "glex/api/dict/v1"
	filev1 "glex/api/file/v1"
	orgv1 "glex/api/org/v1"
	userv1 "glex/api/user/v1"
	domainaudit "glex/internal/domain/audit"
	auditentity "glex/internal/domain/audit/entity"
	auditservice "glex/internal/domain/audit/service"
	authnrepo "glex/internal/domain/authn/repository"
	authmw "glex/pkg/middleware/auth"
	operlog "glex/pkg/middleware/operlog"
)

// authRoutes is the single declaration of the operations the auth middleware
// enforces. Every operation absent from the table is public.
func authRoutes() map[string]authmw.Route {
	return map[string]authmw.Route{
		userv1.OperationUserServiceGetInfo:           {Login: true},
		userv1.OperationUserServiceCreateUser:        {Permission: "system:user:add"},
		userv1.OperationUserServiceGetUser:           {Permission: "system:user:query"},
		userv1.OperationUserServiceListUsers:         {Permission: "system:user:list"},
		userv1.OperationUserServiceUpdateUser:        {Permission: "system:user:edit"},
		userv1.OperationUserServiceDeleteUser:        {Permission: "system:user:remove"},
		accessv1.OperationMenuServiceGetRouters:      {Login: true},
		accessv1.OperationMenuServiceCreateMenu:      {Permission: "system:menu:add"},
		accessv1.OperationMenuServiceGetMenu:         {Permission: "system:menu:query"},
		accessv1.OperationMenuServiceListMenus:       {Permission: "system:menu:list"},
		accessv1.OperationMenuServiceUpdateMenu:      {Permission: "system:menu:edit"},
		accessv1.OperationMenuServiceDeleteMenu:      {Permission: "system:menu:remove"},
		accessv1.OperationRoleServiceCreateRole:      {Permission: "system:role:add"},
		accessv1.OperationRoleServiceGetRole:         {Permission: "system:role:query"},
		accessv1.OperationRoleServiceListRoles:       {Permission: "system:role:list"},
		accessv1.OperationRoleServiceUpdateRole:      {Permission: "system:role:edit"},
		accessv1.OperationRoleServiceDeleteRole:      {Permission: "system:role:remove"},
		accessv1.OperationRoleServiceListRoleMenus:   {Permission: "system:role:query"},
		accessv1.OperationRoleServiceUpdateRoleMenus: {Permission: "system:role:edit"},

		accessv1.OperationRoleAssignmentServiceListUserRoles:   {Permission: "system:user:query"},
		accessv1.OperationRoleAssignmentServiceUpdateUserRoles: {Permission: "system:user:edit"},
		accessv1.OperationRoleAssignmentServiceListRoleUsers:   {Permission: "system:role:list"},

		orgv1.OperationDeptServiceCreateDept:  {Permission: "system:dept:add"},
		orgv1.OperationDeptServiceGetDept:     {Permission: "system:dept:query"},
		orgv1.OperationDeptServiceListDepts:   {Permission: "system:dept:list"},
		orgv1.OperationDeptServiceUpdateDept:  {Permission: "system:dept:edit"},
		orgv1.OperationDeptServiceDeleteDept:  {Permission: "system:dept:remove"},
		// The consumption read every form's department select uses: login
		// required, no Functional Permission (ListDictEntriesByType's
		// pattern).
		orgv1.OperationDeptServiceListDeptTree: {Login: true},

		dictv1.OperationDictServiceCreateDictType:  {Permission: "system:dict:add"},
		dictv1.OperationDictServiceGetDictType:     {Permission: "system:dict:query"},
		dictv1.OperationDictServiceListDictTypes:   {Permission: "system:dict:list"},
		dictv1.OperationDictServiceUpdateDictType:  {Permission: "system:dict:edit"},
		dictv1.OperationDictServiceDeleteDictType:  {Permission: "system:dict:remove"},
		dictv1.OperationDictServiceCreateDictEntry: {Permission: "system:dict:add"},
		dictv1.OperationDictServiceGetDictEntry:    {Permission: "system:dict:query"},
		dictv1.OperationDictServiceListDictEntries: {Permission: "system:dict:list"},
		dictv1.OperationDictServiceUpdateDictEntry: {Permission: "system:dict:edit"},
		dictv1.OperationDictServiceDeleteDictEntry: {Permission: "system:dict:remove"},
		// The consumption read every page's selects and status tags use:
		// login required, no Functional Permission (GetInfo's pattern).
		dictv1.OperationDictServiceListDictEntriesByType: {Login: true},

		// The two transfer routes are hand-written (ADR 0008); their
		// operation names are declared in file_transfer.go.
		OperationFileServiceUploadFile:          {Permission: "system:file:upload"},
		OperationFileServiceDownloadFile:        {Permission: "system:file:download"},
		filev1.OperationFileServiceGetFile:      {Permission: "system:file:query"},
		filev1.OperationFileServiceListFiles:    {Permission: "system:file:list"},
		filev1.OperationFileServiceWithdrawFile: {Permission: "system:file:remove"},

		authnv1.OperationOnlineSessionServiceListOnlineSessions:    {Permission: "monitor:online:list"},
		authnv1.OperationOnlineSessionServiceKickoutOnlineSession:  {Permission: "monitor:online:forceLogout"},
		authnv1.OperationOnlineSessionServiceListMyOnlineSessions:  {Login: true},
		authnv1.OperationOnlineSessionServiceDeleteMyOnlineSession: {Login: true},

		auditv1.OperationOperLogServiceListOperLogs:    {Permission: "monitor:operlog:list"},
		auditv1.OperationOperLogServiceGetOperLog:      {Permission: "monitor:operlog:query"},
		auditv1.OperationOperLogServiceDeleteOperLog:   {Permission: "monitor:operlog:remove"},
		auditv1.OperationLoginLogServiceListLoginLogs:  {Permission: "monitor:loginlog:list"},
		auditv1.OperationLoginLogServiceDeleteLoginLog: {Permission: "monitor:loginlog:remove"},
	}
}

// operLogRecorder adapts the audit domain service to the operlog
// middleware's Recorder port (ADR 0012): the composition root performs the
// adaptation, so the middleware never imports internal/ types.
type operLogRecorder struct {
	audit auditservice.Audit
}

// Record persists one operation record. Recording failures return to the
// middleware, which owns the best-effort handling (Warn, drop).
func (r operLogRecorder) Record(ctx context.Context, entry *operlog.Entry) error {
	o := &auditentity.OperLog{
		Operation:    entry.Operation,
		HTTPMethod:   entry.HTTPMethod,
		OperUserID:   entry.OperUserID,
		OperUserName: entry.OperUserName,
		IPAddr:       entry.IPAddr,
		Request:      entry.Request,
		Reason:       entry.Reason,
		ErrMsg:       entry.ErrMsg,
		LatencyMS:    entry.LatencyMS,
		RequestID:    entry.RequestID,
	}
	if entry.Success {
		o.Status = domainaudit.StatusSuccess
	} else {
		o.Status = domainaudit.StatusFailure
	}
	_, err := r.audit.CreateOperLog(ctx, o)
	return err
}

// authenticatorAdapter adapts authnrepo.Authenticator to the middleware's
// interface; domain errors pass through with identical status and reason.
type authenticatorAdapter struct {
	authenticator authnrepo.Authenticator
}

func (a authenticatorAdapter) Authenticate(ctx context.Context, bearerToken string) (authmw.Principal, error) {
	return a.authenticator.Authenticate(ctx, bearerToken)
}
