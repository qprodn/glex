package entity

import (
	"time"
)

// RoleAssignment is one role with whether the user is assigned it — the
// role-assignment read shape for a user's Role Assignment (角色分配), mirroring
// RuoYi's authRole dialog data (all roles + checked flag).
type RoleAssignment struct {
	Role     *Role
	Assigned bool
}

// RoleUser is the System User projection the by-role endpoint returns: a flat
// profile snapshot owned by access because the endpoint and its link storage
// are access's (ADR 0005). It is a read shape, not the user module's
// aggregate.
type RoleUser struct {
	ID          int64
	UserName    string
	NickName    string
	Email       string
	PhoneNumber string
	Gender      int32
	Status      int32
	Avatar      string
	Remark      string
	CreateBy    int64
	UpdateBy    int64
	LoginIP     string
	LoginDate   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
