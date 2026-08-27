# glex — Identity & Access

The domain concerned with **who can operate the system, the navigation they see,
and how their identity is established**. It is organized as three modules in one
service: **user** (System User accounts), **access** (authorization — roles,
menus, grants, and permission resolution), and **authn** (authentication —
credentials, login sessions, and tokens), ported from RuoYi-Vue-Plus's
`sys_user`, `sys_menu`, `sys_role`, and `sys_user_role`.

## Language

### user

**System User** (系统用户):
A back-office operator who logs into the system and works within it; the unit of
identity and accountability being ported from RuoYi's `sys_user`. Two system
users are told apart by the roles assigned to them, not by a tier flag on the
record itself. The user module owns the operator's profile; Credentials and
Login Sessions belong to authn.
_Avoid_: User (ambiguous on its own), Account, Member, Admin.

### access

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
resolving a grant into permission handles is the access module's job.
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
per user, never per role; the link table and both of its endpoints — queried
by user and by role — belong to the access module. Deleting a user hard-deletes
its links with it; a role that still has users assigned cannot be deleted or
disabled.
_Avoid_: User-Role Link, User Role, Role Grant (a grant is menu→role; an
assignment is role→user).

**Menu** (菜单):
A node in the back-office navigation and permission tree, ported from RuoYi's
`sys_menu`. A Menu is one of three types — **Directory** (目录, groups menus),
**Menu** (菜单, a routeable page), or **Button** (按钮, a permission-gated
action); they nest via `parent_id` (0 = root). The `perms` field (e.g.
`system:user:list`) is a permission *handle* stored on the menu — resolving it
against a user's permissions is the access module's job, not the menu record's.
Which menus a Role carries is the Role's **Menu Grant**, not the Menu's concern.
_Avoid_: Route, Page, Tab, Permission (say Button or perms).

**Functional Permission** (功能权限):
A Menu `perms` handle made available to a System User through active Role
Assignments and Menu Grants; it authorizes an operation, independently of any
future restriction on which records that operation may access.
_Avoid_: Permission (ambiguous on its own), Data Permission, Data Scope.

**Authorization Resolution** (授权解析):
The computation, owned by the access module, that turns a System User's active
Role Assignments and Menu Grants into the Role Keys and Functional Permissions
captured in an **Authorization Snapshot**. authn requests it at login and never
derives permissions itself.
_Avoid_: Permission Cache, Snapshot Build.

**Superadmin** (超级管理员):
The Role key whose Authorization Resolution yields the wildcard Functional
Permission `*:*:*`; a carrier holding that wildcard matches every permission
handle by literal wildcard. Which Role key is Superadmin is decided by access's
resolution; matching the wildcard is the carrier's only job.
_Avoid_: Administrator, Root, Admin flag, tier flag.

### authn

**Credential** (凭证):
The secret material — currently a password hash — and its policy that verify a
System User during authentication. A Credential belongs to authn even though it
is stored alongside the System User record; changing or resetting it goes
through authn, orchestrated by the caller's flow.
_Avoid_: Password (ambiguous on its own — say Credential or password policy).

**Login Session** (登录会话):
One revocable sign-in by a System User on a client device. A System User may
hold several Login Sessions at once, and each is represented by its own token.
_Avoid_: Login State, Account Session, Token (the token represents the session).

**Session ID** (会话编号):
A non-secret identifier used to display and revoke a Login Session without
exposing its bearer token. Possessing a Session ID does not authenticate a
request.
_Avoid_: Token ID, Access Token.

**Online Session** (在线会话):
The monitoring view over the currently active Login Sessions: what an
administrator lists and force-revokes (强退) from the online monitor, and what a
System User lists and revokes among their own sessions. An Online Session is a
Login Session seen from outside; the term names the view, never a second kind
of session.
_Avoid_: Active User, Live Token, Concurrent Login.

**Authorization Snapshot** (授权快照):
The active Roles and Functional Permissions captured for a System User and
shared by all of that user's Login Sessions. The snapshot is carried by authn's
Login Sessions; its content is computed by the access module's Authorization
Resolution. A new login refreshes the shared snapshot; a Role Assignment or
Menu Grant change makes it stale and requires all affected Login Sessions to
end.
_Avoid_: Permission Cache, Live Permissions.
