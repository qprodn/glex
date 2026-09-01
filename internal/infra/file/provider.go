package fileinfra

import (
	"context"
	"log/slog"

	"glex/internal/domain/file/storage"

	"go.uber.org/fx"
)

// Module contains file persistence adapters: the Stored File repo and the
// S3-protocol object storage client (ADR 0007).
var Module = fx.Module("file-infra",
	fx.Provide(
		NewFileRepo,
		NewObjectStorage,
	),
	fx.Invoke(ensureBucketOnStart),
)

// ensureBucketOnStart provisions the configured bucket at startup and fails
// fast when storage is unreachable — the same posture as the Redis ping in
// the store module.
func ensureBucketOnStart(lc fx.Lifecycle, store storage.ObjectStorage, logger *slog.Logger) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := store.EnsureBucket(ctx); err != nil {
				return err
			}
			logger.Info("object storage bucket ready", slog.String("bucket", store.Bucket()))
			return nil
		},
	})
}
