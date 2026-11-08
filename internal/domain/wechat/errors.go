// Package wechat owns the wechat module per ADR 0005 and ADR 0029: the
// WeChat Platforms this back office registers and the WeChat Apps
// connected to them. The package root holds only the typed errors; the
// objects live in entity, the row ports in repository, and the module's
// own rules in service.
package wechat

import "github.com/go-kratos/kratos/v3/errors"

var (
	// ErrPlatformNotFound: the requested WeChat Platform does not exist
	// or is withdrawn.
	ErrPlatformNotFound = errors.NotFound("WECHAT_PLATFORM_NOT_FOUND", "wechat platform not found")
	// ErrPlatformInvalid: a WeChat Platform parameter is invalid — blank
	// name, blank component AppID, missing secret on register, or an
	// over-length value.
	ErrPlatformInvalid = errors.BadRequest("WECHAT_PLATFORM_INVALID", "wechat platform parameters are invalid")
	// ErrPlatformDuplicate: a platform that is not withdrawn already holds
	// this component AppID (ADR 0029).
	ErrPlatformDuplicate = errors.Conflict("WECHAT_PLATFORM_DUPLICATE", "component app id already exists")
	// ErrPlatformAppIDImmutable: the component AppID is fixed for the life
	// of the row (ADR 0029).
	ErrPlatformAppIDImmutable = errors.BadRequest("WECHAT_PLATFORM_APP_ID_IMMUTABLE", "component app id cannot be changed")
	// ErrPlatformNotReady: the platform does not hold its secrets and a
	// verify ticket, so it cannot start or finish a Platform Authorization.
	ErrPlatformNotReady = errors.BadRequest("WECHAT_PLATFORM_NOT_READY", "wechat platform is not ready")
	// ErrPlatformInUse: a non-withdrawn WeChat App is still authorized
	// through this platform, so the platform cannot be withdrawn (ADR 0029).
	ErrPlatformInUse = errors.Conflict("WECHAT_PLATFORM_IN_USE", "wechat platform still authorizes an app")
	// ErrMasterKeyMissing: WECHAT_MASTER_KEY is absent or malformed
	// (ADR 0018, ADR 0029) — registration and secret replacement refuse
	// to operate rather than degrade to plaintext.
	ErrMasterKeyMissing = errors.InternalServer("WECHAT_MASTER_KEY_MISSING", "wechat master key is not configured")
	// ErrSecretCrypto: the stored ciphertext failed to decrypt or
	// authenticate — corrupted rows or a wrong master key.
	ErrSecretCrypto = errors.InternalServer("WECHAT_SECRET_CRYPTO_ERROR", "wechat secret decryption failed")
	// ErrWechatStoreUnavailable: a wechat-store operation failed
	// unexpectedly (connectivity, deadlock) — the raw error is logged at
	// the handling site and this sentinel crosses the boundary instead
	// (ADR 0007).
	ErrWechatStoreUnavailable = errors.ServiceUnavailable("WECHAT_STORE_UNAVAILABLE", "wechat store unavailable")
	// ErrPushRejected: the push is plaintext, compatible-mode, or failed
	// verification. Nothing the operator reads changes.
	ErrPushRejected = errors.BadRequest("WECHAT_PUSH_REJECTED", "wechat push was rejected")
	// ErrAppNotFound: the requested WeChat App does not exist or is
	// withdrawn.
	ErrAppNotFound = errors.NotFound("WECHAT_APP_NOT_FOUND", "wechat app not found")
	// ErrAppInvalid: a WeChat App parameter is invalid — blank name,
	// blank AppID, missing AppSecret on a Direct Hold, an official
	// account without a Service Account or Subscription Account kind, a
	// kind on a mini program, or an over-length value.
	ErrAppInvalid = errors.BadRequest("WECHAT_APP_INVALID", "wechat app parameters are invalid")
	// ErrAppDuplicate: an app that is not withdrawn already holds this
	// AppID (ADR 0029). This is independent of component-AppID uniqueness
	// on WeChat Platforms.
	ErrAppDuplicate = errors.Conflict("WECHAT_APP_DUPLICATE", "app id already exists")
	// ErrAppIDImmutable: the AppID is fixed for the life of the row
	// (ADR 0029).
	ErrAppIDImmutable = errors.BadRequest("WECHAT_APP_ID_IMMUTABLE", "app id cannot be changed")
	// ErrAppLapsed: WeChat has definitively refused this app, so
	// connectivity testing is refused until a replacement AppSecret or a
	// new grant on the same platform restores it.
	ErrAppLapsed = errors.BadRequest("WECHAT_APP_LAPSED", "wechat app has lapsed")
	// ErrWebAuthorizationRefused: Web Authorization cannot start, or the
	// operator settings do not belong on this app. A subscription
	// account, a mini program, a grant without the capability, a
	// suspended app, or an app with no return URL is refused locally.
	ErrWebAuthorizationRefused = errors.BadRequest("WECHAT_WEB_AUTHORIZATION_REFUSED", "web authorization cannot start")
	// ErrHandoffCodeInvalid: the Handoff Code was never issued or was
	// already redeemed. A second redeem cannot observe the identity.
	ErrHandoffCodeInvalid = errors.BadRequest("WECHAT_HANDOFF_CODE_INVALID", "handoff code is invalid")
)
