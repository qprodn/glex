package excelx

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

type sheetRow struct {
	UserName    string `excel:"name=登录账号;required"`
	NickName    string `excel:"name=用户昵称;required"`
	DeptPath    string `excel:"name=部门"`
	Email       string `excel:"name=邮箱"`
	PhoneNumber string `excel:"name=手机号"`
	Gender      int32  `excel:"name=性别;dict=sys_user_gender"`
	Status      int32  `excel:"name=状态;dict=sys_normal_disable"`
}

// stubDict is a canned per-type Translator: labels forward, values
// reverse, and both directions accept the other side's text (value
// passthrough, RuoYi-style).
type stubDict struct {
	byType map[string]map[string]string
}

func (d stubDict) Label(dictType, value string) (string, bool) {
	label, ok := d.byType[dictType][value]
	return label, ok
}

func (d stubDict) Value(dictType, label string) (string, bool) {
	m := d.byType[dictType]
	if value, ok := m[label]; ok {
		return value, true
	}
	for v, l := range m {
		if l == label || v == label {
			return v, true
		}
	}
	return "", false
}

var dicts = stubDict{byType: map[string]map[string]string{
	"sys_user_gender":     {"0": "男", "1": "女", "2": "未知"},
	"sys_normal_disable":  {"0": "正常", "1": "停用"},
}}

// minimalRow is the two-column shape for header-level tests.
type minimalRow struct {
	UserName string `excel:"name=登录账号;required"`
	NickName string `excel:"name=用户昵称;required"`
}

func TestRoundTrip(t *testing.T) {
	in := []sheetRow{
		{UserName: "admin", NickName: "管理员", DeptPath: "总公司/研发部", Email: "a@b.com", PhoneNumber: "13800000000", Gender: 0, Status: 0},
		{UserName: "ry", NickName: "若依", Gender: 1, Status: 1},
	}
	var buf bytes.Buffer
	if err := Write(&buf, "用户数据", in, WithDict(dicts)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	exported := buf.Bytes()

	// The exported cells carry labels, not stored values.
	f, err := excelize.OpenReader(&buf)
	if err != nil {
		t.Fatalf("open exported workbook: %v", err)
	}
	defer func() { _ = f.Close() }()
	got, err := f.GetRows("用户数据")
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("sheet rows = %d, want 3 (header + 2)", len(got))
	}
	if got[1][5] != "男" || got[1][6] != "正常" {
		t.Fatalf("row 1 dict cells = %q, want 男/正常", got[1][5:7])
	}

	// And the labels read back as the stored values.
	out, errs, err := Read[sheetRow](bytes.NewReader(exported), 100, WithDict(dicts))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("Read row errors = %v, want none", errs)
	}
	for i, want := range in {
		if out[i].Value != want {
			t.Fatalf("round trip row %d = %+v, want %+v", i, out[i].Value, want)
		}
	}
	if len(out) != len(in) {
		t.Fatalf("round trip rows = %d, want %d", len(out), len(in))
	}
}

