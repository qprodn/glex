package entity

import (
	"time"

	"glex/internal/domain/param"
)

// Config is a Runtime Parameter domain object (运行时参数), ported from RuoYi
// sys_config. The key identifies the parameter to every consumer and —
// unlike a Dict Type key — stays editable after create: it is referenced
// only by consumer code, never by rows, so a rename surfaces as a
// consumer-side not-found instead of dangling data (docs/modules/param.md).
// BuiltIn governs withdrawability only: a built-in parameter may be re-valued
// at will but never withdrawn.
type Config struct {
	ID          int64
	ConfigName  string
	ConfigKey   string
	ConfigValue string
	BuiltIn     bool
	Remark      string
	CreateBy    int64
	UpdateBy    int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Validate enforces the invariants a Config must satisfy before persistence:
// non-blank name and key inside the charset and column budgets, value and
// remark inside theirs.
func (c *Config) Validate() error {
	if c == nil ||
		c.ConfigName == "" || len(c.ConfigName) > param.NameMaxLen ||
		!param.KeyPattern.MatchString(c.ConfigKey) || len(c.ConfigKey) > param.KeyMaxLen ||
		len(c.ConfigValue) > param.ValueMaxLen ||
		len(c.Remark) > param.RemarkMaxLen {
		return param.ErrConfigInvalid
	}
	return nil
}
