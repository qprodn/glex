// Package param owns the param module per ADR 0005: Runtime Parameters — the
// named system-wide values administrators tune without a redeploy, ported
// from RuoYi sys_config. The package root holds only the typed errors; the
// objects live in entity, the ports in repository, and the module's own
// rules in service.
package param

import (
	"github.com/go-kratos/kratos/v3/errors"
)

var (
	// ErrConfigNotFound: the requested config does not exist (or is
	// soft-deleted).
	ErrConfigNotFound = errors.NotFound("CONFIG_NOT_FOUND", "config not found")
	// ErrConfigKeyConflict: the config_key already exists among active
	// parameters (RuoYi: 参数键名已存在).
	ErrConfigKeyConflict = errors.Conflict("CONFIG_KEY_CONFLICT", "config_key already exists")
	// ErrConfigBuiltInLocked: the parameter is built-in and cannot be deleted
	// (RuoYi: 内置参数不能删除). Re-valuing stays allowed.
	ErrConfigBuiltInLocked = errors.Conflict("CONFIG_BUILT_IN_LOCKED", "built-in config cannot be deleted")
	// ErrConfigInvalid: config_name/config_key is blank or over length, the
	// key violates the charset, or config_value/remark is over length.
	ErrConfigInvalid = errors.BadRequest("CONFIG_INVALID", "config_name and config_key are required; config_key must match ^[A-Za-z0-9][A-Za-z0-9._-]*$")
)

var (
	// ErrConfigStoreUnavailable: a param-store operation failed unexpectedly —
	// driver down, tx failure, serialization. Adapters log the raw error once
	// and return this sentinel (ADR 0007), so no driver text crosses the
	// repository boundary; clients see 503, never a driver message.
	ErrConfigStoreUnavailable = errors.ServiceUnavailable("CONFIG_STORE_UNAVAILABLE", "param store unavailable")
)
