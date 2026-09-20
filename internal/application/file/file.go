package file

import (
	"context"
	"io"
	"log/slog"
	stdhttp "net/http"
	"strings"

	filev1 "glex/api/file/v1"
	"glex/internal/bizpkg"
	"glex/internal/domain/file"
	fileentity "glex/internal/domain/file/entity"
	fileservice "glex/internal/domain/file/service"
	"glex/internal/domain/file/storage"
	"glex/pkg/listq"
	"glex/pkg/logx"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// convertStoredFile maps a fileentity.StoredFile DO to the proto StoredFile
// DTO. The Display URL is not mapped here — it is derived at read time and
// attached by fillURL, so the mapper stays a pure field copy.
func convertStoredFile(f *fileentity.StoredFile) *filev1.StoredFile {
	return &filev1.StoredFile{
		FileId:       f.ID,
		OriginalName: f.OriginalName,
		Suffix:       f.Suffix,
		MimeType:     f.MIMEType,
		SizeBytes:    f.SizeBytes,
		Bucket:       f.Bucket,
		ObjKey:       f.ObjKey,
		ContentHash:  f.ContentHash,
		CreateBy:     f.CreateBy,
		UpdateBy:     f.UpdateBy,
		CreatedAt:    timestamppb.New(f.CreatedAt),
		UpdatedAt:    timestamppb.New(f.UpdatedAt),
	}
}

// fillURL attaches one Display URL (ADR 0016) to a converted DTO. A signing
// failure degrades to an empty url — display sugar never fails a data
// response — and the domain service's propagation breadcrumb already
// recorded the failure, so nothing is swallowed silently here.
func (s *FileApplication) fillURL(ctx context.Context, out *filev1.StoredFile, f *fileentity.StoredFile) {
	url, err := s.svc.DisplayURL(ctx, f)
	if err != nil {
		return
	}
	out.Url = url
}

// Disposition is the Content-Disposition a proxy download serves.
type Disposition string

const (
	DispositionAttachment Disposition = "attachment"
	DispositionInline     Disposition = "inline"
)

// UploadInput is what one relayed multipart upload carries to the
// application: the byte stream, its known size (net/http reports the part
// size before reading), the original file name, and the optional business
// directory prefix. The content type is not here — it is sniffed from the
// bytes; a client's declaration never reaches the record (ADR 0008).
type UploadInput struct {
	Content      io.Reader
	Size         int64
	OriginalName string
	BizPath      string
}

// Download is an opened byte stream plus everything a proxy download needs
// to write the response. The caller owns Body and must close it.
type Download struct {
	Disposition   Disposition
	OriginalName  string
	ContentType   string
	ContentLength int64
	// ContentRange is set only for a range response; empty means the full
	// body.
	ContentRange string
	Body         io.ReadCloser
}

// FileApplication implements the file management service. It owns the
// module's use cases; all row and byte access goes through the file domain
// service (ADR 0005 — this module has no cross-domain orchestration). The
// module declares no Transaction: every write is a single-row statement and
// the byte leg is non-transactional by nature, so there is no critical
// section to wrap. Authentication and the operation's Functional Permission
// policy are enforced by the registered HTTP middleware.
type FileApplication struct {
	filev1.UnimplementedFileServiceServer

	svc    fileservice.File
	logger *slog.Logger
}

// NewFileApplication creates a FileApplication. svc is the file domain
// service — the module's only repository and storage holder.
func NewFileApplication(svc fileservice.File, logger *slog.Logger) *FileApplication {
	return &FileApplication{
		svc:    svc,
		logger: logger.With(slog.String("component", "application.file")),
	}
}

// Upload accepts one relayed multipart upload and returns the recorded
// Stored File. It sniffs the content type from the bytes before anything is
// stored, so the whitelist decision is made on what the bytes actually are.
// Called by the hand-written HTTP transfer handler (ADR 0008).
func (s *FileApplication) Upload(ctx context.Context, in *UploadInput) (*filev1.StoredFile, error) {
	mimeType, err := sniffContentType(in.Content)
	if err != nil {
		err = file.ErrFileInvalid.WithCause(err)
		logx.Propagate(ctx, s.logger, "file.upload", err)
		return nil, err
	}
	up := &fileentity.Upload{
		OriginalName: in.OriginalName,
		BizPath:      in.BizPath,
		MIMEType:     mimeType,
		SizeBytes:    in.Size,
	}
	created, err := s.svc.Accept(ctx, up, in.Content, bizpkg.AuditActor(ctx))
	if err != nil {
		logx.Propagate(ctx, s.logger, "file.upload", err)
		return nil, err
	}
	out := convertStoredFile(created)
	s.fillURL(ctx, out, created)
	return out, nil
}

// Download opens a Stored File's bytes for the proxy download: it resolves
// the disposition (default attachment), optionally one Range request, and
// returns the stream. Called by the hand-written HTTP transfer handler
// (ADR 0008); the caller owns and closes the returned Body.
func (s *FileApplication) Download(ctx context.Context, fileID int64, disposition string, rangeHeader string) (*Download, error) {
	disp := Disposition(disposition)
	if disp == "" {
		disp = DispositionAttachment
	}
	if disp != DispositionAttachment && disp != DispositionInline {
		err := file.ErrFileInvalid
		logx.Propagate(ctx, s.logger, "file.download", err)
		return nil, err
	}
	var rng *storage.ByteRange
	if rangeHeader != "" {
		rng = &storage.ByteRange{Header: rangeHeader}
	}
	obj, f, err := s.svc.Open(ctx, fileID, rng)
	if err != nil {
		logx.Propagate(ctx, s.logger, "file.download", err)
		return nil, err
	}
	contentType := obj.ContentType
	if contentType == "" {
		contentType = f.MIMEType
	}
	return &Download{
		Disposition:   disp,
		OriginalName:  f.OriginalName,
		ContentType:   contentType,
		ContentLength: obj.ContentLength,
		ContentRange:  obj.ContentRange,
		Body:          obj.Body,
	}, nil
}

// GetFile retrieves a Stored File's metadata by id.
func (s *FileApplication) GetFile(ctx context.Context, req *filev1.GetFileRequest) (*filev1.StoredFile, error) {
	f, err := s.svc.Get(ctx, req.GetFileId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "file.get_file", err)
		return nil, err
	}
	out := convertStoredFile(f)
	s.fillURL(ctx, out, f)
	return out, nil
}

