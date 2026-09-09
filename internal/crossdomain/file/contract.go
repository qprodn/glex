// Package file publishes the file module's cross-module contracts in the
// anti-corruption shape of ADR 0005: contract.go holds the consumer-shaped
// interface, impl/ adapts the provider's domain capability, and filemock
// holds the mockgen-generated test double. Contracts are consumed by
// constructor injection into the calling application service; cmd stays the
// only composition root.
package file

import (
	"context"
	"io"
)

//go:generate go tool mockgen -package filemock -destination filemock/url_resolver.go glex/internal/crossdomain/file URLResolver
//go:generate go tool mockgen -package filemock -destination filemock/content_reader.go glex/internal/crossdomain/file ContentReader

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

// ContentReader exposes one Stored File's bytes to modules consuming them
// as Import File sources (ADR 0017): Data Import reads the workbook the
// operator uploaded, in-process, without a presigned round-trip. Errors
// cross this seam as the file domain's kratos errors (FILE_NOT_FOUND for
// unknown or withdrawn ids) — the wire shape is the contract's.
type ContentReader interface {
	// OpenContent opens the Stored File's bytes in full; the caller owns
	// closing the body.
	OpenContent(ctx context.Context, fileID int64) (io.ReadCloser, error)
}
