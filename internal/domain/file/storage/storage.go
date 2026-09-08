// Package storage declares the file module's byte port (the inversion seam
// declared in domain, implemented in infra over the S3 protocol — ADR 0007).
// Rows live in PostgreSQL behind the repository port; bytes live here.
package storage

import (
	"context"
	"io"
)

// ByteRange is one HTTP range request for the stored bytes ("bytes=0-1023",
// "bytes=1024-", "bytes=-512"): the raw header value passes through and the
// implementation rejects what it cannot satisfy.
type ByteRange struct {
	Header string
}

// Object is an opened byte stream. The caller owns Body and must close it.
type Object struct {
	Body          io.ReadCloser
	ContentType   string
	ContentLength int64
	// ContentRange is set only for a range response ("bytes start-end/size");
	// empty means the full body.
	ContentRange string
}

// ObjectStorage is the byte-level gateway behind the file domain service.
type ObjectStorage interface {
	// Bucket reports the single configured bucket (ADR 0007).
	Bucket() string
	// EnsureBucket creates the bucket when missing; startup fails fast when
	// storage is unreachable.
	EnsureBucket(ctx context.Context) error
	// Put streams content to the Stored Key under a known size and returns
	// the bytes' SHA-256 hex digest (the Content Fingerprint), computed
	// while streaming — the bytes are never fully buffered (ADR 0008).
	// Acceptance policy (size cap, content-type whitelist) is enforced here,
	// where the static config lives.
	Put(ctx context.Context, key string, content io.Reader, size int64, contentType string) (digest string, err error)
	// Get fetches the bytes, optionally one range (nil = full body). The
	// caller owns the returned Object's Body.
	Get(ctx context.Context, key string, rng *ByteRange) (*Object, error)
	// PresignGet mints a short-TTL presigned GET URL for the Stored Key's
	// bytes — a Display URL (ADR 0016). Signing is a local computation (no
	// storage round-trip); the TTL is the adapter's configured deployment
	// concern.
	PresignGet(ctx context.Context, key string) (url string, err error)
	// Delete removes the bytes; it is idempotent — a missing key succeeds.
	Delete(ctx context.Context, key string) error
}
