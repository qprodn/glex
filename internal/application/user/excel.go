package user

import (
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"time"

	userv1 "glex/api/user/v1"
	"glex/internal/bizpkg"
	"glex/internal/crossdomain/dict"
	userdomain "glex/internal/domain/user"
	userentity "glex/internal/domain/user/entity"
	"glex/pkg/excelx"
	"glex/pkg/logx"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
)

// DefaultImportInitPassword backs an empty user.init_password config —
// RuoYi's sys.user.initPassword lineage: a documented weak default
// operators must rotate, not a real credential (ADR 0017).
const DefaultImportInitPassword = "123456"

// importRowCap and exportRowCap bound one import/export at ADR 0017's
// 50,000-row target; both directions stream, but the DB reads and the
// decoded rows still land in memory once.
const (
	importRowCap = 50_000
	exportRowCap = 50_000
)

// The org contract crosses its errors as kratos reasons (DEPT_NOT_FOUND,
// DEPT_DISABLED) — the documented wire shape this module keys on to cite
// the operator-facing reason without importing org's domain package.
const (
	reasonDeptNotFound = "DEPT_NOT_FOUND"
	reasonDeptDisabled = "DEPT_DISABLED"
)

// userSheetRow is the user module's spreadsheet shape, declared once for
// export, import, and the template (ADR 0017): headers, required columns,
// and dict translations all ride the excelx tags. The department travels
// as its full path; the sheet never carries passwords.
type userSheetRow struct {
	UserName    string `excel:"name=登录账号;required"`
	NickName    string `excel:"name=用户昵称;required"`
	DeptPath    string `excel:"name=部门"`
	Email       string `excel:"name=邮箱"`
	PhoneNumber string `excel:"name=手机号"`
	Gender      int32  `excel:"name=性别;dict=sys_user_gender"`
	Status      int32  `excel:"name=状态;dict=sys_normal_disable"`
	Remark      string `excel:"name=备注"`
}

// WorkbookStream is a streamed export delivery (ADR 0017): the download's
// filename and the workbook's byte body. The body is a pipe — the encoding
// goroutine fills it while the HTTP handler drains; closing is the
// handler's. An encoding failure surfaces as a read error there, after the
// status line is out.
type WorkbookStream struct {
	Filename string
	Body     io.ReadCloser
}

// errImportAborted rolls the import transaction back once business
// conflicts were collected — the application's private all-or-nothing
// signal (ADR 0017); the collected row errors become the response instead.
var errImportAborted = stderrors.New("user.import_users: row conflicts, transaction rolled back")

// importResult carries every row failure collected across the validation
// stages to the wire response; imported_count stays 0 — all-or-nothing.
// The row failures are excelx.RowError whether the codec cited them or the
// post-validation checks did (in-sheet duplicates, department paths, DB
// conflicts) — one shape, one table on the frontend.
func importResult(errs ...excelx.RowError) *userv1.ImportUsersResponse {
	out := &userv1.ImportUsersResponse{}
	for _, e := range errs {
		out.RowErrors = append(out.RowErrors, &userv1.ImportRowError{
			Row:    int32(e.Row),
			Column: e.Column,
			Reason: e.Reason,
		})
	}
	return out
}

// dictTranslator adapts the dict vocabulary contract to excelx's
// Translator: one snapshot fetch per dict type, cached for the request.
// The codec is capability-free (ADR 0017), so this is the only place the
// sheet meets the dict module. The request context is captured for the
// fetches — the codec's synchronous callbacks all run inside one request.
type dictTranslator struct {
	ctx     context.Context
	options dict.VocabularyReader
	loaded  map[string]bool
	labels  map[string]map[string]string // dict type → value→label
	values  map[string]map[string]string // dict type → label→value
	failure error
}

func newDictTranslator(ctx context.Context, options dict.VocabularyReader) *dictTranslator {
	return &dictTranslator{
		ctx:     ctx,
		options: options,
		loaded:  map[string]bool{},
		labels:  map[string]map[string]string{},
		values:  map[string]map[string]string{},
	}
}

// load fetches one vocabulary once; a failed fetch is remembered so every
// later cell of the same type fails identically instead of re-querying.
func (t *dictTranslator) load(dictType string) {
	if t.loaded[dictType] || t.failure != nil {
		return
	}
	t.loaded[dictType] = true
	entries, err := t.options.ValueLabels(t.ctx, dictType)
	if err != nil {
		t.failure = err
		return
	}
	labels := make(map[string]string, len(entries))
	values := make(map[string]string, len(entries))
	for _, e := range entries {
		labels[e.Value] = e.Label
		values[e.Label] = e.Value
	}
	t.labels[dictType] = labels
	t.values[dictType] = values
}

