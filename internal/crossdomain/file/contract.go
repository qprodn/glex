// Package file publishes the file module's cross-module contracts in the
// anti-corruption shape of ADR 0005: contract.go holds the consumer-shaped
// interface, impl/ adapts the provider's domain capability, and filemock
// holds the mockgen-generated test double. Contracts are consumed by
// constructor injection into the calling application service; cmd stays the
// only composition root.
package file

import (
	"context"
)

//go:generate go tool mockgen -package filemock -destination filemock/url_resolver.go glex/internal/crossdomain/file URLResolver

// URLResolver exposes the file module's Display URL reads (ADR 0016) to
// modules that hold Stored File references — the avatar today.
type URLResolver interface {
	// ResolveURLs maps Stored File ids to freshly minted Display URLs.
	// Unknown or withdrawn ids are absent from the result — a reference
	// that no longer resolves renders its default, it never fails the
	// read. Signing failures skip the id the same way; the skip is logged
	// at this boundary, not propagated.
	ResolveURLs(ctx context.Context, fileIDs []int64) (map[int64]string, error)
}
