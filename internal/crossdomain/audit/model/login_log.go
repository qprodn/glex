package model

// LoginLog is the flat snapshot of one sign-in or session-end event. The
// provider's domain types never cross the crossdomain seam.
type LoginLog struct {
	// The account the event is about; 0 when the attempt never resolved to
	// an account (wrong name, withdrawn user).
	UserID   int64
	UserName string
	IPAddr   string
	Browser  string
	OS       string
	// Success marks the outcome (RuoYi status 0/1).
	Success bool
	// Msg is the human-readable outcome ("登录成功", "密码错误", "强制退出").
	Msg string
	// ActorID is the authenticated principal that caused the row: the
	// signing-in user, the revoking administrator (强退), or 0 for an
	// unauthenticated attempt.
	ActorID int64
}