// Label translates a stored value for export; unresolved values degrade to
// the raw text (ADR 0017 — a sour dictionary never fails an export).
func (t *dictTranslator) Label(dictType, value string) (string, bool) {
	t.load(dictType)
	label, ok := t.labels[dictType][value]
	return label, ok
}

// Value translates a cell for import; the raw stored value also passes
// (operators may type 0/1 where the template shows 男/女, RuoYi-style).
func (t *dictTranslator) Value(dictType, label string) (string, bool) {
	t.load(dictType)
	if v, ok := t.values[dictType][label]; ok {
		return v, true
	}
	_, ok := t.labels[dictType][label]
	return label, ok
}

// Failure reports the first vocabulary fetch failure, if any — a store
// outage is a system error, not a page of sour rows.
func (t *dictTranslator) Failure() error {
	return t.failure
}

// ImportUsers applies an Import File of new System Users (ADR 0017). The
// workbook streams in through the file contract, every row is validated —
// codec cells, in-sheet duplicates, department paths, then DB uniqueness
// inside the write — and either all rows are created or none is. Business
// conflicts come back as row errors on a 200; system failures propagate.
func (s *UserApplication) ImportUsers(ctx context.Context, req *userv1.ImportUsersRequest) (*userv1.ImportUsersResponse, error) {
	body, err := s.importFiles.OpenContent(ctx, req.GetFileId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "user.import_users", err)
		return nil, err
	}
	defer func() { _ = body.Close() }()

	tr := newDictTranslator(ctx, s.dicts)
	rows, cellErrs, err := excelx.Read[userSheetRow](body, importRowCap, excelx.WithDict(tr))
	if err != nil {
		if stderrors.Is(err, excelx.ErrTooManyRows) {
			err = userdomain.ErrImportRowOverflow
		} else {
			err = userdomain.ErrImportWorkbook.WithCause(err)
		}
		logx.Propagate(ctx, s.logger, "user.import_users", err)
		return nil, err
	}
	if err := tr.Failure(); err != nil {
		logx.Propagate(ctx, s.logger, "user.import_users", err)
		return nil, err
	}
	// Every validation stage reports into one list — the operator fixes the
	// whole sheet per round-trip, not one stage at a time (ADR 0017's
	// collect-all row errors).
	errs := append([]excelx.RowError(nil), cellErrs...)
	errs = append(errs, checkSheetUniqueness(rows)...)
	deptIDs, deptErrs, deptFailure := s.resolveSheetDeptPaths(ctx, rows)
	errs = append(errs, deptErrs...)
	if deptFailure != nil {
		logx.Propagate(ctx, s.logger, "user.import_users", deptFailure)
		return nil, deptFailure
	}
	if len(errs) > 0 {
		return importResult(errs...), nil
	}

	var conflicts []excelx.RowError
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		created := make([]int64, 0, len(rows))
		for _, row := range rows {
			id, err := s.createImported(ctx, row.Value, deptIDs[row.Value.DeptPath])
			if err != nil {
				if c, ok := importConflict(err, row.No); ok {
					conflicts = append(conflicts, c)
					continue
				}
				return err
			}
			created = append(created, id)
		}
		if len(conflicts) > 0 {
			return errImportAborted
		}
		// One hash for every row: imported users share the initial
		// credential, and authn hashes the batch once (ADR 0017).
		return s.credentials.WriteSystemUserCredentials(ctx, created, s.initPassword)
	}); err != nil {
		if stderrors.Is(err, errImportAborted) {
			return importResult(conflicts...), nil
		}
		logx.Propagate(ctx, s.logger, "user.import_users", err)
		return nil, err
	}
	return &userv1.ImportUsersResponse{ImportedCount: int64(len(rows))}, nil
}

// checkSheetUniqueness rejects duplicate login accounts, emails, and phone
// numbers within the sheet itself — the DB cannot see these, and a
// duplicate would surface as a mid-transaction conflict instead of a
// complete report.
func checkSheetUniqueness(rows []excelx.Row[userSheetRow]) []excelx.RowError {
	type key struct{ column, value string }
	seen := map[key]int{}
	var errs []excelx.RowError
	for _, row := range rows {
		r := row.Value
		for _, col := range []struct{ column, value string }{
			{"登录账号", r.UserName},
			{"邮箱", r.Email},
			{"手机号", r.PhoneNumber},
		} {
			if col.value == "" {
				continue
			}
			k := key{col.column, col.value}
			if first, dup := seen[k]; dup {
				errs = append(errs, excelx.RowError{
					Row:    row.No,
					Column: col.column,
					Reason: fmt.Sprintf("与第%d行重复", first),
				})
				continue
			}
			seen[k] = row.No
		}
	}
	return errs
}

