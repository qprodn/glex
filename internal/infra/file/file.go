package fileinfra

import (
	"context"
	"time"

	"github.com/go-kratos/aip-go/ents"
	"glex/internal/domain/file"
	fileentity "glex/internal/domain/file/entity"
	filerepo "glex/internal/domain/file/repository"
	"glex/internal/infra/ent"
	entfile "glex/internal/infra/ent/file"
	"glex/internal/infra/store"
)

// toBizFile maps an ent File PO to a fileentity.StoredFile DO.
func toBizFile(po *ent.File) *fileentity.StoredFile {
	return &fileentity.StoredFile{
		ID:           po.ID,
		OriginalName: po.OriginalName,
		Suffix:       po.Suffix,
		MIMEType:     po.MimeType,
		SizeBytes:    po.SizeBytes,
		Bucket:       po.Bucket,
		ObjKey:       po.ObjKey,
		ContentHash:  po.ContentHash,
		CreateBy:     po.CreateBy,
		UpdateBy:     po.UpdateBy,
		CreatedAt:    po.CreatedAt,
		UpdatedAt:    po.UpdatedAt,
	}
}

type fileRepo struct {
	data *store.Data
}

// NewFileRepo creates a new FileRepo instance.
func NewFileRepo(data *store.Data) filerepo.FileRepo {
	return &fileRepo{data: data}
}

func (r *fileRepo) NextID() int64 {
	return newSnowflakeID()
}

func (r *fileRepo) FindByID(ctx context.Context, id int64) (*fileentity.StoredFile, error) {
	po, err := r.data.DB.File(ctx).Query().
		Where(entfile.IDEQ(id), entfile.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, file.ErrFileNotFound
		}
		return nil, err
	}
	return toBizFile(po), nil
}

func (r *fileRepo) List(ctx context.Context, opts ...filerepo.ListOption) ([]*fileentity.StoredFile, error) {
	o := filerepo.ListOptions{Limit: 20}
	for _, opt := range opts {
		opt(&o)
	}
	pos, err := r.data.DB.File(ctx).Query().
		Where(
			entfile.DeletedAtIsNil(),
			ents.ApplyFilter(o.Filter),
		).
		Order(ents.ApplyOrderBy(o.OrderBy)).
		Offset(o.Offset).
		Limit(o.Limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	files := make([]*fileentity.StoredFile, 0, len(pos))
	for _, po := range pos {
		files = append(files, toBizFile(po))
	}
	return files, nil
}

// Create inserts the Stored File row. The ID was minted by the domain
// service (it builds the Stored Key from it) and is trusted here.
func (r *fileRepo) Create(ctx context.Context, f *fileentity.StoredFile) (*fileentity.StoredFile, error) {
	po, err := r.data.DB.File(ctx).Create().
		SetID(f.ID).
		SetOriginalName(f.OriginalName).
		SetSuffix(f.Suffix).
		SetMimeType(f.MIMEType).
		SetSizeBytes(f.SizeBytes).
		SetBucket(f.Bucket).
		SetObjKey(f.ObjKey).
		SetContentHash(f.ContentHash).
		SetCreateBy(f.CreateBy).
		SetUpdateBy(f.UpdateBy).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return toBizFile(po), nil
}

// Delete soft-deletes the row (ADR 0002 — no row is hard-deleted), stamping
// actorID into the audit fields. The bytes are removed separately by the
// domain service (ADR 0009).
func (r *fileRepo) Delete(ctx context.Context, id, actorID int64) error {
	err := r.data.DB.File(ctx).UpdateOneID(id).
		SetDeletedAt(time.Now()).
		SetUpdateBy(actorID).
		Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return file.ErrFileNotFound
		}
		return err
	}
	return nil
}
