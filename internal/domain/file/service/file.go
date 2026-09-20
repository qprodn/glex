package service

import (
	"context"
	"io"
	"log/slog"
	"strconv"
	"time"

	"glex/internal/domain/file"
	"glex/internal/domain/file/entity"
	"glex/internal/domain/file/repository"
	"glex/internal/domain/file/storage"
	"glex/pkg/listq"
	"glex/pkg/logx"
)

// AcceptRequest is one relayed upload: the validated Upload record, the
// byte stream behind it, and the actor stamped into the audit fields
// (ADR 0025).
type AcceptRequest struct {
	Upload  *entity.Upload
	Content io.Reader
	ActorID int64
}

// WithdrawRequest is one Stored File withdrawal: the row to withdraw and
// the actor performing it (ADR 0025).
type WithdrawRequest struct {
	ID      int64
	ActorID int64
}

// OpenRequest is one byte read: the Stored File to open, optionally one
// HTTP range on its bytes (nil = full body) (ADR 0025).
type OpenRequest struct {
	ID    int64
	Range *storage.ByteRange
}

// OpenResult is one opened transfer: the byte stream and the Stored File
// row it belongs to (ADR 0025 — the resolved pair rides one struct). The
// caller owns Object.Body and must close it.
type OpenResult struct {
	Object *storage.Object
	File   *entity.StoredFile
}

// File is the file domain service behind the file endpoints: the only path
// to the Stored File rows and their bytes (ADR 0005). The S3 protocol
// carries the bytes (ADR 0007); every transfer flows through the service
// (ADR 0008); withdrawal keeps the row and removes the bytes (ADR 0009).
// The module has no multi-statement write, so no ambient transaction is
// needed — each write is a single-row statement. Every error a method
// returns to its caller first logs its Warn breadcrumb (ADR 0011).
type File interface {
	Get(ctx context.Context, id int64) (*entity.StoredFile, error)
	// BatchGet resolves a batch of ids to active Stored Files; unknown or
	// withdrawn ids are absent from the result (batch semantics — callers
	// render what resolved).
	BatchGet(ctx context.Context, ids []int64) ([]*entity.StoredFile, error)
	List(ctx context.Context, q listq.Query) ([]*entity.StoredFile, error)
	// Accept validates the upload (req.Upload), mints the Stored Key,
	// streams req.Content to storage while computing the Content
	// Fingerprint, then records the row with req.ActorID in the audit
	// fields. A row failure after the bytes landed leaves an orphan object —
	// logged, never a live row pointing at missing bytes.
	Accept(ctx context.Context, req AcceptRequest) (*entity.StoredFile, error)
	// Withdraw soft-deletes the row req.ID (stamping req.ActorID), then
	// removes the bytes best-effort (ADR 0009): a failed object removal
	// leaves an invisible orphan — logged — and the withdrawal still
	// succeeds.
	Withdraw(ctx context.Context, req WithdrawRequest) error
	// Open locates the Stored File req.ID and opens its bytes, optionally
	// one req.Range. The caller owns the returned result's Object.Body.
	Open(ctx context.Context, req OpenRequest) (*OpenResult, error)
	// DisplayURL mints one Display URL (ADR 0016) — a short-TTL presigned
	// GET for the row's Stored Key. Signing is local; the failure mode is
	// upstream-reachability reasons only.
	DisplayURL(ctx context.Context, f *entity.StoredFile) (string, error)
}

type fileImpl struct {
	files  repository.FileRepo
	store  storage.ObjectStorage
	logger *slog.Logger
}

// NewFileService creates the file domain service. files backs the metadata
// rows; store carries the bytes; logger records the orphan cases ADR 0009
// and the Accept ordering call out.
func NewFileService(files repository.FileRepo, store storage.ObjectStorage, logger *slog.Logger) File {
	return &fileImpl{
		files:  files,
		store:  store,
		logger: logger.With(slog.String("component", "domain.file.svc")),
	}
}

func (s *fileImpl) Get(ctx context.Context, id int64) (*entity.StoredFile, error) {
	f, err := s.files.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "file.svc.get", err)
		return nil, err
	}
	return f, nil
}

func (s *fileImpl) BatchGet(ctx context.Context, ids []int64) ([]*entity.StoredFile, error) {
	fs, err := s.files.FindByIDs(ctx, ids)
	if err != nil {
		logx.Propagate(ctx, s.logger, "file.svc.batch_get", err)
		return nil, err
	}
	return fs, nil
}

