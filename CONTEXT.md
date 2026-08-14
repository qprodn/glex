# glex — Identity & Access

The domain concerned with **who can operate the system, the navigation they see,
and how their identity is established**. This context covers the back-office user
base, menu/permission tree, and roles being ported from RuoYi-Vue-Plus's
`sys_user`, `sys_menu`, and `sys_role`, built fresh on Kratos — one module at a
time. System User is done; Menu is done; Role is done (basic CRUD).

## Language

**System User** (系统用户):
A back-office operator who logs into the system and works within it; the unit of
identity and accountability being ported from RuoYi's `sys_user`. Two system
users are told apart by the roles assigned to them, not by a tier flag on the
record itself.
_Avoid_: User (ambiguous on its own), Account, Member, Admin.

**Role** (角色):
A named bundle of permissions that can be granted to a System User, ported from
RuoYi's `sys_role`. Basic CRUD is implemented; the role is a standalone record
(`role_name`, `role_key`, `role_sort`, `status`, `remark`) with no links to
other tables yet — the role↔menu link (`sys_role_menu`) is deferred to the role
module's later phase, and user↔role assignment belongs to the auth module.
_Avoid_: Group, Profile, Tier.

**Menu** (菜单):
A node in the back-office navigation and permission tree, ported from RuoYi's
`sys_menu`. A Menu is one of three types — **Directory** (目录, groups menus),
**Menu** (菜单, a routeable page), or **Button** (按钮, a permission-gated
action); they nest via `parent_id` (0 = root). The `perms` field (e.g.
`system:user:list`) is a permission *handle* stored on the menu — resolving it
against a user's permissions is the auth module's job, not the menu module's.
The link between a role and its menus (`sys_role_menu`) is deferred to the role
module's later phase.
_Avoid_: Route, Page, Tab, Permission (say Button or perms).
