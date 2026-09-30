package param

import "regexp"

// KeyPattern is the charset a config key must match: a letter or digit first,
// then letters, digits, dots, underscores, or hyphens — loose enough for
// RuoYi's dotted keys (sys.account.registerUser), tight enough to stay a
// code-referenceable identifier.
var KeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Field budgets, kept in step with the table's column lengths so an
// over-length value fails validation instead of the insert.
const (
	NameMaxLen   = 256
	KeyMaxLen    = 256
	ValueMaxLen  = 500
	RemarkMaxLen = 500
)
