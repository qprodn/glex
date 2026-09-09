// Package model holds the dict contract's flat record types — the shapes
// consumers see, never the provider's domain entities (ADR 0005).
package model

// Entry is one labeled option: the machine value a stored column carries
// and the label a rendered cell shows.
type Entry struct {
	Value string
	Label string
}
