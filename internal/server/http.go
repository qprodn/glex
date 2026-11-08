package server

import (
	"log/slog"

	accessv1 "glex/api/access/v1"
	auditv1 "glex/api/audit/v1"
	authnv1 "glex/api/authn/v1"
	dictv1 "glex/api/dict/v1"
	filev1 "glex/api/file/v1"
	jobv1 "glex/api/job/v1"
	orgv1 "glex/api/org/v1"
	paramv1 "glex/api/param/v1"
	payv1 "glex/api/pay/v1"
	userv1 "glex/api/user/v1"
	wechatv1 "glex/api/wechat/v1"
	accessapp "glex/internal/application/access"
	auditapp "glex/internal/application/audit"
	authnapp "glex/internal/application/authn"
	dictapp "glex/internal/application/dict"
	fileapp "glex/internal/application/file"
	jobapp "glex/internal/application/job"
	orgapp "glex/internal/application/org"
	paramapp "glex/internal/application/param"
	payapp "glex/internal/application/pay"
	userapp "glex/internal/application/user"
	wechatapp "glex/internal/application/wechat"
	"glex/internal/conf"
	auditservice "glex/internal/domain/audit/service"
	authnrepo "glex/internal/domain/authn/repository"
	authmw "glex/pkg/middleware/auth"
	"glex/pkg/middleware/logging"
	"glex/pkg/middleware/operlog"
	"glex/pkg/middleware/requestid"
	"glex/pkg/validate"

	"github.com/go-kratos/kratos/v3/middleware/recovery"
	"github.com/go-kratos/kratos/v3/transport/http"
)

// NewHTTPServer new an HTTP server.
func NewHTTPServer(c *conf.Server, user *userapp.UserApplication, menu *accessapp.MenuApplication, role *accessapp.RoleApplication, assignment *accessapp.RoleAssignmentApplication, login *authnapp.LoginApplication, online *authnapp.OnlineSessionApplication, dict *dictapp.DictApplication, file *fileapp.FileApplication, audit *auditapp.AuditApplication, dept *orgapp.DeptApplication, post *orgapp.PostApplication, postAssignment *orgapp.PostAssignmentApplication, pay *payapp.PayApplication, job *jobapp.JobApplication, config *paramapp.ConfigApplication, payNotifyLimiter NotifyLimiter, authenticator authnrepo.Authenticator, auditDomain auditservice.Audit, wechat *wechatapp.WechatApplication, logger *slog.Logger) *http.Server {
	var opts = []http.ServerOption{
		http.Filter(
			requestid.HTTP(requestid.WithLogger(logger)),
		),
		http.Middleware(
			logging.Server(logger),
			recovery.Recovery(recovery.WithLogger(logger)),
			authmw.Middleware(authenticatorAdapter{authenticator: authenticator}, authRoutes()),
			validate.Middleware(),
			operlog.Server(operLogRecorder{audit: auditDomain},
				operlog.WithLogger(logger),
				operlog.WithSkip(
					authnv1.OperationAuthServiceIssueLoginChallenge,
					authnv1.OperationAuthServiceLogin,
					authnv1.OperationAuthServiceLogout,
					// Mini Program Login is an identity exchange, not a
					// back-office mutation, and it writes no Login Log.
					wechatv1.OperationWechatServiceMiniProgramLogin,
					// The notify route records itself in pay_notify_log
					// (ADR 0020) and carries no Principal — never an
					// oper-log event.
					OperationPayServiceChannelNotification,
					OperationWechatServicePlatformCallback,
					// The Direct Hold callback is the address the app
					// shows. It carries no Principal, writes no oper log,
					// and writes no Login Log.
					OperationWechatServiceAppCallback,
					// Web Authorization identity calls carry no Principal
					// and are not back-office oper logs. They also write
					// no Login Log.
					wechatv1.OperationWechatServiceStartWechatWebAuthorization,
					wechatv1.OperationWechatServiceRedeemWechatHandoffCode,
					OperationWechatServiceWebAuthorizationReturn,
				),
				operlog.WithSecretKeys(append(pay.ChannelSecretFields(), wechat.SecretFields()...)...),
			),
		),
	}
	if c.Http.Network != "" {
		opts = append(opts, http.Network(c.Http.Network))
	}
	if c.Http.Addr != "" {
		opts = append(opts, http.Address(c.Http.Addr))
	}
	if c.Http.Timeout != nil {
		opts = append(opts, http.Timeout(c.Http.Timeout.AsDuration()))
	}
	srv := http.NewServer(opts...)
	userv1.RegisterUserServiceHTTPServer(srv, user)
	accessv1.RegisterMenuServiceHTTPServer(srv, menu)
	accessv1.RegisterRoleServiceHTTPServer(srv, role)
	accessv1.RegisterRoleAssignmentServiceHTTPServer(srv, assignment)
	orgv1.RegisterDeptServiceHTTPServer(srv, dept)
	orgv1.RegisterPostServiceHTTPServer(srv, post)
	orgv1.RegisterPostAssignmentServiceHTTPServer(srv, postAssignment)
	authnv1.RegisterAuthServiceHTTPServer(srv, login)
	authnv1.RegisterOnlineSessionServiceHTTPServer(srv, online)
	dictv1.RegisterDictServiceHTTPServer(srv, dict)
	filev1.RegisterFileServiceHTTPServer(srv, file)
	auditv1.RegisterOperLogServiceHTTPServer(srv, audit)
	auditv1.RegisterLoginLogServiceHTTPServer(srv, audit)
	payv1.RegisterPayServiceHTTPServer(srv, pay)
	wechatv1.RegisterWechatServiceHTTPServer(srv, wechat)
	jobv1.RegisterJobServiceHTTPServer(srv, job)
	paramv1.RegisterConfigServiceHTTPServer(srv, config)
	registerPayNotifyHTTP(srv, pay, payNotifyLimiter)
	registerWechatCallbackHTTP(srv, wechat)
	registerFileTransferHTTP(srv, file)
	registerUserExcelHTTP(srv, user)
	return srv
}
