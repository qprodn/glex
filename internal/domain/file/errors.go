// Package file owns the file module per ADR 0005: Stored Files — the uploads
// the back office accepts, the downloads it serves, and their withdrawal.
// The package root holds only the typed errors; the objects live in entity,
// the row port in repository, the byte port in storage, and the module's own
// rules in service.
package file

import (
	"github.com/go-kratos/kratos/v3/errors"
)

var (
	// ErrFileNotFound: the requested Stored File does not exist (or is
	// soft-deleted), or its bytes are gone from storage.
	ErrFileNotFound = errors.NotFound("FILE_NOT_FOUND", "stored file not found")
	// ErrFileInvalid: an intake or download parameter is invalid — blank
	// original name, unsafe business path, non-positive size, or a malformed
	// disposition or file id.
	ErrFileInvalid = errors.BadRequest("FILE_INVALID", "file parameters are invalid")
	// ErrFileNoUpload: the multipart request carries no "file" part.
	ErrFileNoUpload = errors.BadRequest("FILE_NO_UPLOAD", `multipart form field "file" is required`)
	// ErrFileTooLarge: the upload exceeds the configured size cap.
	ErrFileTooLarge = errors.New(413, "FILE_TOO_LARGE", "upload exceeds the configured size cap")
	// ErrFileMimeNotAllowed: the sniffed content type is outside the
	// configured whitelist.
	ErrFileMimeNotAllowed = errors.BadRequest("FILE_MIME_NOT_ALLOWED", "content type is not allowed")
	// ErrFileRangeNotSatisfiable: the requested byte range is invalid for
	// this object.
	ErrFileRangeNotSatisfiable = errors.New(416, "FILE_RANGE_NOT_SATISFIABLE", "requested range not satisfiable")
	// ErrFileStorage: object storage rejected the request (connectivity,
	// credentials, bucket) — bytes never landed or could not be read.
	ErrFileStorage = errors.New(502, "FILE_STORAGE_ERROR", "object storage request failed")
)
