// Package wechat owns the wechat module per ADR 0005 and ADR 0029: the
// WeChat Platforms this back office registers, and later the WeChat Apps
// connected through them. The package root holds only the typed errors;
// the objects live in entity, the row ports in repository, and the
// module's own rules in service.
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
)
