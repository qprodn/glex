package fileinfra

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"glex/internal/conf"
	"glex/internal/domain/file"
	"glex/internal/domain/file/storage"
)

// DefaultMaxUploadSize is the accept cap when the config leaves
// max_size_bytes unset: 100 MiB (ADR 0008 sizes the relay against this cap).
const DefaultMaxUploadSize int64 = 100 << 20

// DefaultAllowMIME is the content-type whitelist when the config leaves
// allow_mime empty: images, video, audio, text, and the common document and
// archive types (the go-wind-admin default set, minus per-cloud flavors).
var DefaultAllowMIME = []string{
	"image/*",
	"video/*",
	"audio/*",
	"text/plain",
	"text/csv",
	"application/json",
	"application/pdf",
	"application/zip",
	"application/x-zip-compressed",
	"application/gzip",
	"application/x-7z-compressed",
	"application/x-rar-compressed",
	"application/msword",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"application/vnd.ms-excel",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"application/vnd.ms-powerpoint",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation",
}

// objectStorage is the S3-protocol implementation of the storage port
// (ADR 0007: one aws-sdk-go-v2 client, one statically configured bucket).
type objectStorage struct {
	client    *s3.Client
	bucket    string
	maxSize   int64
	allowMIME []string
}

// NewObjectStorage builds the S3 client from the static file config. The
// endpoint is scheme-joined with use_ssl; path_style addresses MinIO and
// most self-hosted S3-compatible stores.
func NewObjectStorage(c *conf.File) (storage.ObjectStorage, error) {
	cfg := c.GetS3()
	scheme := "https"
	if !cfg.GetUseSsl() {
		scheme = "http"
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(cfg.GetRegion()),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.GetAccessKey(), cfg.GetSecretKey(), "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(scheme + "://" + cfg.GetEndpoint())
		o.UsePathStyle = cfg.GetPathStyle()
	})
	maxSize := int64(c.GetMaxSizeBytes())
	if maxSize <= 0 {
		maxSize = DefaultMaxUploadSize
	}
	allow := DefaultAllowMIME
	if len(c.GetAllowMime()) > 0 {
		allow = c.GetAllowMime()
	}
	return &objectStorage{
		client:    client,
		bucket:    cfg.GetBucket(),
		maxSize:   maxSize,
		allowMIME: allow,
	}, nil
}

func (s *objectStorage) Bucket() string {
	return s.bucket
}

// EnsureBucket creates the bucket when missing so a fresh MinIO needs no
// manual provisioning (both reference repos auto-create).
func (s *objectStorage) EnsureBucket(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &s.bucket})
	if err == nil {
		return nil
	}
	if !isStatus(err, 404) {
		return wrapStorage(err)
	}
	_, err = s.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &s.bucket})
	if err != nil && !isBucketAlreadyOwned(err) {
		return wrapStorage(err)
	}
	return nil
}

// Put streams the bytes to the Stored Key while hashing them — the bytes are
// never fully buffered in memory (ADR 0008). Acceptance policy is enforced
// here, where the static config lives: the size cap and the whitelist
// checked against the sniffed content type the caller provides.
func (s *objectStorage) Put(ctx context.Context, key string, content io.Reader, size int64, contentType string) (string, error) {
	if size > s.maxSize {
		return "", file.ErrFileTooLarge
	}
	if !mimeAllowed(contentType, s.allowMIME) {
		return "", file.ErrFileMimeNotAllowed
	}
	digest := sha256.New()
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          io.TeeReader(content, digest),
		ContentLength: aws.Int64(size),
		ContentType:   aws.String(contentType),
	})
	if err != nil {
		return "", wrapStorage(err)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func (s *objectStorage) Get(ctx context.Context, key string, rng *storage.ByteRange) (*storage.Object, error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}
	if rng != nil && rng.Header != "" {
		// The raw single-range header passes through; S3 rejects what it
		// cannot satisfy and the 416 maps to the domain error.
		input.Range = aws.String(rng.Header)
	}
	out, err := s.client.GetObject(ctx, input)
	if err != nil {
		if isStatus(err, 404) {
			return nil, file.ErrFileNotFound
		}
		if isStatus(err, 416) {
			return nil, file.ErrFileRangeNotSatisfiable
		}
		return nil, wrapStorage(err)
	}
	return &storage.Object{
		Body:          out.Body,
		ContentType:   aws.ToString(out.ContentType),
		ContentLength: aws.ToInt64(out.ContentLength),
		ContentRange:  aws.ToString(out.ContentRange),
	}, nil
}

// Delete removes the bytes; S3 deletes are idempotent, so a missing key
// succeeds — withdrawal stays best-effort (ADR 0009).
func (s *objectStorage) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return wrapStorage(err)
	}
	return nil
}

// mimeAllowed reports whether a sniffed content type passes the whitelist:
// an entry ending in "/*" admits its whole type ("image/*" → "image/png"),
// any other entry is an exact match. Parameters are stripped first —
// http.DetectContentType returns parameterized values for text types
// ("text/plain; charset=utf-8") while whitelist entries are bare media
// types.
func mimeAllowed(mimeType string, allow []string) bool {
	if mimeType == "" {
		return false
	}
	if i := strings.IndexByte(mimeType, ';'); i >= 0 {
		mimeType = strings.TrimSpace(mimeType[:i])
	}
	for _, entry := range allow {
		if entry == mimeType {
			return true
		}
		if strings.HasSuffix(entry, "/*") && strings.HasPrefix(mimeType, strings.TrimSuffix(entry, "*")) {
			return true
		}
	}
	return false
}

// isStatus reports whether the smithy error wraps the given HTTP status.
func isStatus(err error, status int) bool {
	var re *awshttp.ResponseError
	if errors.As(err, &re) {
		return re.HTTPStatusCode() == status
	}
	var nfe *s3types.NotFound
	if status == 404 && errors.As(err, &nfe) {
		return true
	}
	return false
}

// isBucketAlreadyOwned swallows the "already exists" flavors of
// CreateBucket — MinIO and AWS report them differently (409 vs the typed
// BucketAlreadyOwnedByYou) and both are success for EnsureBucket.
func isBucketAlreadyOwned(err error) bool {
	var owned *s3types.BucketAlreadyOwnedByYou
	if errors.As(err, &owned) {
		return true
	}
	return isStatus(err, 409)
}

// wrapStorage carries the SDK error as the cause behind the module's
// upstream-failure reason; the details stay in the server log, not the
// client response.
func wrapStorage(err error) error {
	return file.ErrFileStorage.WithCause(err)
}
