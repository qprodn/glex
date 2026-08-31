package dict

import "regexp"

// TypeKeyPattern is the charset a Dict Type key must match (RuoYi's
// DICTIONARY_TYPE regex): a lowercase letter first, then lowercase letters,
// digits, or underscores.
var TypeKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// TagStyles are the tag color presets a Dict Entry may render with (RuoYi
// list_class; css_class was dropped as an unused escape hatch).
var TagStyles = map[string]bool{
	"default": true,
	"primary": true,
	"success": true,
	"warning": true,
	"danger":  true,
	"info":    true,
}

// DefaultTagStyle normalizes an absent style (proto TAG_STYLE_UNSPECIFIED)
// to the plain preset.
const DefaultTagStyle = "default"
