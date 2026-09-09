package server

import (
	"context"
	"io"
	"mime"
	stdhttp "net/http"
	"net/url"

	userv1 "glex/api/user/v1"
	userapp "glex/internal/application/user"

	khttp "github.com/go-kratos/kratos/v3/transport/http"
)

// The two user spreadsheet operations carry no google.api.http annotation
// (ADR 0017: the streaming byte response is beyond the proto generators,
// same shape as the file module's transfers), so protoc-gen-go-http emits
// no Operation constant for them. The canonical names are declared here,
// spelled exactly like the generated ones, so the auth route table keys on
// them like any generated route.
const (
	OperationUserServiceExportUsers    = "/user.v1.UserService/ExportUsers"
	OperationUserServiceImportTemplate = "/user.v1.UserService/ImportTemplate"
)

// registerUserExcelHTTP wires the two hand-written HTTP-only spreadsheet
// routes (ADR 0017): the streaming export and the import template. The
// ImportUsers JSON endpoint comes from the generated registration.
func registerUserExcelHTTP(s *khttp.Server, app *userapp.UserApplication) {
	r := s.Route("/")
	r.Handle("POST", "/iam/user/-/export", userExportHandler(app))
	r.Handle("POST", "/iam/user/-/importTemplate", userTemplateHandler(app))
}

func userExportHandler(app *userapp.UserApplication) func(khttp.Context) error {
	return func(ctx khttp.Context) error {
		khttp.SetOperation(ctx, OperationUserServiceExportUsers)
		// The workbook encode runs inside the request, bounded by the
		// transfer budget — a large export outruns the plain HTTP timeout
		// like any byte transfer (the file route's detached-context rule).
		xfer, cancel := transferContext(ctx)
		defer cancel()
		h := ctx.Middleware(func(context.Context, any) (any, error) {
			var in userv1.ExportUsersRequest
			if err := ctx.Bind(&in); err != nil {
				return nil, err
			}
			return app.ExportUsersWorkbook(xfer, &in)
		})
		return streamWorkbookResponse(ctx, h, &userv1.ExportUsersRequest{})
	}
}

func userTemplateHandler(app *userapp.UserApplication) func(khttp.Context) error {
	return func(ctx khttp.Context) error {
		khttp.SetOperation(ctx, OperationUserServiceImportTemplate)
		xfer, cancel := transferContext(ctx)
		defer cancel()
		h := ctx.Middleware(func(context.Context, any) (any, error) {
			return app.ImportTemplateWorkbook(xfer)
		})
		return streamWorkbookResponse(ctx, h, &userv1.ImportTemplateRequest{})
	}
}

// streamWorkbookResponse runs one workbook-producing middleware call, then
// streams the pipe onto the response (the file download route's shape).
// The status line goes out before the first byte — an encoding failure
// mid-stream surfaces as a client-side read error here, after the boundary
// logged what it could.
func streamWorkbookResponse(ctx khttp.Context, h func(context.Context, any) (any, error), emptyReq any) error {
	out, err := h(ctx, emptyReq)
	if err != nil {
		return err
	}
	wb := out.(*userapp.WorkbookStream)
	defer wb.Body.Close()
	resp := ctx.Response()
	resp.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	resp.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": wb.Filename}))
	// The percent-encoded name rides a plain header the browser's blob
	// save can read; cross-origin deployments expose it explicitly.
	resp.Header().Set("download-filename", url.PathEscape(wb.Filename))
	resp.Header().Set("Access-Control-Expose-Headers", "Content-Disposition,download-filename")
	resp.WriteHeader(stdhttp.StatusOK)
	_, err = io.Copy(resp, wb.Body)
	return err
}
