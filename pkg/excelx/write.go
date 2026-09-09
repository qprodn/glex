package excelx

import (
	"fmt"
	"io"
	"reflect"

	"github.com/xuri/excelize/v2"
)

// options carries the injected Translator across read and write calls.
type options struct {
	dict Translator
}

// Option tunes a Read or a Write.
type Option func(*options)

// WithDict injects the dictionary Translator used by dict-tagged columns.
func WithDict(tr Translator) Option {
	return func(o *options) { o.dict = tr }
}

// Write streams rows as a single-sheet workbook onto w. The header row
// comes from the row type's tags in field order; data rows stream through
// excelize's StreamWriter, so the workbook is never fully materialized
// (ADR 0017's row target). Cells render as text — dictionary labels and
// identifiers keep their exact shape, dodging spreadsheet number precision.
func Write[T any](w io.Writer, sheet string, rows []T, opts ...Option) error {
	cols, err := columnsOf[T]()
	if err != nil {
		return err
	}
	o := options{}
	for _, opt := range opts {
		opt(&o)
	}
	if sheet == "" {
		sheet = "Sheet1"
	}
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	if sheet != "Sheet1" {
		if err := f.SetSheetName("Sheet1", sheet); err != nil {
			return fmt.Errorf("excelx: rename sheet: %w", err)
		}
	}
	sw, err := f.NewStreamWriter(sheet)
	if err != nil {
		return fmt.Errorf("excelx: stream writer: %w", err)
	}
	header := make([]any, len(cols))
	for i, c := range cols {
		header[i] = c.header
	}
	if err := sw.SetRow("A1", header); err != nil {
		return fmt.Errorf("excelx: header row: %w", err)
	}
	for ri, row := range rows {
		cells := make([]any, len(cols))
		rv := reflect.ValueOf(row)
		for ci, c := range cols {
			cells[ci] = cellValue(rv.Field(c.index), c.dict, o.dict)
		}
		cell, err := excelize.CoordinatesToCellName(1, ri+2)
		if err != nil {
			return fmt.Errorf("excelx: row %d address: %w", ri+2, err)
		}
		if err := sw.SetRow(cell, cells); err != nil {
			return fmt.Errorf("excelx: row %d: %w", ri+2, err)
		}
	}
	if err := sw.Flush(); err != nil {
		return fmt.Errorf("excelx: flush: %w", err)
	}
	if _, err := f.WriteTo(w); err != nil {
		return fmt.Errorf("excelx: write workbook: %w", err)
	}
	return nil
}
