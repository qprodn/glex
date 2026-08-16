package server

import (
	"context"
	"errors"
	"strings"

	policyv1 "glex/api/system/auth/v1/policy"
	"glex/internal/biz"

	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"
)

func parseBearerCredential(header string) (string, error) {
	if header == "" {
		return "", biz.ErrCredentialMissing
	}
	separator := strings.IndexByte(header, ' ')
	if separator <= 0 || !strings.EqualFold(header[:separator], "Bearer") {
		return "", biz.ErrCredentialMalformed
	}
	remaining := header[separator:]
	for len(remaining) > 0 && remaining[0] == ' ' {
		remaining = remaining[1:]
	}
	if remaining == "" || strings.IndexAny(remaining, " \t\r\n") >= 0 {
		return "", biz.ErrCredentialMalformed
	}
	return remaining, nil
}

func authMiddleware(authenticator biz.Authenticator, policies *authPolicyRegistry) middleware.Middleware {
	return func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			transporter, ok := transport.FromServerContext(ctx)
			if !ok || transporter.Kind() != transport.KindHTTP {
				return nil, biz.ErrAuthBackendUnavailable
			}
			policy, ok := policies.lookup(transporter.Operation())
			if !ok {
				return nil, biz.ErrAuthBackendUnavailable
			}
			if policy.mode == policyv1.AuthMode_PUBLIC {
				return next(ctx, req)
			}
			if authenticator == nil {
				return nil, biz.ErrAuthBackendUnavailable
			}
			header := transporter.RequestHeader()
			authorization := ""
			if header != nil {
				authorization = header.Get("Authorization")
			}
			token, err := parseBearerCredential(authorization)
			if err != nil {
				if policy.mode == policyv1.AuthMode_OPTIONAL && errors.Is(err, biz.ErrCredentialMissing) {
					return next(ctx, req)
				}
				return nil, err
			}
			principal, err := authenticator.Authenticate(ctx, token)
			if err != nil {
				if policy.mode == policyv1.AuthMode_OPTIONAL && (errors.Is(err, biz.ErrTokenInvalid) || errors.Is(err, biz.ErrTokenExpired)) {
					return next(ctx, req)
				}
				return nil, err
			}
			if principal == nil {
				return nil, biz.ErrAuthBackendUnavailable
			}
			if policy.mode == policyv1.AuthMode_PERMISSION && !principal.HasPermission(policy.permission) {
				return nil, biz.ErrPermissionDenied
			}
			return next(biz.WithPrincipal(ctx, principal), req)
		}
	}
}
