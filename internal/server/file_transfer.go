package server

import (
	"context"
	"io"
	"mime"
	stdhttp "net/http"
	"strconv"
	"time"

	filev1 "glex/api/file/v1"
	fileapp "glex/internal/application/file"
	"glex/internal/domain/file"

	khttp "github.com/go-kratos/kratos/v3/transport/http"
)

// The two transfer operations carry no google.api.http annotation (ADR 0008:
// kratos proto generators cannot express multipart or a streaming byte
// response), so protoc-gen-go-http emits no Operation constant for them.
// The canonical names are declared here, spelled exactly like the generated
// ones, so the auth route table keys on them like any generated route.
const (
	OperationFileServiceUploadFile   = "/file.v1.FileService/UploadFile"
	OperationFileServiceDownloadFile = "/file.v1.FileService/DownloadFile"
)

// transferTimeout bounds one byte transfer. The HTTP server timeout
// (server.http.timeout, seconds) is a request-handling budget; a legitimate
// transfer runs past it (ADR 0008), so each handler detaches from the
// request deadline while keeping its context values (the Principal). A
// disconnected client surfaces as a read or write error on the socket.
const transferTimeout = 30 * time.Minute

// transferContext derives the detached, transfer-bounded context.
func transferContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), transferTimeout)
}

// registerFileTransferHTTP wires the two hand-written HTTP-only transfer
// routes (ADR 0008): the multipart upload and the proxy download. The
// metadata routes (list/get/withdraw) come from the generated registration.
func registerFileTransferHTTP(s *khttp.Server, app *fileapp.FileApplication) {
	r := s.Route("/")
	r.Handle("POST", "/file/files", fileUploadHandler(app))
	r.Handle("GET", "/file/files/{file_id}/content", fileDownloadHandler(app))
}

func fileUploadHandler(app *fileapp.FileApplication) func(ctx khttp.Context) error {
	return func(ctx khttp.Context) error {
		khttp.SetOperation(ctx, OperationFileServiceUploadFile)
		// The transfer context is minted at handler scope so it outlives the
		// middleware callback and is cancelled when the request finishes.
		xfer, cancel := transferContext(ctx)
		defer cancel()
		h := ctx.Middleware(func(context.Context, any) (any, error) {
			in, cleanup, err := parseUploadForm(ctx.Request())
			if err != nil {
				return nil, err
			}
			defer cleanup()
			return app.Upload(xfer, in)
		})
		out, err := h(ctx, &filev1.UploadFileRequest{})
		if err != nil {
			return err
		}
		return ctx.Result(200, out.(*filev1.StoredFile))
	}
}

func fileDownloadHandler(app *fileapp.FileApplication) func(ctx khttp.Context) error {
	return func(ctx khttp.Context) error {
		khttp.SetOperation(ctx, OperationFileServiceDownloadFile)
		xfer, cancel := transferContext(ctx)
		defer cancel()
		h := ctx.Middleware(func(context.Context, any) (any, error) {
			fileID, err := strconv.ParseInt(ctx.Vars().Get("file_id"), 10, 64)
			if err != nil {
				return nil, file.ErrFileInvalid.WithCause(err)
			}
			return app.Download(xfer, fileapp.DownloadRequest{
				FileID:      fileID,
				Disposition: ctx.Query().Get("disposition"),
				RangeHeader: ctx.Header().Get("Range"),
			})
		})
		out, err := h(ctx, &filev1.DownloadFileRequest{})
		if err != nil {
			return err
		}
		dl := out.(*fileapp.Download)
		defer dl.Body.Close()
		resp := ctx.Response()
		resp.Header().Set("Content-Type", dl.ContentType)
		resp.Header().Set("Content-Disposition", mime.FormatMediaType(string(dl.Disposition), map[string]string{"filename": dl.OriginalName}))
		resp.Header().Set("Accept-Ranges", "bytes")
		if dl.ContentLength > 0 {
			resp.Header().Set("Content-Length", strconv.FormatInt(dl.ContentLength, 10))
		}
		if dl.ContentRange != "" {
			resp.Header().Set("Content-Range", dl.ContentRange)
			resp.WriteHeader(stdhttp.StatusPartialContent)
		} else {
			resp.WriteHeader(stdhttp.StatusOK)
		}
		// The stream runs after the middleware chain returned; a client
		// disconnect surfaces as a write error here, and the status line is
		// already out — the JSON error encoder has nothing left to do.
		_, err = io.Copy(resp, dl.Body)
		return err
	}
}

// parseUploadForm extracts the "file" part and the optional "path" business
// prefix from a multipart upload. The returned cleanup closes the file part.
func parseUploadForm(r *stdhttp.Request) (*fileapp.UploadInput, func(), error) {
	part, header, err := r.FormFile("file")
	if err != nil {
		return nil, nil, file.ErrFileNoUpload.WithCause(err)
	}
	in := &fileapp.UploadInput{
		Content:      part,
		Size:         header.Size,
		OriginalName: header.Filename,
		BizPath:      r.FormValue("path"),
	}
	// The part is fully read once cleanup runs, and closing it then cannot
	// fail actionably — in-memory parts close as a no-op and the temp file is
	// opened read-only — so the error is discarded (ADR 0007).
	return in, func() { _ = part.Close() }, nil
}