// resolveSheetDeptPaths resolves every distinct department path once (ADR
// 0013: the org module owns the tree); unknown or disabled departments
// become row errors on every row that references them. Any other failure
// from the contract is a system error — it propagates as the third return
// so an org outage never masquerades as a page of 部门不存在 row errors.
func (s *UserApplication) resolveSheetDeptPaths(ctx context.Context, rows []excelx.Row[userSheetRow]) (map[string]int64, []excelx.RowError, error) {
	distinct := map[string]struct{}{}
	for _, row := range rows {
		if row.Value.DeptPath != "" {
			distinct[row.Value.DeptPath] = struct{}{}
		}
	}
	paths := make(map[string]int64, len(distinct))
	reasons := make(map[string]string, len(distinct))
	for path := range distinct {
		id, err := s.depts.ResolveDeptPath(ctx, path)
		if err == nil {
			paths[path] = id
			continue
		}
		// Only the two documented business reasons become row errors; any
		// other failure crossing the contract is a system error.
		switch kerrors.Reason(err) {
		case reasonDeptNotFound:
			reasons[path] = "部门不存在"
		case reasonDeptDisabled:
			reasons[path] = "部门已停用"
		default:
			logx.Propagate(ctx, s.logger, "user.import_users.dept_paths", err)
			return nil, nil, err
		}
	}
	if len(reasons) == 0 {
		return paths, nil, nil
	}
	var errs []excelx.RowError
	for _, row := range rows {
		if reason, failed := reasons[row.Value.DeptPath]; failed {
			errs = append(errs, excelx.RowError{Row: row.No, Column: "部门", Reason: reason})
		}
	}
	return paths, errs, nil
}

// createImported persists one imported row through the domain service
// (uniqueness rules live there, ADR 0005) and returns the new id. The
// initial credential is written once for the whole batch afterwards.
func (s *UserApplication) createImported(ctx context.Context, r userSheetRow, deptID int64) (int64, error) {
	actor := bizpkg.AuditActor(ctx)
	u := &userentity.User{
		UserName:    r.UserName,
		NickName:    r.NickName,
		Email:       r.Email,
		PhoneNumber: r.PhoneNumber,
		Gender:      r.Gender,
		Password:    s.initPassword,
		Status:      r.Status,
		DeptID:      deptID,
		Remark:      r.Remark,
		CreateBy:    actor,
		UpdateBy:    actor,
	}
	created, err := s.svc.Create(ctx, u)
	if err != nil {
		return 0, err
	}
	return created.ID, nil
}

// importConflict maps a domain uniqueness sentinel to its sheet column —
// the row error the operator fixes. Anything else is a system failure.
func importConflict(err error, row int) (excelx.RowError, bool) {
	switch {
	case stderrors.Is(err, userdomain.ErrUserNameConflict):
		return excelx.RowError{Row: row, Column: "登录账号", Reason: "登录账号已存在"}, true
	case stderrors.Is(err, userdomain.ErrEmailConflict):
		return excelx.RowError{Row: row, Column: "邮箱", Reason: "邮箱已存在"}, true
	case stderrors.Is(err, userdomain.ErrPhoneConflict):
		return excelx.RowError{Row: row, Column: "手机号", Reason: "手机号已存在"}, true
	}
	return excelx.RowError{}, false
}

// ExportUsersWorkbook streams the filtered users as an xlsx workbook (ADR
// 0017): the query mirrors the list page's filter, the workbook encodes
// onto a pipe, and nothing is stored — the oper log entry minted for the
// POST is the only record an export happened. The proto-shaped
// ExportUsers (Empty return) stays unimplemented on gRPC, the file
// module's HTTP-only precedent.
func (s *UserApplication) ExportUsersWorkbook(ctx context.Context, req *userv1.ExportUsersRequest) (*WorkbookStream, error) {
	users, err := s.listForExport(ctx, req)
	if err != nil {
		logx.Propagate(ctx, s.logger, "user.export_users", err)
		return nil, err
	}
	rows := s.exportRows(ctx, users)
	filename := fmt.Sprintf("用户数据_%s.xlsx", time.Now().Format("20060102150405"))
	return streamWorkbook(filename, "用户数据", rows, newDictTranslator(ctx, s.dicts))
}

