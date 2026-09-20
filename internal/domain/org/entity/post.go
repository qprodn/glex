package entity

import (
	"time"

	"glex/internal/domain/org"
)

// Post is a post domain object (岗位), ported from RuoYi 6.0 sys_post —
// department-scoped: the name is unique within its department, the code is
// unique across all posts (ADR 0015).
type Post struct {
	ID        int64
	DeptID    int64
	PostCode  string
	PostName  string
	PostSort  int
	Status    int32
	Remark    string
	CreateBy  int64
	UpdateBy  int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PostUpdate pairs the persisted post with its pre-image so the service can
// key the in-use guard on the enable/disable transition.
type PostUpdate struct {
	Post     *Post
	Previous *Post
}

// Validate enforces the invariants a Post must satisfy before persistence.
func (p *Post) Validate() error {
	if p == nil || p.DeptID == 0 || p.PostName == "" || p.PostCode == "" {
		return org.ErrPostInvalid
	}
	return nil
}

// Enabled reports whether the post is assignable (0正常).
func (p *Post) Enabled() bool {
	return p != nil && p.Status == 0
}
