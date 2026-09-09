package service

import (
	"context"
	"fmt"
	"log/slog"

	"glex/internal/domain/authn/repository"
	"glex/pkg/logx"

	"golang.org/x/crypto/bcrypt"
)

// Credential is the authn domain service behind the crossdomain credential
// contract: the only writer of the credential column besides the login flow
// (ADR 0005). Hashing happens here, beside the provider's domain policy, so
// consumers never pick a hash format.
type Credential interface {
	// Store hashes the requested password with authn's policy and stores it
	// inside the caller's propagated transaction.
	Store(ctx context.Context, systemUserID int64, password string) error
	// StoreAll stores the same password for a batch of System Users inside
	// the caller's propagated transaction, hashing it once — Data Import's
	// initial credential (ADR 0017), where every row shares one password
	// and per-row bcrypt would cost a minute per thousand rows.
	StoreAll(ctx context.Context, systemUserIDs []int64, password string) error
}

type credentialImpl struct {
	credentials repository.CredentialRepo
	logger      *slog.Logger
}

// NewCredentialService creates the credential domain service.
func NewCredentialService(credentials repository.CredentialRepo, logger *slog.Logger) Credential {
	return &credentialImpl{
		credentials: credentials,
		logger:      logger.With(slog.String("component", "domain.authn.svc")),
	}
}

func (s *credentialImpl) Store(ctx context.Context, systemUserID int64, password string) error {
	hash, err := HashPassword(password)
	if err != nil {
		err = fmt.Errorf("write system user credential: hash password: %w", err)
		logx.Propagate(ctx, s.logger, "authn.svc.store_credential", err)
		return err
	}
	if err := s.credentials.StoreCredential(ctx, systemUserID, hash); err != nil {
		logx.Propagate(ctx, s.logger, "authn.svc.store_credential", err)
		return err
	}
	return nil
}

func (s *credentialImpl) StoreAll(ctx context.Context, systemUserIDs []int64, password string) error {
	if len(systemUserIDs) == 0 {
		return nil
	}
	hash, err := HashPassword(password)
	if err != nil {
		err = fmt.Errorf("store system user credentials: hash password: %w", err)
		logx.Propagate(ctx, s.logger, "authn.svc.store_credentials", err)
		return err
	}
	for _, id := range systemUserIDs {
		if err := s.credentials.StoreCredential(ctx, id, hash); err != nil {
			logx.Propagate(ctx, s.logger, "authn.svc.store_credentials", err)
			return err
		}
	}
	return nil
}

// HashPassword hashes a plaintext password with the project's bcrypt policy.
// Credentials belong to authn (ADR 0005), so no other module picks the cost.
func HashPassword(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

// VerifyPassword checks a plaintext password against a stored bcrypt hash.
func VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// DecoyCredentialHash makes unknown-account verification consume the same
// bcrypt work as a wrong password without depending on any real account.
const DecoyCredentialHash = "$2a$10$7EqJtq98hPqEX7fNZaFWoO5jQFKP3N8wJYnAqH.G2L/tW8G8zN2qG"
