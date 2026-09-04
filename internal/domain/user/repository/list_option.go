package repository

import (
	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
)

type ListOption func(*ListOptions)

type ListOptions struct {
	Filter  filtering.Filter
	OrderBy ordering.OrderBy
	Offset  int
	Limit   int
	// DeptIDs restricts the listing to these departments (the
	// department-subtree filter; nil = no restriction).
	DeptIDs []int64
}

func ListFilter(filter filtering.Filter) ListOption {
	return func(o *ListOptions) {
		o.Filter = filter
	}
}

func ListOrderBy(orderBy ordering.OrderBy) ListOption {
	return func(o *ListOptions) {
		o.OrderBy = orderBy
	}
}

func ListOffset(offset int) ListOption {
	return func(o *ListOptions) {
		o.Offset = offset
	}
}

func ListLimit(limit int) ListOption {
	return func(o *ListOptions) {
		o.Limit = limit
	}
}

// ListDeptIDs restricts a listing to the given departments (the
// department-subtree filter resolved through the crossdomain org contract).
func ListDeptIDs(ids []int64) ListOption {
	return func(o *ListOptions) {
		o.DeptIDs = ids
	}
}
