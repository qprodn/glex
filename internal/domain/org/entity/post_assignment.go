package entity

// PostAssignment is one candidate post with whether the user is assigned it
// — the user editor's post dialog data. Candidates are the effective
// department's active posts, ordered by (post_sort, id).
type PostAssignment struct {
	Post     *Post
	Assigned bool
}
