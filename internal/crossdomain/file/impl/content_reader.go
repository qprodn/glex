package impl

import (
	"context"
	"io"
	"log/slog"

	"glex/internal/crossdomain/file"
	fileservice "glex/internal/domain/file/service"
)

// contentReader satisfies the file content contract by delegating to the
// provider's file domain service: Open locates the row and streams the
// bytes (ADR 0008), beside the module's byte policy.
type contentReader struct {
	files  fileservice.File
	logger *slog.Logger
}

// NewContentReader adapts the file domain service to the crossdomain
// content-read contract.
func NewContentReader(files fileservice.File, logger *slog.Logger) file.ContentReader {
	return &contentReader{
		files:  files,
		logger: logger.With(slog.String("component", "crossdomain.file")),
	}
}

// OpenContent streams an active Stored File's bytes in full. The row lookup
// and the storage read already logged their breadcrumbs in the domain
// service; errors cross with their file reasons (FILE_NOT_FOUND for unknown
// or withdrawn ids).
func (r *contentReader) OpenContent(ctx context.Context, fileID int64) (io.ReadCloser, error) {
	opened, err := r.files.Open(ctx, fileservice.OpenRequest{ID: fileID})
	if err != nil {
		return nil, err
	}
	return opened.Object.Body, nil
}