func (s *fileImpl) List(ctx context.Context, q listq.Query) ([]*entity.StoredFile, error) {
	fs, err := s.files.List(ctx, q)
	if err != nil {
		logx.Propagate(ctx, s.logger, "file.svc.list", err)
		return nil, err
	}
	return fs, nil
}

func (s *fileImpl) Accept(ctx context.Context, req AcceptRequest) (*entity.StoredFile, error) {
	up := req.Upload
	if err := up.Validate(); err != nil {
		logx.Propagate(ctx, s.logger, "file.svc.accept", err)
		return nil, err
	}
	// One snowflake serves both the row ID and the Stored Key so they
	// correlate; the repo trusts the entity's ID on create.
	id := s.files.NextID()
	suffix := file.NormalizeSuffix(up.OriginalName)
	key := storedKey(id, up.BizPath, suffix, time.Now())
	digest, err := s.store.Put(ctx, storage.PutRequest{
		Key:         key,
		Content:     req.Content,
		Size:        up.SizeBytes,
		ContentType: up.MIMEType,
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "file.svc.accept", err)
		return nil, err
	}
	f := &entity.StoredFile{
		ID:           id,
		OriginalName: up.OriginalName,
		Suffix:       suffix,
		MIMEType:     up.MIMEType,
		SizeBytes:    up.SizeBytes,
		Bucket:       s.store.Bucket(),
		ObjKey:       key,
		ContentHash:  digest,
		CreateBy:     req.ActorID,
		UpdateBy:     req.ActorID,
	}
	created, err := s.files.Create(ctx, f)
	if err != nil {
		s.logger.Warn("orphan object: bytes landed but the row insert failed",
			slog.String("bucket", f.Bucket),
			slog.String("obj_key", f.ObjKey),
			slog.Any("error", err),
		)
		logx.Propagate(ctx, s.logger, "file.svc.accept", err)
		return nil, err
	}
	return created, nil
}

func (s *fileImpl) Withdraw(ctx context.Context, req WithdrawRequest) error {
	f, err := s.files.FindByID(ctx, req.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "file.svc.withdraw", err)
		return err
	}
	if err := s.files.Delete(ctx, repository.DeleteRequest{ID: req.ID, ActorID: req.ActorID}); err != nil {
		logx.Propagate(ctx, s.logger, "file.svc.withdraw", err)
		return err
	}
	// Row first, bytes best-effort (ADR 0009): the withdrawal stands; a
	// failed removal leaves an invisible orphan for an operator, never a
	// live row pointing at missing bytes.
	if err := s.store.Delete(ctx, f.ObjKey); err != nil {
		s.logger.Warn("orphan object: row withdrawn but byte removal failed",
			slog.String("bucket", f.Bucket),
			slog.String("obj_key", f.ObjKey),
			slog.Any("error", err),
		)
	}
	return nil
}

func (s *fileImpl) Open(ctx context.Context, req OpenRequest) (*OpenResult, error) {
	f, err := s.files.FindByID(ctx, req.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "file.svc.open", err)
		return nil, err
	}
	obj, err := s.store.Get(ctx, storage.GetRequest{Key: f.ObjKey, Range: req.Range})
	if err != nil {
		logx.Propagate(ctx, s.logger, "file.svc.open", err)
		return nil, err
	}
	return &OpenResult{Object: obj, File: f}, nil
}

func (s *fileImpl) DisplayURL(ctx context.Context, f *entity.StoredFile) (string, error) {
	url, err := s.store.PresignGet(ctx, f.ObjKey)
	if err != nil {
		logx.Propagate(ctx, s.logger, "file.svc.display_url", err)
		return "", err
	}
	return url, nil
}

// storedKey builds the Stored Key: [<biz>/]yyyy/MM/dd/<snowflake>.<suffix>.
// Date directories keep the prefix navigable and purgable by period; the
// snowflake name cannot collide and correlates with the row ID.
func storedKey(id int64, bizPath, suffix string, now time.Time) string {
	name := strconv.FormatInt(id, 10)
	if suffix != "" {
		name += "." + suffix
	}
	key := now.Format("2006/01/02") + "/" + name
	if bizPath != "" {
		key = bizPath + "/" + key
	}
	return key
}
