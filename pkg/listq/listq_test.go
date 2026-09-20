package listq_test

import (
	"testing"

	dictv1 "glex/api/dict/v1"
	"glex/pkg/listq"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
)

// dictTypeSpec mirrors the dict module's real list spec: two string idents,
// default order id ascending.
func dictTypeSpec() listq.Spec {
	return listq.Spec{
		Idents: []filtering.DeclarationOption{
			filtering.DeclareIdent("dict_name", filtering.TypeString),
			filtering.DeclareIdent("dict_type", filtering.TypeString),
		},
		DefaultOrder: []ordering.Field{{Path: "id"}},
	}
}

func TestParseDefaults(t *testing.T) {
	q, page, err := listq.Parse(&dictv1.ListDictTypesRequest{}, dictTypeSpec())
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if q.Limit != listq.DefaultPageSize {
		t.Errorf("Limit = %d, want %d", q.Limit, listq.DefaultPageSize)
	}
	if q.Offset != 0 {
		t.Errorf("Offset = %d, want 0", q.Offset)
	}
	if q.Filter.CheckedExpr != nil {
		t.Errorf("Filter.CheckedExpr = %v, want nil", q.Filter.CheckedExpr)
	}
	if len(q.OrderBy.Fields) != 1 || q.OrderBy.Fields[0].Path != "id" {
		t.Errorf("OrderBy.Fields = %+v, want [{id}]", q.OrderBy.Fields)
	}
	if page.NextToken(20) == "" {
		t.Error("NextToken(full page) = empty, want a token")
	}
}

func TestParseExplicitValues(t *testing.T) {
	req := &dictv1.ListDictTypesRequest{
		PageSize: 50,
		Filter:   `dict_name="gender"`,
		OrderBy:  "dict_type desc",
	}
	q, _, err := listq.Parse(req, dictTypeSpec())
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if q.Limit != 50 {
		t.Errorf("Limit = %d, want 50", q.Limit)
	}
	if q.Filter.CheckedExpr == nil {
		t.Error("Filter.CheckedExpr = nil, want parsed filter")
	}
	want := []ordering.Field{{Path: "dict_type", Desc: true}}
	if len(q.OrderBy.Fields) != 1 || q.OrderBy.Fields[0] != want[0] {
		t.Errorf("OrderBy.Fields = %+v, want %+v", q.OrderBy.Fields, want)
	}
}

func TestParsePageSize(t *testing.T) {
	cases := []struct {
		name string
		size int32
		want int
	}{
		{"zero defaults", 0, 20},
		{"negative defaults", -5, 20},
		{"at cap", 500, 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, _, err := listq.Parse(&dictv1.ListDictTypesRequest{PageSize: tc.size}, dictTypeSpec())
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if q.Limit != tc.want {
				t.Errorf("Limit = %d, want %d", q.Limit, tc.want)
			}
		})
	}
	t.Run("over cap rejected", func(t *testing.T) {
		_, _, err := listq.Parse(&dictv1.ListDictTypesRequest{PageSize: 501}, dictTypeSpec())
		if err == nil {
			t.Fatal("Parse() error = nil, want rejection over MaxPageSize")
		}
	})
}

func TestParseRejectsBadInput(t *testing.T) {
	cases := []struct {
		name string
		req  *dictv1.ListDictTypesRequest
	}{
		{"unknown filter ident", &dictv1.ListDictTypesRequest{Filter: `no_such_field="x"`}},
		{"bad filter syntax", &dictv1.ListDictTypesRequest{Filter: `dict_name=`}},
		{"garbage page token", &dictv1.ListDictTypesRequest{PageToken: "!!!not-a-token"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := listq.Parse(tc.req, dictTypeSpec()); err == nil {
				t.Fatal("Parse() error = nil, want rejection")
			}
		})
	}
}

func TestParseRequiresDefaultOrder(t *testing.T) {
	spec := listq.Spec{Idents: dictTypeSpec().Idents}
	if _, _, err := listq.Parse(&dictv1.ListDictTypesRequest{}, spec); err == nil {
		t.Fatal("Parse() error = nil, want rejection when DefaultOrder missing")
	}
}

func TestParseDoesNotMutateRequest(t *testing.T) {
	req := &dictv1.ListDictTypesRequest{}
	if _, _, err := listq.Parse(req, dictTypeSpec()); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if req.GetPageSize() != 0 {
		t.Errorf("req.PageSize mutated to %d, want untouched request", req.GetPageSize())
	}
}

func TestWindowNextTokenRoundTrip(t *testing.T) {
	// The client follows a next token with the same request shape.
	page1 := &dictv1.ListDictTypesRequest{PageSize: 20}
	_, window, err := listq.Parse(page1, dictTypeSpec())
	if err != nil {
		t.Fatalf("Parse(page1) error = %v", err)
	}
	token := window.NextToken(20)
	if token == "" {
		t.Fatal("NextToken(full page) = empty, want a token")
	}
	if short := window.NextToken(19); short != "" {
		t.Errorf("NextToken(short page) = %q, want empty", short)
	}

	page2 := &dictv1.ListDictTypesRequest{PageSize: 20, PageToken: token}
	q2, _, err := listq.Parse(page2, dictTypeSpec())
	if err != nil {
		t.Fatalf("Parse(page2) error = %v", err)
	}
	if q2.Offset != 20 {
		t.Errorf("page2 Offset = %d, want 20", q2.Offset)
	}
	if q2.Limit != 20 {
		t.Errorf("page2 Limit = %d, want 20", q2.Limit)
	}
}

func TestWindowNextTokenChecksumBindsQueryShape(t *testing.T) {
	page1 := &dictv1.ListDictTypesRequest{PageSize: 20, Filter: `dict_name="gender"`}
	_, window, err := listq.Parse(page1, dictTypeSpec())
	if err != nil {
		t.Fatalf("Parse(page1) error = %v", err)
	}
	token := window.NextToken(20)

	// The checksum deliberately excludes page_size (einride clears it), so
	// following a token with a different page size stays legal.
	resized := &dictv1.ListDictTypesRequest{PageSize: 10, PageToken: token, Filter: `dict_name="gender"`}
	if _, _, err := listq.Parse(resized, dictTypeSpec()); err != nil {
		t.Fatalf("Parse(resized page) error = %v, want accepted", err)
	}

	// Changing the query itself no longer matches the request checksum.
	refiltered := &dictv1.ListDictTypesRequest{PageSize: 20, PageToken: token, Filter: `dict_name="other"`}
	if _, _, err := listq.Parse(refiltered, dictTypeSpec()); err == nil {
		t.Fatal("Parse(changed filter) error = nil, want checksum mismatch")
	}
}
