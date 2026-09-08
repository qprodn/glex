package impl

import (
	"context"
	"log/slog"

	"glex/internal/crossdomain/file"
	fileservice "glex/internal/domain/file/service"
)

// urlResolver satisfies the file Display URL contract by delegating to the
// provider's file domain service: the Stored File rows and the presigning
// live there, beside the module's byte policy.
type urlResolver struct {
	files  fileservice.File
	logger *slog.Logger
}

// NewURLResolver adapts the file domain service to the crossdomain Display
// URL contract.
func NewURLResolver(files fileservice.File, logger *slog.Logger) file.URLResolver {
	return &urlResolver{
		files:  files,
		logger: logger.With(slog.String("component", "crossdomain.file")),
	}
}

// ResolveURLs maps ids to fresh Display URLs. Row lookups carry batch
// semantics — unknown or withdrawn ids resolve to nothing. A signing
// failure skips just that id: the breadcrumb the domain service already
// logged covers visibility, and one sour id must not blank every avatar
// in a user list (ADR 0016 — display degrades, data reads do not).
func (r *urlResolver) ResolveURLs(ctx context.Context, fileIDs []int64) (map[int64]string, error) {
	urls := make(map[int64]string, len(fileIDs))
	files, err := r.files.BatchGet(ctx, fileIDs)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		url, err := r.files.DisplayURL(ctx, f)
		if err != nil {
			continue
		}
		urls[f.ID] = url
	}
	return urls, nil
}
