# Posts are department-scoped

`sys_post` follows RuoYi-Vue-Plus 6.0's shape, not classic RuoYi's: every Post
belongs to exactly one Department (`dept_id NOT NULL`), the post name is unique
within its Department, the post code is unique across all Posts, and the user
editor offers only the posts of the user's own Department (decided 2026-09-04).

## Considered Options

- **Chosen:** dept-scoped posts (6.0 fidelity). The vendored reference is
  6.0.0, and per-department positions ("研发部-前端开发") model a real
  organization better than one global list.
- **Rejected:** classic global posts (RuoYi 4.x — most people's muscle memory).
  A simpler form with no department select and global name uniqueness, but it
  diverges from the reference we port everything else from, and one shared
  post list degenerates into a flat free-for-all in multi-department offices.

## Consequences

- Post CRUD and the post page need the department tree; the post list filters
  by exact department and by department subtree.
- A user's post choices follow their department: the user editor loads posts
  for the chosen department, and moving a user across departments drops
  post selections that no longer apply.
- Uniqueness is two rules, not one: name per department, code globally.
- Reversing this later means a data migration (denormalize `dept_id`, merge
  name collisions), which is why it is an ADR.
