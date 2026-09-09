// Package excelx is the stateless spreadsheet codec behind Data Import and
// Data Export (ADR 0017): rows are declared by struct tag, export streams a
// workbook onto an io.Writer, and import iterates sheet rows from an
// io.Reader collecting per-cell failures. The codec makes no capability
// calls — dictionary translation rides an injected Translator, so the
// calling application service owns every fetch.
package excelx

import (
	"errors"
	"fmt"
)

// Sentinel conditions the calling application maps onto its wire errors;
// reasons stay programmer-facing English, row-level reasons (RowError) are
// the operator's language.
var (
	// ErrBadWorkbook: the bytes are not a readable xlsx workbook.
	ErrBadWorkbook = errors.New("excelx: bad workbook")
	// ErrNoSheet: the workbook contains no sheet.
	ErrNoSheet = errors.New("excelx: workbook has no sheet")
	// ErrNoHeader: the first sheet has no header row.
	ErrNoHeader = errors.New("excelx: sheet has no header row")
	// ErrNoColumns: the row type declares no excel-tagged fields.
	ErrNoColumns = errors.New("excelx: row type declares no excel columns")
	// ErrMissingColumn: the header row lacks a declared column; the wrapped
	// text lists them.
	ErrMissingColumn = errors.New("excelx: workbook misses declared columns")
	// ErrTooManyRows: the sheet carries more data rows than the caller's
	// cap.
	ErrTooManyRows = errors.New("excelx: row count exceeds the cap")
)

// RowError is one cell-level import failure: the 1-based sheet row (the
// header is row 1), the column's header, and an operator-readable reason.
type RowError struct {
	Row    int
	Column string
	Reason string
}

func (e RowError) Error() string {
	return fmt.Sprintf("第%d行 [%s] %s", e.Row, e.Column, e.Reason)
}

// Translator resolves dictionary values and labels for dict-tagged columns.
// The calling application builds it from its dict capability and injects it
// — excelx itself never fetches anything (ADR 0017).
type Translator interface {
	// Label returns the display label for a stored value. ok=false makes
	// export fall back to the raw value — display degrades, the export
	// never fails on a sour dictionary.
	Label(dictType, value string) (label string, ok bool)
	// Value resolves a cell's label (or the raw stored value) back to the
	// stored value. ok=false is an import RowError.
	Value(dictType, label string) (value string, ok bool)
}
