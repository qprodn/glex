package user

import (
	"bytes"
	"context"
	"io"
	"testing"

	"glex/internal/conf"
	dictmodel "glex/internal/crossdomain/dict/model"
	filedomain "glex/internal/domain/file"
	userentity "glex/internal/domain/user/entity"
	"glex/pkg/excelx"

	userv1 "glex/api/user/v1"

	"github.com/xuri/excelize/v2"
)

// fakeContentReader is the file contract double: it serves canned workbook
// bytes for one Stored File id (the Import File).
type fakeContentReader struct {
	byID  map[int64][]byte
	asked int64
}

func (f *fakeContentReader) OpenContent(_ context.Context, fileID int64) (io.ReadCloser, error) {
	f.asked = fileID
	data, ok := f.byID[fileID]
	if !ok {
		return nil, filedomain.ErrFileNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// fakeDictOptions is the dict contract double: canned vocabularies per
// type, shaped like the seeded sys_user_gender and sys_normal_disable.
type fakeDictOptions struct {
	byType map[string][]dictmodel.Entry
}

func (f *fakeDictOptions) ValueLabels(_ context.Context, dictType string) ([]dictmodel.Entry, error) {
	return f.byType[dictType], nil
}

func seededDictOptions() *fakeDictOptions {
	return &fakeDictOptions{byType: map[string][]dictmodel.Entry{
		"sys_user_gender": {
			{Value: "0", Label: "男"},
			{Value: "1", Label: "女"},
			{Value: "2", Label: "未知"},
		},
		"sys_normal_disable": {
			{Value: "0", Label: "正常"},
			{Value: "1", Label: "停用"},
		},
	}}
}

// newExcelTestApplication wires an application with the excel seams faked:
// the Import File bytes, the dict vocabularies, and the department
// resolution table.
func newExcelTestApplication(svc *fakeUserService, credentials *fakeCredentialWriter, files *fakeContentReader, depts *fakeDeptReader, initPassword string) *UserApplication {
	return NewUserApplication(svc, credentials, depts, new(fakeURLResolver), files, seededDictOptions(), &conf.User{InitPassword: initPassword}, newFakeTransaction(), new(fakeSessionRevoker), testLogger())
}

// sheetBytes builds an import workbook straight through the codec, with
// dict labels in cells — what an operator filling the template produces.
func sheetBytes(t *testing.T, rows []userSheetRow) []byte {
	t.Helper()
	tr := translatorFrom(seededDictOptions())
	var buf bytes.Buffer
	if err := excelx.Write(&buf, "用户数据", rows, excelx.WithDict(tr)); err != nil {
		t.Fatalf("write sheet: %v", err)
	}
	return buf.Bytes()
}

// translatorFrom adapts the fake contract to the codec in tests.
type optionsTranslator struct {
	o *fakeDictOptions
}

func (t optionsTranslator) Label(dictType, value string) (string, bool) {
	for _, e := range t.o.byType[dictType] {
		if e.Value == value {
			return e.Label, true
		}
	}
	return "", false
}

func (t optionsTranslator) Value(dictType, label string) (string, bool) {
	for _, e := range t.o.byType[dictType] {
		if e.Label == label {
			return e.Value, true
		}
		if e.Value == label {
			return e.Value, true
		}
	}
	return "", false
}

func translatorFrom(o *fakeDictOptions) excelx.Translator {
	return optionsTranslator{o: o}
}

func TestImportUsersCreatesAllRowsWithSharedInitialCredential(t *testing.T) {
	svc := newFakeUserService()
	credentials := new(fakeCredentialWriter)
	files := &fakeContentReader{byID: map[int64][]byte{7: sheetBytes(t, []userSheetRow{
		{UserName: "alice", NickName: "爱丽丝", DeptPath: "总公司/研发部", Gender: 0, Status: 0},
		{UserName: "bob", NickName: "鲍勃", Gender: 1, Status: 1},
	})}}
	depts := &fakeDeptReader{resolve: map[string]int64{"总公司/研发部": 30}}
	app := newExcelTestApplication(svc, credentials, files, depts, "InitPass123")

	out, err := app.ImportUsers(context.Background(), &userv1.ImportUsersRequest{FileId: 7})
	if err != nil {
		t.Fatalf("ImportUsers: %v", err)
	}
	if out.GetImportedCount() != 2 || len(out.GetRowErrors()) != 0 {
		t.Fatalf("import result = %+v, want 2 rows and no errors", out)
	}
	if files.asked != 7 {
		t.Fatalf("opened file %d, want 7", files.asked)
	}
	if len(svc.created) != 2 {
		t.Fatalf("created %d users, want 2", len(svc.created))
	}
	alice := svc.created[0]
	if alice.DeptID != 30 || alice.Password != "InitPass123" || alice.UserName != "alice" {
		t.Fatalf("imported alice = %+v, want dept 30, the shared initial credential", alice)
	}
	if bob := svc.created[1]; bob.DeptID != 0 {
		t.Fatalf("row without a department must land dept-less, got %d", bob.DeptID)
	}
	if len(credentials.written) != 2 {
		t.Fatalf("credential batch writes = %#v, want one per imported user", credentials.written)
	}
	for _, w := range credentials.written {
		if w.Password != "InitPass123" {
			t.Fatalf("batch credential = %q, want the shared initial credential", w.Password)
		}
	}
}

func TestImportUsersCellErrorsRejectTheWholeBatch(t *testing.T) {
	svc := newFakeUserService()
	files := &fakeContentReader{byID: map[int64][]byte{7: sheetBytes(t, []userSheetRow{
		{UserName: "alice", NickName: "好", Gender: 0},
		{UserName: "bob", NickName: "好", Gender: 9},
	})}}
	app := newExcelTestApplication(svc, new(fakeCredentialWriter), files, new(fakeDeptReader), "InitPass123")

	out, err := app.ImportUsers(context.Background(), &userv1.ImportUsersRequest{FileId: 7})
	if err != nil {
		t.Fatalf("cell errors are a 200 response, got error %v", err)
	}
	if out.GetImportedCount() != 0 || len(out.GetRowErrors()) == 0 {
		t.Fatalf("result = %+v, want zero imports and row errors", out)
	}
	if len(svc.created) != 0 {
		t.Fatalf("all-or-nothing: created %d users despite row errors", len(svc.created))
	}
	e := out.GetRowErrors()[0]
	if e.GetRow() != 3 || e.GetColumn() != "性别" {
		t.Fatalf("row error = %v, want sheet row 3 on 性别", e)
	}
}

func TestImportUsersInSheetDuplicatesAreRowErrors(t *testing.T) {
	files := &fakeContentReader{byID: map[int64][]byte{7: sheetBytes(t, []userSheetRow{
		{UserName: "alice", NickName: "一号"},
		{UserName: "alice", NickName: "二号"},
	})}}
	svc := newFakeUserService()
	app := newExcelTestApplication(svc, new(fakeCredentialWriter), files, new(fakeDeptReader), "InitPass123")

	out, err := app.ImportUsers(context.Background(), &userv1.ImportUsersRequest{FileId: 7})
	if err != nil {
		t.Fatalf("ImportUsers: %v", err)
	}
	if out.GetImportedCount() != 0 || len(out.GetRowErrors()) != 1 {
		t.Fatalf("result = %+v, want the duplicate cited", out)
	}
	e := out.GetRowErrors()[0]
	if e.GetRow() != 3 || e.GetColumn() != "登录账号" || e.GetReason() != "与第2行重复" {
		t.Fatalf("duplicate error = %v, want row 3 citing row 2", e)
	}
	if len(svc.created) != 0 {
		t.Fatalf("duplicates must reject the batch, created %d", len(svc.created))
	}
}

func TestImportUsersUnknownDepartmentCitesEveryRow(t *testing.T) {
	files := &fakeContentReader{byID: map[int64][]byte{7: sheetBytes(t, []userSheetRow{
		{UserName: "alice", NickName: "a", DeptPath: "不存在/部门"},
		{UserName: "bob", NickName: "b", DeptPath: "不存在/部门"},
	})}}
	svc := newFakeUserService()
	app := newExcelTestApplication(svc, new(fakeCredentialWriter), files, new(fakeDeptReader), "InitPass123")

	out, err := app.ImportUsers(context.Background(), &userv1.ImportUsersRequest{FileId: 7})
	if err != nil {
		t.Fatalf("ImportUsers: %v", err)
	}
	if len(out.GetRowErrors()) != 2 {
		t.Fatalf("row errors = %v, want one per referencing row", out.GetRowErrors())
	}
	for _, e := range out.GetRowErrors() {
		if e.GetColumn() != "部门" || e.GetReason() != "部门不存在" {
			t.Fatalf("dept error = %v, want 部门不存在 on the 部门 column", e)
		}
	}
}

func TestImportUsersDBConflictsRollBackAndReport(t *testing.T) {
	svc := newFakeUserService()
	svc.conflictOn = "bob" // alice already exists in the store
	credentials := new(fakeCredentialWriter)
	files := &fakeContentReader{byID: map[int64][]byte{7: sheetBytes(t, []userSheetRow{
		{UserName: "alice", NickName: "a"},
		{UserName: "bob", NickName: "b"},
	})}}
	app := newExcelTestApplication(svc, credentials, files, new(fakeDeptReader), "InitPass123")

	out, err := app.ImportUsers(context.Background(), &userv1.ImportUsersRequest{FileId: 7})
	if err != nil {
		t.Fatalf("conflicts are a 200 response, got %v", err)
	}
	if out.GetImportedCount() != 0 || len(out.GetRowErrors()) != 1 {
		t.Fatalf("result = %+v, want the conflict cited and nothing imported", out)
	}
	e := out.GetRowErrors()[0]
	if e.GetRow() != 3 || e.GetColumn() != "登录账号" || e.GetReason() != "登录账号已存在" {
		t.Fatalf("conflict error = %v, want row 3 on 登录账号", e)
	}
	if len(credentials.written) != 0 {
		t.Fatalf("rolled-back import must not write credentials, got %#v", credentials.written)
	}
}

func TestImportUsersBadWorkbookIsAWireError(t *testing.T) {
	files := &fakeContentReader{byID: map[int64][]byte{7: []byte("not an xlsx")}}
	app := newExcelTestApplication(newFakeUserService(), new(fakeCredentialWriter), files, new(fakeDeptReader), "InitPass123")

	if _, err := app.ImportUsers(context.Background(), &userv1.ImportUsersRequest{FileId: 7}); err == nil {
		t.Fatal("garbage bytes must fail the request, not come back as row errors")
	}
}

func TestImportUsersMissingFilePropagatesFileError(t *testing.T) {
	app := newExcelTestApplication(newFakeUserService(), new(fakeCredentialWriter), new(fakeContentReader), new(fakeDeptReader), "InitPass123")

	if _, err := app.ImportUsers(context.Background(), &userv1.ImportUsersRequest{FileId: 404}); err == nil {
		t.Fatal("an unknown file id must surface the file contract's error")
	}
}

func TestExportUsersStreamsLabelledSheet(t *testing.T) {
	svc := newFakeUserService()
	svc.list = []*userentity.User{
		{ID: 1, UserName: "alice", NickName: "爱丽丝", DeptID: 30, Gender: 0, Status: 0},
		{ID: 2, UserName: "bob", NickName: "鲍勃", Gender: 1, Status: 1},
	}
	depts := &fakeDeptReader{paths: map[int64]string{30: "总公司/研发部"}}
	app := newExcelTestApplication(svc, new(fakeCredentialWriter), new(fakeContentReader), depts, "InitPass123")

	wb, err := app.ExportUsersWorkbook(context.Background(), &userv1.ExportUsersRequest{})
	if err != nil {
		t.Fatalf("ExportUsersWorkbook: %v", err)
	}
	defer wb.Body.Close()
	if svc.lastListCall.limit != exportRowCap+1 {
		t.Fatalf("export queried with limit %d, want one past the cap (overflow detection, no pagination)", svc.lastListCall.limit)
	}
	data, err := io.ReadAll(wb.Body)
	if err != nil {
		t.Fatalf("drain export body: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("exported bytes are a workbook: %v", err)
	}
	defer func() { _ = f.Close() }()
	got, err := f.GetRows("用户数据")
	if err != nil || len(got) != 3 {
		t.Fatalf("export sheet = %v (err %v), want header + 2 rows", got, err)
	}
	if got[0][0] != "登录账号" || got[1][2] != "总公司/研发部" || got[1][5] != "男" || got[2][6] != "停用" {
		t.Fatalf("export cells = %v, want headers, dept paths, and dict labels", got)
	}
}

func TestImportTemplateWorkbookIsHeadersOnly(t *testing.T) {
	app := newExcelTestApplication(newFakeUserService(), new(fakeCredentialWriter), new(fakeContentReader), new(fakeDeptReader), "InitPass123")

	wb, err := app.ImportTemplateWorkbook(context.Background())
	if err != nil {
		t.Fatalf("ImportTemplateWorkbook: %v", err)
	}
	defer wb.Body.Close()
	data, err := io.ReadAll(wb.Body)
	if err != nil {
		t.Fatalf("drain template body: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("template bytes are a workbook: %v", err)
	}
	defer func() { _ = f.Close() }()
	got, err := f.GetRows("用户数据")
	if err != nil || len(got) != 1 || got[0][0] != "登录账号" || got[0][7] != "备注" {
		t.Fatalf("template sheet = %v (err %v), want the header row only", got, err)
	}
}