func TestWriteEmptyIsTemplate(t *testing.T) {
	var buf bytes.Buffer
	if err := Write[sheetRow](&buf, "用户数据", nil, WithDict(dicts)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	rows, errs, err := Read[minimalRow](bytes.NewReader(buf.Bytes()), 100)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(errs) != 0 || len(rows) != 0 {
		t.Fatalf("template read = %d rows, errs %v; want 0/none", len(rows), errs)
	}
	f, _ := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	defer func() { _ = f.Close() }()
	got, _ := f.GetRows("用户数据")
	if len(got) != 1 || got[0][0] != "登录账号" {
		t.Fatalf("template sheet = %v, want a single header row", got)
	}
}

func TestWriteUnknownLabelFallsBackToRaw(t *testing.T) {
	in := []sheetRow{{UserName: "u", NickName: "n", Gender: 7}}
	var buf bytes.Buffer
	if err := Write(&buf, "用户数据", in, WithDict(dicts)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	f, _ := excelize.OpenReader(&buf)
	defer func() { _ = f.Close() }()
	got, _ := f.GetRows("用户数据")
	if got[1][5] != "7" {
		t.Fatalf("unknown dict value exported as %q, want raw 7", got[1][5])
	}
}

func TestReadCollectsRowErrors(t *testing.T) {
	in := []sheetRow{
		{UserName: "ok", NickName: "好", Gender: 0, Status: 0},
		{UserName: "", NickName: "空账号", Gender: 9, Status: 0},
	}
	var buf bytes.Buffer
	if err := Write(&buf, "用户数据", in, WithDict(dicts)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Gender 9 has no label, so it exports raw and fails to read back.
	rows, errs, err := Read[sheetRow](bytes.NewReader(buf.Bytes()), 100, WithDict(dicts))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(rows) != 1 || rows[0].Value.UserName != "ok" {
		t.Fatalf("valid rows = %+v, want only row 1", rows)
	}
	if len(errs) != 2 {
		t.Fatalf("row errors = %v, want 2", errs)
	}
	wantReasons := map[string]bool{"不能为空": false, `无法识别的选项 "9"`: false}
	for _, e := range errs {
		if e.Row != 3 {
			t.Fatalf("error row = %d, want 3 (header is row 1, blank-free)", e.Row)
		}
		if _, ok := wantReasons[e.Reason]; !ok {
			t.Fatalf("unexpected reason %q", e.Reason)
		}
		wantReasons[e.Reason] = true
	}
	for reason, seen := range wantReasons {
		if !seen {
			t.Fatalf("missing reason %q in %v", reason, errs)
		}
	}
}

func TestReadSkipsBlankRows(t *testing.T) {
	f := excelize.NewFile()
	if err := f.SetSheetRow("Sheet1", "A1", &[]any{"登录账号", "用户昵称"});
		err != nil {
		t.Fatalf("header: %v", err)
	}
	if err := f.SetSheetRow("Sheet1", "A3", &[]any{"admin", "管理员"}); err != nil {
		t.Fatalf("row: %v", err)
	}
	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}
	rows, errs, err := Read[minimalRow](bytes.NewReader(buf.Bytes()), 100)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// The data sits on sheet row 3 — the decoded Row must cite 3, not 2:
	// blank-row skipping cannot renumber the operator's sheet.
	if len(errs) != 0 || len(rows) != 1 || rows[0].Value.UserName != "admin" || rows[0].No != 3 {
		t.Fatalf("rows = %+v errs = %v, want the single data row on sheet row 3", rows, errs)
	}
}

func TestReadHeaderOrderAgnostic(t *testing.T) {
	f := excelize.NewFile()
	if err := f.SetSheetRow("Sheet1", "A1", &[]any{"用户昵称", "登录账号"}); err != nil {
		t.Fatalf("header: %v", err)
	}
	if err := f.SetSheetRow("Sheet1", "A2", &[]any{"昵称", "账号"}); err != nil {
		t.Fatalf("row: %v", err)
	}
	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}
	rows, _, err := Read[minimalRow](bytes.NewReader(buf.Bytes()), 100)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(rows) != 1 || rows[0].Value.UserName != "账号" || rows[0].Value.NickName != "昵称" {
		t.Fatalf("rows = %+v, want columns matched by header", rows)
	}
}

func TestReadMissingColumn(t *testing.T) {
	f := excelize.NewFile()
	if err := f.SetSheetRow("Sheet1", "A1", &[]any{"登录账号"}); err != nil {
		t.Fatalf("header: %v", err)
	}
	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, _, err := Read[minimalRow](bytes.NewReader(buf.Bytes()), 100)
	if !errors.Is(err, ErrMissingColumn) {
		t.Fatalf("err = %v, want ErrMissingColumn", err)
	}
	if !strings.Contains(err.Error(), "用户昵称") {
		t.Fatalf("err = %v, want it to name the missing column", err)
	}
}

func TestReadBadWorkbook(t *testing.T) {
	if _, _, err := Read[sheetRow](strings.NewReader("not an xlsx"), 100); !errors.Is(err, ErrBadWorkbook) {
		t.Fatalf("err = %v, want ErrBadWorkbook", err)
	}
}

func TestReadTooManyRows(t *testing.T) {
	in := []sheetRow{
		{UserName: "a", NickName: "a"},
		{UserName: "b", NickName: "b"},
		{UserName: "c", NickName: "c"},
	}
	var buf bytes.Buffer
	if err := Write(&buf, "用户数据", in); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, _, err := Read[sheetRow](bytes.NewReader(buf.Bytes()), 2); !errors.Is(err, ErrTooManyRows) {
		t.Fatalf("err = %v, want ErrTooManyRows", err)
	}
}

func TestSchemaErrors(t *testing.T) {
	type noTags struct {
		Name string
	}
	if _, err := columnsOf[noTags](); !errors.Is(err, ErrNoColumns) {
		t.Fatalf("err = %v, want ErrNoColumns", err)
	}
	type badKind struct {
		When float64 `excel:"name=时间"`
	}
	if _, err := columnsOf[badKind](); err == nil || !strings.Contains(err.Error(), "unsupported kind") {
		t.Fatalf("err = %v, want unsupported kind", err)
	}
	type noName struct {
		Name string `excel:"required"`
	}
	if _, err := columnsOf[noName](); err == nil || !strings.Contains(err.Error(), "name=<header>") {
		t.Fatalf("err = %v, want missing name", err)
	}
	if err := Write(&strings.Builder{}, "s", []int{1}); err == nil {
		t.Fatal("non-struct row type must fail")
	}
}
