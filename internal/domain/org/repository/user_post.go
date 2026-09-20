package repository

import (
	"context"
)

// ReplaceUserPostsRequest is one full replacement of a user's Post
// Assignment: the user, the post ids to store, and the actor stamped into
// the audit fields of both the withdrawn and the inserted rows.
type ReplaceUserPostsRequest struct {
	UserID  int64
	PostIDs []int64
	ActorID int64
}

// UserPostRepo is the repository interface for user↔post links (the
// inversion seam declared in domain, implemented in data). The link table is
// sys_user_post (snowflake PK id, audit columns, soft delete — ADR 0002; one
// active row per (user_id, post_id) pair). Owned by the org module; the user
// module never reads it — it soft-deletes a user's links as part of the
// user delete (the sys_user_role precedent).
type UserPostRepo interface {
	// UserExists reports whether the System User exists and is not
	// soft-deleted — the existence gate behind the by-user endpoints.
	UserExists(ctx context.Context, userID int64) (bool, error)
	// UserDeptID returns the user's stored department reference (0 = none) —
	// the fallback behind the assignment dialog's candidate set. Missing
	// users return ErrUserNotFound.
	UserDeptID(ctx context.Context, userID int64) (int64, error)
	// AssignedPostIDs returns the ids of the user's active Post Assignment,
	// ordered by post_id.
	AssignedPostIDs(ctx context.Context, userID int64) ([]int64, error)
	// ReplaceUserPosts atomically replaces the user's Post Assignment with
	// req.PostIDs (full replacement of the active set; an empty list clears
	// all), stamping req.ActorID into the audit fields of both the withdrawn
	// and the inserted rows. req.PostIDs are stored verbatim after caller
	// validation and deduplication.
	ReplaceUserPosts(ctx context.Context, req ReplaceUserPostsRequest) error
	// CountUserPosts counts the active users assigned the post — the guard
	// behind "a post with users cannot be deleted or disabled".
	CountUserPosts(ctx context.Context, postID int64) (int, error)
}
