# glex — Identity & Access

The domain concerned with **who can operate the system, the navigation they see,
and how their identity is established**. This context covers the back-office user
base and menu/permission tree being ported from RuoYi-Vue-Plus's `sys_user` and
`sys_menu`, built fresh on Kratos — one module at a time. System User is done;
Menu is in progress.

## Language

**System User** (系统用户):
A back-office operator who logs into the system and works within it; the unit of
identity and accountability being ported from RuoYi's `sys_user`. Two system
users are told apart by the roles assigned to them, not by a tier flag on the
record itself.
_Avoid_: User (ambiguous on its own), Account, Member, Admin.

**Role**:
A named bundle of permissions that can be granted to a System User. Modeling and
scope of roles is decided per-module; not part of the System User MVP.
_Avoid_: Group, Profile, Tier.

**Menu** (菜单):
A node in the back-office navigation and permission tree, ported from RuoYi's
`sys_menu`. A Menu is one of three types — **Directory** (目录, groups menus),
**Menu** (菜单, a routeable page), or **Button** (按钮, a permission-gated
action); they nest via `parent_id` (0 = root). The `perms` field (e.g.
`system:user:list`) is a permission *handle* stored on the menu — resolving it
against a user's permissions is the auth module's job, not the menu module's. The
link between a role and its menus (`sys_role_menu`) lives in the role module.
_Avoid_: Route, Page, Tab, Permission (say Button or perms).
