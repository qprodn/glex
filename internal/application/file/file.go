package file

import (
	"context"
	"io"
	"log/slog"
	stdhttp "net/http"
	"strings"

	filev1 "glex/api/file/v1"
	"glex/internal/domain/file"
	fileentity "glex/internal/domain/file/entity"
	fileservice "glex/internal/domain/file/service"
	"glex/internal/domain/file/storage"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"go.einride.tech/aip/pagination"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// convertStoredFile maps a fileentity.StoredFile DO to the proto StoredFile
// DTO.
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
		return nil, file.ErrFileInvalid.WithCause(err)
	}
	up := &fileentity.Upload{
		OriginalName: in.OriginalName,
		BizPath:      in.BizPath,
		MIMEType:     mimeType,
		SizeBytes:    in.Size,
	}
	created, err := s.svc.Accept(ctx, up, in.Content, auditActor(ctx))
	if err != nil {
		return nil, err
	}
	return convertStoredFile(created), nil
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
		return nil, file.ErrFileInvalid
	}
	var rng *storage.ByteRange
	if rangeHeader != "" {
		rng = &storage.ByteRange{Header: rangeHeader}
	}
	obj, f, err := s.svc.Open(ctx, fileID, rng)
	if err != nil {
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
		return nil, err
	}
	return convertStoredFile(f), nil
}

// ListFiles lists stored files with AIP-160 filter, AIP-132 order, and
// page-token pagination. Default order: newest first.
func (s *FileApplication) ListFiles(ctx context.Context, req *filev1.ListFilesRequest) (*filev1.StoredFileSet, error) {
	declarations, err := filtering.NewDeclarations(
		filtering.DeclareStandardFunctions(),
		filtering.DeclareIdent("original_name", filtering.TypeString),
		filtering.DeclareIdent("suffix", filtering.TypeString),
		filtering.DeclareIdent("mime_type", filtering.TypeString),
		filtering.DeclareIdent("create_by", filtering.TypeInt),
	)
	if err != nil {
		return nil, err
	}
	filter, err := filtering.ParseFilter(req, declarations)
	if err != nil {
		return nil, err
	}
	pageToken, err := pagination.ParsePageToken(req)
	if err != nil {
		return nil, err
	}
	orderBy, err := ordering.ParseOrderBy(req)
	if err != nil {
		return nil, err
	}
	if len(orderBy.Fields) == 0 {
		// Default order: newest first — a file listing's natural order.
		orderBy.Fields = []ordering.Field{{Path: "id", Desc: true}}
	}
	if req.GetPageSize() <= 0 {
		req.PageSize = 20
	}
	files, err := s.svc.List(ctx, filter, orderBy, int(req.GetPageSize()), int(pageToken.Offset))
	if err != nil {
		return nil, err
	}
	set := &filev1.StoredFileSet{Files: make([]*filev1.StoredFile, 0, len(files))}
	for _, f := range files {
		set.Files = append(set.Files, convertStoredFile(f))
	}
	if len(files) >= int(req.GetPageSize()) {
		set.NextPageToken = pageToken.Next(req).String()
	}
	return set, nil
}

// WithdrawFile withdraws a Stored File: row soft-deleted, bytes removed
// (ADR 0009).
func (s *FileApplication) WithdrawFile(ctx context.Context, req *filev1.WithdrawFileRequest) (*emptypb.Empty, error) {
	if err := s.svc.Withdraw(ctx, req.GetFileId(), auditActor(ctx)); err != nil {
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