// BatchGetFiles resolves a batch of ids to Stored Files with display URLs;
// unknown or withdrawn ids are absent from the response (the business-form
// counterpart of RuoYi's listByIds, ADR 0016).
func (s *FileApplication) BatchGetFiles(ctx context.Context, req *filev1.BatchGetFilesRequest) (*filev1.StoredFileSet, error) {
	files, err := s.svc.BatchGet(ctx, req.GetFileIds())
	if err != nil {
		logx.Propagate(ctx, s.logger, "file.batch_get_files", err)
		return nil, err
	}
	set := &filev1.StoredFileSet{Files: make([]*filev1.StoredFile, 0, len(files))}
	for _, f := range files {
		out := convertStoredFile(f)
		s.fillURL(ctx, out, f)
		set.Files = append(set.Files, out)
	}
	return set, nil
}

// ListFiles lists stored files with AIP-160 filter, AIP-132 order, and
// page-token pagination. Default order: newest first.
// fileListSpec is this module's list fact (ADR 0024): filterable
// idents plus the file listing order.
var fileListSpec = listq.Spec{
	Idents: []filtering.DeclarationOption{
		filtering.DeclareIdent("original_name", filtering.TypeString),
		filtering.DeclareIdent("suffix", filtering.TypeString),
		filtering.DeclareIdent("mime_type", filtering.TypeString),
		filtering.DeclareIdent("create_by", filtering.TypeInt),
	},
	DefaultOrder: []ordering.Field{{Path: "id", Desc: true}}, // 
}

func (s *FileApplication) ListFiles(ctx context.Context, req *filev1.ListFilesRequest) (*filev1.StoredFileSet, error) {
	q, page, err := listq.Parse(req, fileListSpec)
	if err != nil {
		logx.Propagate(ctx, s.logger, "file.list_files", err)
		return nil, err
	}
	files, err := s.svc.List(ctx, q)
	if err != nil {
		logx.Propagate(ctx, s.logger, "file.list_files", err)
		return nil, err
	}
	set := &filev1.StoredFileSet{Files: make([]*filev1.StoredFile, 0, len(files))}
	for _, f := range files {
		out := convertStoredFile(f)
		s.fillURL(ctx, out, f)
		set.Files = append(set.Files, out)
	}
	set.NextPageToken = page.NextToken(len(files))
	return set, nil
}

// WithdrawFile withdraws a Stored File: row soft-deleted, bytes removed
// (ADR 0009).
func (s *FileApplication) WithdrawFile(ctx context.Context, req *filev1.WithdrawFileRequest) (*emptypb.Empty, error) {
	if err := s.svc.Withdraw(ctx, req.GetFileId(), bizpkg.AuditActor(ctx)); err != nil {
		logx.Propagate(ctx, s.logger, "file.withdraw_file", err)
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// sniffContentType reports the bare media type net/http detects from the
// first 512 bytes, then rewinds the stream so the bytes are still stored in
// full. DetectContentType parameterizes text types ("text/plain;
// charset=utf-8"); the parameter is dropped — the stored value is a bare
// media type so the AIP mime_type filter can match it exactly. The reader
// must be seekable — a multipart.File always is.
func sniffContentType(content io.Reader) (string, error) {
	seeker, ok := content.(io.Seeker)
	if !ok {
		return "", file.ErrFileInvalid
	}
	buf := make([]byte, 512)
	n, err := io.ReadFull(content, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", err
	}
	if _, err := seeker.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	mimeType := stdhttp.DetectContentType(buf[:n])
	if i := strings.IndexByte(mimeType, ';'); i >= 0 {
		mimeType = strings.TrimSpace(mimeType[:i])
	}
	return mimeType, nil
}
