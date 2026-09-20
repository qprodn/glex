package entity

import (
	"time"

	"glex/internal/domain/org"
)

// Dept is a department domain object (部门), ported from RuoYi sys_dept. The
// tree shape is parent_id only (ADR 0014): Children is populated by
// BuildDeptTree on read paths and never persisted.
type Dept struct {
	ID       int64
	ParentID int64
	DeptName string
	OrderNum int
	// Leader as a System User id; 0 = none. Display-only: never validated
	// against sys_user (RuoYi parity — the editor restricts the choices).
	Leader    int64
	Phone     string
	Email     string
	Status    int32
	CreateBy  int64
	UpdateBy  int64
	CreatedAt time.Time
	UpdatedAt time.Time
	Children  []*Dept
}

// DeptUpdate pairs the persisted department with its pre-image so the
// service can key the ancestor cascade on the enable/disable transition.
type DeptUpdate struct {
	Dept     *Dept
	Previous *Dept
}

// Validate enforces the invariants a Dept must satisfy before persistence.
func (d *Dept) Validate() error {
	if d == nil || d.DeptName == "" {
		return org.ErrDeptInvalid
	}
	return nil
}

// Enabled reports whether the department is usable (0正常).
func (d *Dept) Enabled() bool {
	return d != nil && d.Status == 0
}
