package excelx

import (
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Option and WithDict live in write.go — one option set serves both
// directions.

// Row is one decoded data row with its 1-based sheet row number — the
// number survives blank-row skipping, so post-validation failures (in-sheet
// duplicates, cross-module checks) can cite the operator's actual row.
type Row[T any] struct {
	No    int
	Value T
}

// Read decodes the first sheet of the workbook in r into T rows. Columns
// match by header text (order agnostic; extra workbook columns are
// ignored), fully blank rows are skipped, and every cell-level failure is
// collected into the returned []RowError instead of stopping at the first —
// the caller rejects the whole import when any row failed (ADR 0017's
// all-or-nothing). maxRows bounds the data rows scanned.
func Read[T any](r io.Reader, maxRows int, opts ...Option) ([]Row[T], []RowError, error) {
	cols, err := columnsOf[T]()
	if err != nil {
		return nil, nil, err
	}
	o := options{}
	for _, opt := range opts {
		opt(&o)
	}
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrBadWorkbook, err)
	}
	defer func() { _ = f.Close() }()
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, nil, ErrNoSheet
	}
	iter, err := f.Rows(sheets[0])
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrBadWorkbook, err)
	}
	defer func() { _ = iter.Close() }()
	if !iter.Next() {
		return nil, nil, ErrNoHeader
	}
	header, err := iter.Columns()
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrBadWorkbook, err)
	}
	colIndex := map[string]int{}
	for i, h := range header {
		colIndex[strings.TrimSpace(h)] = i
	}
	var missing []string
	for _, c := range cols {
		if _, ok := colIndex[c.header]; !ok {
			missing = append(missing, c.header)
		}
	}
	if len(missing) > 0 {
		return nil, nil, fmt.Errorf("%w: %s", ErrMissingColumn, strings.Join(missing, ", "))
	}
	var rows []Row[T]
	var errs []RowError
	scanned := 0
	for rowNo := 2; iter.Next(); rowNo++ {
		cells, err := iter.Columns()
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %s", ErrBadWorkbook, err)
		}
		if blank(cells) {
			continue
		}
		scanned++
		if scanned > maxRows {
			return nil, nil, fmt.Errorf("%w: cap %d", ErrTooManyRows, maxRows)
		}
		row := new(T)
		if rowErrs := decodeRow(row, cells, colIndex, cols, o.dict, rowNo); len(rowErrs) > 0 {
			errs = append(errs, rowErrs...)
			continue
		}
		rows = append(rows, Row[T]{No: rowNo, Value: *row})
	}
	if err := iter.Error(); err != nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrBadWorkbook, err)
	}
	return rows, errs, nil
}

// decodeRow fills one row value, collecting a RowError per failing cell.
// A row with any failure is dropped from the result — all-or-nothing rows,
// not half-parsed records.
func decodeRow[T any](row *T, cells []string, colIndex map[string]int, cols []column, tr Translator, rowNo int) []RowError {
	var errs []RowError
	rv := reflect.ValueOf(row).Elem()
	for _, c := range cols {
		raw := ""
		if i := colIndex[c.header]; i < len(cells) {
			raw = cells[i]
		}
		if strings.TrimSpace(raw) == "" {
			if c.required {
				errs = append(errs, RowError{Row: rowNo, Column: c.header, Reason: "不能为空"})
			}
			continue
		}
		if err := parseCell(rv.Field(c.index), raw, c.dict, tr); err != nil {
			errs = append(errs, RowError{Row: rowNo, Column: c.header, Reason: err.Error()})
		}
	}
	return errs
}

// blank reports whether every present cell is empty. excelize trims
// trailing empty cells, so a nil/short slice is blank too.
func blank(cells []string) bool {
	for _, c := range cells {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}