// ImportTemplateWorkbook streams the import sheet's headers with no data —
// the empty-list export of the import row type (ADR 0017). The
// proto-shaped ImportTemplate stays unimplemented on gRPC, like the
// export.
func (s *UserApplication) ImportTemplateWorkbook(ctx context.Context) (*WorkbookStream, error) {
	return streamWorkbook("用户导入模板.xlsx", "用户数据", nil, newDictTranslator(ctx, s.dicts))
}

// listForExport runs the list page's query without pagination. It asks for
// one row past the cap and refuses when that row exists — a result set too
// large for v1's 50,000-row target is an operator-visible refusal (the
// import cap's symmetry), never a silently truncated workbook.
func (s *UserApplication) listForExport(ctx context.Context, req *userv1.ExportUsersRequest) ([]*userentity.User, error) {
	filter, err := userFilter(req)
	if err != nil {
		logx.Propagate(ctx, s.logger, "user.list_for_export", err)
		return nil, err
	}
	orderBy, err := userOrder(req)
	if err != nil {
		logx.Propagate(ctx, s.logger, "user.list_for_export", err)
		return nil, err
	}
	var deptIDs []int64
	if req.GetDeptId() != 0 {
		deptIDs, err = s.depts.ResolveSubtreeIDs(ctx, req.GetDeptId())
		if err != nil {
			logx.Propagate(ctx, s.logger, "user.list_for_export", err)
			return nil, err
		}
	}
	users, err := s.svc.List(ctx, filter, orderBy, exportRowCap+1, 0, deptIDs)
	if err != nil {
		logx.Propagate(ctx, s.logger, "user.list_for_export", err)
		return nil, err
	}
	if len(users) > exportRowCap {
		err = userdomain.ErrExportRowOverflow
		logx.Propagate(ctx, s.logger, "user.list_for_export", err)
		return nil, err
	}
	return users, nil
}

// exportRows converts the loaded users to sheet rows, resolving department
// paths and dict labels in batches.
func (s *UserApplication) exportRows(ctx context.Context, users []*userentity.User) []userSheetRow {
	deptIDs := make([]int64, 0, len(users))
	for _, u := range users {
		if u.DeptID != 0 {
			deptIDs = append(deptIDs, u.DeptID)
		}
	}
	paths := s.exportDeptPaths(ctx, deptIDs)
	rows := make([]userSheetRow, 0, len(users))
	for _, u := range users {
		rows = append(rows, userSheetRow{
			UserName:    u.UserName,
			NickName:    u.NickName,
			DeptPath:    paths[u.DeptID],
			Email:       u.Email,
			PhoneNumber: u.PhoneNumber,
			Gender:      u.Gender,
			Status:      u.Status,
			Remark:      u.Remark,
		})
	}
	return rows
}

// exportDeptPaths batch-resolves department ids to display paths; a failed
// lookup degrades to empty cells — one sour reference never fails the page
// (the contract boundary already logged why).
func (s *UserApplication) exportDeptPaths(ctx context.Context, deptIDs []int64) map[int64]string {
	paths, err := s.depts.DeptPaths(ctx, deptIDs)
	if err != nil {
		return map[int64]string{}
	}
	return paths
}

// streamWorkbook encodes the rows onto a pipe so the HTTP handler streams
// while the encoding runs (ADR 0017).
func streamWorkbook(filename, sheet string, rows []userSheetRow, tr *dictTranslator) (*WorkbookStream, error) {
	pr, pw := io.Pipe()
	go func() {
		if err := excelx.Write(pw, sheet, rows, excelx.WithDict(tr)); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_ = pw.Close()
	}()
	return &WorkbookStream{Filename: filename, Body: pr}, nil
}

// userFilter and userOrder parse the AIP filter/order expressions the list
// page already parses — the export follows the list exactly, so the shared
// declarations live once.
func userFilter(req filtering.Request) (filtering.Filter, error) {
	declarations, err := userFilterDeclarations()
	if err != nil {
		return filtering.Filter{}, err
	}
	return filtering.ParseFilter(req, declarations)
}

func userOrder(req ordering.Request) (ordering.OrderBy, error) {
	return ordering.ParseOrderBy(req)
}

func userFilterDeclarations() (*filtering.Declarations, error) {
	return filtering.NewDeclarations(
		filtering.DeclareStandardFunctions(),
		filtering.DeclareIdent("user_name", filtering.TypeString),
		filtering.DeclareIdent("email", filtering.TypeString),
		filtering.DeclareIdent("phone_number", filtering.TypeString),
		filtering.DeclareIdent("status", filtering.TypeInt),
		filtering.DeclareIdent("created_at", filtering.TypeTimestamp),
	)
}
