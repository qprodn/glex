# glex — Identity & Access

The domain concerned with **who can operate the system, the navigation they see,
and how their identity is established**. This context covers the back-office user
base, menu/permission tree, roles, and the auth module being ported from
RuoYi-Vue-Plus's `sys_user`, `sys_menu`, `sys_role`, and `sys_user_role`, built
fresh on Kratos — one module at a time. System User is done; Menu is done; Role
is done (basic CRUD + **Menu Grant**); the auth module exists and owns the
user↔role link (**Role Assignment**), with login/session/permission resolution
still to come.

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
(`role_name`, `role_key`, `role_sort`, `status`, `remark`) carrying a
**Menu Grant**; its **Menu Check Strictly** flag shapes how that grant is
presented. A role that still has users assigned cannot be deleted or disabled.
_Avoid_: Group, Profile, Tier.

**Menu Grant** (菜单授权):
The set of Menus a Role carries, stored as role↔menu links (`sys_role_menu`).
Granting is **full-replacement**: saving a role's menus replaces the whole set
(empty set = no menus), never a diff. The grant is per role, never per user;
resolving a grant into permission handles is the auth module's job.
_Avoid_: Menu Assignment, Menu Permission, Perm Set.

**Menu Check Strictly** (菜单选择是否关联显示):
A Role flag, default on, controlling how the role editor treats parent/child
menu selection. When on, a linked Menu whose Menu id is also the parent of
another linked Menu is *half-checked* — it is not returned as a grant on its
own (its children are). When off, every linked Menu id is returned verbatim.

**Role Assignment** (角色分配):
The set of Roles a System User carries, stored as user↔role links
(`sys_user_role`). Assigning is **full-replacement**: saving a user's roles
replaces the whole set (empty set = no roles), never a diff. The assignment is
per user, never per role; the link table and its endpoints belong to the auth
module (`/system/user/{user_id}/roles`, `/system/role/{role_id}/users`).
Deleting a user hard-deletes its links with it; a role that still has users
assigned cannot be deleted or disabled. Resolving assignments (and **Menu
Grants**) into permission handles is also the auth module's job.
_Avoid_: User-Role Link, User Role, Role Grant (a grant is menu→role; an
assignment is role→user).

**Menu** (菜单):
A node in the back-office navigation and permission tree, ported from RuoYi's
`sys_menu`. A Menu is one of three types — **Directory** (目录, groups menus),
**Menu** (菜单, a routeable page), or **Button** (按钮, a permission-gated
action); they nest via `parent_id` (0 = root). The `perms` field (e.g.
`system:user:list`) is a permission *handle* stored on the menu — resolving it
against a user's permissions is the auth module's job, not the menu module's.
Which menus a Role carries is the role module's **Menu Grant**, not the menu
module's concern.
_Avoid_: Route, Page, Tab, Permission (say Button or perms).

**Functional Permission** (功能权限):
A Menu `perms` handle made available to a System User through active Role
Assignments and Menu Grants; it authorizes an operation, independently of any
future restriction on which records that operation may access.
_Avoid_: Permission (ambiguous on its own), Data Permission, Data Scope.

**Login Session** (登录会话):
One revocable sign-in by a System User on a client device. A System User may
hold several Login Sessions at once, and each is represented by its own token.
_Avoid_: Login State, Account Session, Token (the token represents the session).

**Session ID** (会话编号):
A non-secret identifier used to display and revoke a Login Session without
exposing its bearer token. Possessing a Session ID does not authenticate a
request.
_Avoid_: Token ID, Access Token.

**Authorization Snapshot** (授权快照):
The active Roles and Functional Permissions captured for a System User and
shared by all of that user's Login Sessions. A new login refreshes the shared
snapshot; a Role Assignment or Menu Grant change makes it stale and requires
all affected Login Sessions to end.
_Avoid_: Permission Cache, Live Permissions.
