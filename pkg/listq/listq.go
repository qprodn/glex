// Package listq is the deep module behind every list read: it parses the
// AIP filter, order, and page-token trio against a module's declared
// filterable idents, applies the shared defaults, and mints next-page
// tokens. A module's only per-list fact is its Spec; the Query value flows
// application → domain → repo unchanged (ADR 0024).
//
// listq is a pure function package: no context, no logger. Callers log
// the one ADR 0011 breadcrumb under their own operation.
package listq

import (
	"fmt"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"go.einride.tech/aip/pagination"
)

// Request is the slice of the AIP list-request surface listq consumes;
// every generated List request satisfies it.
type Request interface {
	filtering.Request
	ordering.Request
	pagination.Request
}

const (
	// DefaultPageSize applies when the request omits one. All sixteen
	// pre-listq stanzas used this value.
	DefaultPageSize = 20

	// MaxPageSize is the shared ceiling; beyond it is an invalid argument
	// on the same path as a bad filter. Observed web need tops out at 200.
	MaxPageSize = 500
)

// Spec declares one list's filterable idents and default order — the only
// per-module fact. Standard filter functions are always declared; Idents
// carries only the field declarations.
type Spec struct {
	Idents       []filtering.DeclarationOption
	DefaultOrder []ordering.Field
}

// Query is the complete shape of one list read.
type Query struct {
	Filter  filtering.Filter
	OrderBy ordering.OrderBy
	Limit   int
	Offset int
}

// Window is the parsed page window alongside a Query. It mints next-page
// tokens from the request's own checksum, so it never mutates the request.
type Window struct {
	token pagination.PageToken
	size  int
}

// Parse parses the request against the spec, applies the shared defaults,
// and returns the query plus its page window. The request is not mutated:
// the effective page size lives in the Query and Window alone.
func Parse(req Request, spec Spec) (Query, Window, error) {
	if len(spec.DefaultOrder) == 0 {
		return Query{}, Window{}, fmt.Errorf("listq: spec must declare a default order")
	}
	declarations, err := filtering.NewDeclarations(
		append([]filtering.DeclarationOption{filtering.DeclareStandardFunctions()}, spec.Idents...)...,
	)
	if err != nil {
		return Query{}, Window{}, err
	}
	filter, err := filtering.ParseFilter(req, declarations)
	if err != nil {
		return Query{}, Window{}, err
	}
	pageToken, err := pagination.ParsePageToken(req)
	if err != nil {
		return Query{}, Window{}, err
	}
	orderBy, err := ordering.ParseOrderBy(req)
	if err != nil {
		return Query{}, Window{}, err
	}
	if len(orderBy.Fields) == 0 {
		orderBy.Fields = append([]ordering.Field(nil), spec.DefaultOrder...)
	}
	size := int(req.GetPageSize())
	if size <= 0 {
		size = DefaultPageSize
	}
	if size > MaxPageSize {
		return Query{}, Window{}, fmt.Errorf("page size %d exceeds the maximum %d", size, MaxPageSize)
	}
	return Query{
		Filter:  filter,
		OrderBy: orderBy,
		Limit:   size,
		Offset: int(pageToken.Offset),
	}, Window{token: pageToken, size: size}, nil
}

// NextToken mints the next page token when got reached the window size;
// a short page mints none. The token preserves the request checksum the
// page token was parsed with, so following it with the same request shape
// parses cleanly.
func (w Window) NextToken(got int) string {
	if got < w.size {
		return ""
	}
	next := w.token
	next.Offset += int64(w.size)
	return next.String()
}
