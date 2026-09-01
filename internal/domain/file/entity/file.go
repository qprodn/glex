package entity

import (
	"strings"
	"time"

	"glex/internal/domain/file"
)

// StoredFile is a Stored File domain object (存储文件): the metadata row
// paired with the bytes in object storage. Withdrawing soft-deletes the row
// and removes the bytes (ADR 0009) — a withdrawn Stored File can be
// remembered, never downloaded again.
type StoredFile struct {
	ID           int64
	OriginalName string
	Suffix       string
	MIMEType     string
	SizeBytes    int64
	Bucket       string
	ObjKey       string
	ContentHash  string
	CreateBy     int64
	UpdateBy     int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Upload is the intake view of a Stored File: what one accepted upload
// carries before the service mints the Stored Key and the Content
// Fingerprint. MIMEType is sniffed from the bytes — a client's declared
// Content-Type never reaches it.
type Upload struct {
	OriginalName string
	BizPath      string
	MIMEType     string
	SizeBytes    int64
}

// Validate enforces the invariants an Upload must satisfy before its bytes
// are accepted: a non-blank bounded original name, a safe optional business
// directory prefix, and a positive size. Validate normalizes BizPath
// (trailing slash trimmed) in place.
func (u *Upload) Validate() error {
	if u == nil || u.OriginalName == "" || len(u.OriginalName) > file.MaxOriginalNameLength || u.SizeBytes <= 0 {
		return file.ErrFileInvalid
	}
	u.BizPath = strings.TrimSuffix(u.BizPath, "/")
	if u.BizPath != "" && (len(u.BizPath) > file.MaxBizPathLength || !file.BizPathPattern.MatchString(u.BizPath)) {
		return file.ErrFileInvalid
	}
	return nil
}
