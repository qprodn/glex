package entity

import (
	"strings"
	"time"
	"unicode/utf8"

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
// a non-blank name (whitespace-only counts as blank, RuoYi's @NotBlank), a
// key inside the charset, and every free-text field inside its column
// budget. Free-text budgets count runes, not bytes — the columns are
// varchar(n) counting characters, and this is a zh-CN back office; the key
// needs no rune counting because its charset is ASCII-only.
func (c *Config) Validate() error {
	if c == nil ||
		strings.TrimSpace(c.ConfigName) == "" || utf8.RuneCountInString(c.ConfigName) > param.NameMaxLen ||
		!param.KeyPattern.MatchString(c.ConfigKey) || len(c.ConfigKey) > param.KeyMaxLen ||
		utf8.RuneCountInString(c.ConfigValue) > param.ValueMaxLen ||
		utf8.RuneCountInString(c.Remark) > param.RemarkMaxLen {
		return param.ErrConfigInvalid
	}
	return nil
}
