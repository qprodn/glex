package excelx

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// The field tag is a semicolon-separated list: `name=<header>` declares the
// column (required on every mapped field), `required` marks cells that must
// be non-empty on import, and `dict=<type>` routes the cell through the
// injected Translator in both directions. Example:
//
//	UserName string `excel:"name=登录账号;required"`
//	Gender   int32  `excel:"name=性别;dict=sys_user_gender"`

// column is one mapped struct field with its tag-declared sheet behavior.
type column struct {
	header   string
	index    int
	required bool
	dict     string
}

// columnsOf reflects T's excel-tagged exported fields in declaration order —
// the sheet's column order on export and the header-matched set on import.
func columnsOf[T any]() ([]column, error) {
	t := reflect.TypeOf([0]T{}).Elem()
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("excelx: row type %s is not a struct", t)
	}
	var cols []column
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag, ok := f.Tag.Lookup("excel")
		if !ok {
			continue
		}
		c, err := parseTag(f, tag)
		if err != nil {
			return nil, err
		}
		c.index = i
		cols = append(cols, c)
	}
	if len(cols) == 0 {
		return nil, ErrNoColumns
	}
	return cols, nil
}

// parseTag validates one field's tag and kind. String and integer kinds are
// the v1 surface; everything else fails at schema-build time, not mid-sheet.
func parseTag(f reflect.StructField, tag string) (column, error) {
	var c column
	for _, part := range strings.Split(tag, ";") {
		part = strings.TrimSpace(part)
		switch {
		case part == "":
		case part == "required":
			c.required = true
		case strings.HasPrefix(part, "name="):
			c.header = strings.TrimSpace(strings.TrimPrefix(part, "name="))
		case strings.HasPrefix(part, "dict="):
			c.dict = strings.TrimSpace(strings.TrimPrefix(part, "dict="))
		default:
			return c, fmt.Errorf("excelx: field %s: unknown tag part %q", f.Name, part)
		}
	}
	if c.header == "" {
		return c, fmt.Errorf("excelx: field %s: tag needs name=<header>", f.Name)
	}
	switch f.Type.Kind() {
	case reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
	default:
		return c, fmt.Errorf("excelx: field %s: unsupported kind %s", f.Name, f.Type.Kind())
	}
	return c, nil
}

// cellValue renders one field for export: dict-tagged values translate
// value→label through the Translator, and unresolved labels (or a missing
// Translator) degrade to the raw value — a sour dictionary never fails an
// export.
func cellValue(fv reflect.Value, dict string, tr Translator) any {
	var s string
	if fv.Kind() == reflect.String {
		s = fv.String()
	} else {
		s = strconv.FormatInt(int64Value(fv), 10)
	}
	if dict != "" && tr != nil {
		if label, ok := tr.Label(dict, s); ok {
			return label
		}
	}
	return s
}

// int64Value widens any integer-kind field for formatting.
func int64Value(fv reflect.Value) int64 {
	switch fv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fv.Int()
	default:
		return int64(fv.Uint())
	}
}

// parseCell decodes one cell into the field: dict-tagged cells resolve
// label→value first, then the field's kind parses the text. The error text
// is the operator-facing RowError reason.
func parseCell(fv reflect.Value, raw, dict string, tr Translator) error {
	s := strings.TrimSpace(raw)
	if dict != "" {
		if tr == nil {
			return fmt.Errorf("未配置字典翻译（%s）", dict)
		}
		v, ok := tr.Value(dict, s)
		if !ok {
			return fmt.Errorf("无法识别的选项 %q", raw)
		}
		s = v
	}
	if fv.Kind() == reflect.String {
		fv.SetString(s)
		return nil
	}
	if fv.Kind() == reflect.Uint || fv.Kind() == reflect.Uint8 || fv.Kind() == reflect.Uint16 ||
		fv.Kind() == reflect.Uint32 || fv.Kind() == reflect.Uint64 {
		n, err := strconv.ParseUint(s, 10, fv.Type().Bits())
		if err != nil {
			return fmt.Errorf("%q 不是有效的整数", raw)
		}
		fv.SetUint(n)
		return nil
	}
	n, err := strconv.ParseInt(s, 10, fv.Type().Bits())
	if err != nil {
		return fmt.Errorf("%q 不是有效的整数", raw)
	}
	fv.SetInt(n)
	return nil
}
