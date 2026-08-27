package authn

import (
	"context"
	"log/slog"

	authnentity "glex/internal/domain/authn/entity"
	authnrepo "glex/internal/domain/authn/repository"
)

// LoginSessionUsecase exposes the business operations permitted for active
// Login Sessions. It deliberately has no bulk public revocation operation.
type LoginSessionUsecase struct {
	repo   authnrepo.LoginSessionRepo
	logger *slog.Logger
}

// NewLoginSessionUsecase creates a LoginSessionUsecase.
func NewLoginSessionUsecase(repo authnrepo.LoginSessionRepo, logger *slog.Logger) *LoginSessionUsecase {
	return &LoginSessionUsecase{
		repo:   repo,
		logger: logger.With(slog.String("component", "application.authn.login_session")),
	}
}

// ListOnlineSessions lists active Login Sessions across all System Users.
func (uc *LoginSessionUsecase) ListOnlineSessions(ctx context.Context, opts ...authnrepo.ListOption) ([]*authnentity.LoginSession, error) {
	return uc.repo.List(ctx, opts...)
}

// KickoutOnlineSession idempotently revokes one Session ID for an
// administrator. An already-absent Session is a successful no-op.
func (uc *LoginSessionUsecase) KickoutOnlineSession(ctx context.Context, sessionID string) error {
	return uc.repo.Kickout(ctx, sessionID)
}

// ListSystemUserSessions lists only Login Sessions owned by systemUserID.
func (uc *LoginSessionUsecase) ListSystemUserSessions(ctx context.Context, systemUserID int64) ([]*authnentity.LoginSession, error) {
	return uc.repo.ListBySystemUser(ctx, systemUserID)
}

// DeleteSystemUserSession atomically proves ownership and revokes one Login
// Session. Missing Sessions are idempotent; a foreign Session is forbidden.
func (uc *LoginSessionUsecase) DeleteSystemUserSession(ctx context.Context, systemUserID int64, sessionID string) error {
	return uc.repo.LogoutOwned(ctx, systemUserID, sessionID)
}
