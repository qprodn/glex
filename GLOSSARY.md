# glex — Back-office Administration

The domain concerned with **who can operate the system, the navigation they see,
how their identity is established, the controlled vocabularies the back office
renders choices from, the runtime parameters it tunes without a redeploy, the
organization structure it groups people by, the
files it stores, the payments it collects, and the recurring work it
schedules**. It is organized as ten modules in one service: **user**
(System User accounts), **org** (organization structure — departments, posts,
and post assignments), **access** (authorization — roles, menus, grants, and
permission resolution), **authn** (authentication — credentials, login
sessions, and tokens), **dict** (dictionaries — the labeled option vocabularies
pages render selects and status tags from), **param** (runtime parameters —
the named system-wide values administrators tune without a redeploy), **file**
(file storage —
accepting uploads, serving downloads, and withdrawing Stored Files), and
**audit** (the write-once operation and login logs, minted by the transport
middleware and authn rather than exposed as an API service of its own), ported
from RuoYi-Vue-Plus's `sys_user`, `sys_dept`, `sys_post`, `sys_menu`,
`sys_role`, `sys_user_role`, `sys_user_post`, `sys_dict_type`,
`sys_dict_data`, `sys_config`, `sys_oper_log`, and `sys_logininfor`; the file module
continues that lineage with its own `sys_file` (RuoYi's `sys_oss`, renamed).
Two further modules, **pay** (payment — the Channel Merchant Accounts the back
office configures and the payments and refunds collected through them) and
**job** (scheduled tasks — the Scheduled Jobs the back office configures and
the Job Executors they fire), are designed natively for glex rather than ported.

## Language

### user

**System User** (系统用户):
A back-office operator who logs into the system and works within it; the unit of
identity and accountability being ported from RuoYi's `sys_user`. Two system
users are told apart by the roles assigned to them, not by a tier flag on the
record itself. The user module owns the operator's profile; Credentials and
Login Sessions belong to authn.
_Avoid_: User (ambiguous on its own), Account, Member, Admin.

### org

**Department** (部门):
A node in the organization tree that places System Users and Posts in the
structure, ported from RuoYi's `sys_dept`. Departments nest through their
parent (0 = root); the tree shape is derived when read, never stored as a path.
A System User references at most one Department as their place in the
structure; a Post belongs to exactly one. A Department that is the seeded root,
has children, is referenced by System Users, or still carries Posts cannot be
withdrawn; one with users or enabled children cannot be disabled; enabling a
Department enables its ancestors with it. Its optional leader is a System User
id recorded for display.
_Avoid_: Org Unit, Team, Unit, Branch, Section.

**Post** (岗位):
A position within exactly one Department, ported from RuoYi's `sys_post`
following its 6.0 shape: posts are department-scoped, the name is unique within
its Department, and the code is unique across all Posts. A Post that still has
System Users assigned cannot be withdrawn or disabled.
_Avoid_: Position, Job, Title, Role (a Role bundles permissions; a Post names a
position in a Department).

**Post Assignment** (岗位分配):
The set of Posts a System User carries, stored as user↔post links
(`sys_user_post`). Assigning is **full-replacement** with the same semantics as
a Role Assignment: saving a user's posts replaces the whole set (empty set =
no posts), never a diff. The link table and both of its endpoints — queried by
user and by post — belong to the org module. Withdrawing a System User
soft-deletes its links with it.
_Avoid_: User-Post Link, User Post, Post Grant.

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
by user and by role — belong to the access module. Deleting a user soft-deletes
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

### dict

**Dict Type** (字典类型):
A named, system-wide vocabulary of labeled options, ported from RuoYi's
`sys_dict_type` and identified by its key (`dict_type`, e.g.
`sys_normal_disable`). The key is immutable once the type is created — it is
referenced by pages and validations as an identifier — while the display name
stays editable. A Dict Type that still has active Dict Entries cannot be
withdrawn.
_Avoid_: Dictionary, Enum, Config, Dict Category.

**Dict Entry** (字典项):
One labeled option inside a Dict Type: a machine value (`dict_value`), a
display label, a sort position, a tag style, and a default flag. The pair
(Dict Type key, value) is unique among active entries. Pages consume entries
to render selects and status tags; sorting is by sort position.
_Avoid_: Dict Data, Option, Enum Value, Dictionary Item.

### param

**Runtime Parameter** (运行时参数):
A named, system-wide value an administrator changes at runtime without a
redeploy, ported from RuoYi's `sys_config`; identified by its key (e.g.
`sys.account.registerUser`) and carried as a plain string — parsing it is the
consumer's job. A value fixed at deploy time in the bootstrap yaml is static
configuration, not a Runtime Parameter; a static knob becomes one only when
its consumer switches to reading the param module, never by seeding a
look-alike row beside the live one.
_Avoid_: Config (overloaded by RuoYi), Setting, Option, 系统参数 (unqualified).

**Built-in Parameter** (内置参数):
A Runtime Parameter protected from withdrawal because the system assumes it
exists: it may be re-valued at will but never withdrawn. The flag is set at
create and immutable afterwards — it governs withdrawability only, and no
update path can strip it.
_Avoid_: system parameter, fixed parameter, config_type.

### file

**Stored File** (存储文件):
One file accepted into the system's storage: a metadata record — original
name, type, size, Content Fingerprint, uploader — paired with the stored
bytes it describes. Withdrawing a Stored File keeps the record as audit
history but removes the bytes; a withdrawn Stored File can be remembered,
never downloaded again.
_Avoid_: OSS File, Attachment, Upload (the act, not the thing), Asset.

**Content Fingerprint** (内容指纹):
The digest of a Stored File's bytes, computed by the service when it accepts
the upload. It attests what was stored; it does not unify — accepting
identical bytes twice yields two Stored Files.
_Avoid_: checksum, hash, MD5.

**Stored Key** (存储键):
The address of a Stored File's bytes inside the single bucket: an optional
business prefix, date directories, and a name carrying the row's identity and
the original suffix. It locates bytes; it is never persisted as a URL, and
attachment downloads still flow only through the service (ADR 0008).
_Avoid_: object key (S3-speak), path, file path.

**Display URL** (展示链接):
A short-TTL presigned GET URL the service mints at read time for a Stored
File, carried on every Stored File response and on translated reads such as
the avatar (ADR 0016). Holding one is storage access until it lapses; it is
derived, never stored, and withdrawn files stop resolving.
_Avoid_: public URL, file URL, link (unqualified), CDN address.

### pay

**Channel** (支付渠道):
One external payment provider the system collects money through — WeChat
Pay, Alipay, UnionPay, Stripe — identified by a stable code. A Channel fixes
the shape of the credentials its Merchant Accounts carry and the wire
behavior its adapter speaks; money moves at the Channel.
_Avoid_: Pay Method (a different concept), gateway, provider, 第三方支付
(unqualified).

**Merchant Account** (商户账号):
The platform's own collecting account at one Channel — the WeChat Pay mchid
with its keys and certificates, the Alipay app, the Stripe account —
configured from the back office with its credential material held encrypted
(ADR 0018). A payment is collected into exactly one Merchant Account;
several may be configured per Channel and are picked explicitly when a
payment is initiated.
_Avoid_: 商户 (unqualified), sub-merchant / 子商户 (service-provider mode, out of
scope), tenant, vendor.

**Pay Method** (支付方式):
The form a payment takes on the paying side — Native (QR code), JSAPI /
Mini Program, H5, App — chosen per initiation within a Channel; the
Channel's response carries the method-specific payload (code URL, prepay
info, redirect).
_Avoid_: Channel (a different concept), 支付类型, trade type (WeChat wire
jargon).

**Payment** (支付单):
One attempt to collect a stated amount into one Merchant Account through one
Pay Method, identified by its own number — which also serves as the
Channel's merchant order number for it. A Payment ends in exactly one
terminal state: paid, failed, closed, or expired. Initiating again for the
same Business Order Number closes that Merchant Account's still-pending
Payments first.
_Avoid_: 订单 (the caller's business order), transaction, trade, 支付记录.

**Business Order Number** (业务单号):
The caller-supplied identifier tying a Payment to the business thing being
paid for. To pay it is opaque reference material — indexed for lookup,
never interpreted, and not unique among Payments.
_Avoid_: 支付单号 (the Payment's own number), out_trade_no, order id.

**Refund** (退款单):
One request to return part or all of a paid Payment's amount through the
Channel it arrived on. A Payment may carry several Refunds, together never
exceeding what was paid; each ends refunded or failed.
_Avoid_: 退款记录, chargeback (a card-dispute concept, the Channel's concern),
撤销.

**Channel Notification** (渠道回调):
A Channel's asynchronous assertion about a Payment or Refund, pushed to the
module's notify endpoint and accepted only with a valid request signature —
the signature is the authentication (ADR 0020). Each is recorded write-once
with its raw body as the evidence stream. A redelivery keeps that one row
and applies no second state change, though a state change that did not
finish can finish on a later delivery.
_Avoid_: webhook, 异步通知 (unqualified), callback.

**Connectivity Test** (连通性测试):
A cheap authenticated Channel call made on demand to prove a Merchant
Account's credentials work. It leaves no Payment and moves no money.
_Avoid_: 验证支付, test pay, ping.

**Payment Completed** (支付完成事件):
The Domain Event asserting a Payment has reached its paid terminal state,
published by the pay module's Channel Notification intake once the state
change is durable (the query-settle and close-flip paths settle silently
for now — absence of the event is not absence of payment). Subscribers
treat it as a prompt to look at the Payment, never as proof for moving
money — the Payment's own state stays the single money truth. It is the
first fact on the pay module's Topic and the anchor use case of the
eventbus (ADR 0022).
_Avoid_: 支付成功通知 (a Channel Notification is the Channel telling us;
this is us telling subscribers), callback, webhook.

### job

**Scheduled Job** (定时任务):
A back-office-configured recurring firing of one Job Executor on a 5-field cron
schedule, in the one configured scheduler timezone (ADR 0023). A job row names
its executor by its registered handler string — validated against the live
registry at save time — and never carries executable text. Firing is
best-effort: one occurrence runs at most once across replicas (a Redis fire
claim keyed by the scheduled moment), runs of the same job never overlap
(a per-job run lock), and occurrences missed while the process was down are
never backfilled. Pausing stops future firings; a run already in flight
finishes.
_Avoid_: 定时器, task (too generic), cron job (the expression is one field of
it), quartz, misfire policy.

**Job Executor** (任务执行器):
The named Go handler a Scheduled Job fires — registered in code by the module
owning the work and referenced by name from job rows, never resolved by
reflecting on operator input (ADR 0023). Its Run receives the job's args
verbatim and must tolerate the same firing happening twice and a firing never
happening — the same discipline eventbus handlers carry. The Name/Run shape is
deliberately the shape external schedulers bind, which is what keeps them a
config-flip away.
_Avoid_: invoke target, bean, worker, runner.

**Job Log** (任务日志):
The record of one completed firing: trigger (scheduled, manual, or the reserved
external), outcome, summary or error text, start time, and duration. A row is
written once at completion — there is no running state and no update path —
and is afterwards only queried or withdrawn, the write-once spirit of the
audit streams (ADR 0012) without their transport provenance.
_Avoid_: execution history, run record, 任务记录.

### eventbus

**Domain Event** (领域事件):
A fact that has already happened in one module's records — a Payment
reaching its paid state, say — published only after it is durably true, for
any other module to react to. The publishing module does not know its
subscribers; reaction never blocks the fact.
_Avoid_: message, notification (that names a Channel Notification), callback.

**Topic** (主题):
The named destination, owned by the publishing module, where that module's
Domain Events are published. Each module keeps few Topics and puts many
Event Types in one.
_Avoid_: queue, channel (taken — a payment Channel), exchange, stream.

**Event Type** (事件类型):
The discriminator naming which fact happened, carried inside a Topic; a
subscriber decides relevance by it.
_Avoid_: event name, kind, op type.

**Consumer Group** (消费组):
The named unit of subscription: each Consumer Group receives a Topic's
Domain Events independently of every other group, in order within the
group. Delivery is best-effort — handlers must tolerate both receiving the
same event again and, after a restart, missing it.
_Avoid_: subscriber, listener, worker.

**Dead Letter Event** (死信事件):
A Domain Event that exhausted its retry budget without being handled,
recorded with its payload for inspection rather than retried forever — a
record that something needs a person, not an error in the fact itself.
_Avoid_: poison message, failed event.

### cross-module

**Audit Fields** (审计字段):
The operator and lifecycle record every persisted table carries without
exception: who created the record (`create_by`), who last changed it
(`update_by`), when each happened (`created_at` / `updated_at`), and whether
it has been withdrawn (`deleted_at`). They are filled server-side from the
authenticated System User (the Principal), never accepted from a client; 0
means the system itself acted (bootstrap, migration).
_Avoid_: metadata, base columns, common fields.

**Soft Delete** (软删除):
Withdrawing a record by marking it (`deleted_at` set) instead of removing the
row; every query treats marked records as absent. No table in the system
hard-deletes rows. Withdrawal is permanent for that record: a link granted
again after withdrawal is a new record with its own creation audit, while the
withdrawn generation stays as history.
_Avoid_: logical delete, del_flag, is_deleted.

### import/export

**Data Import** (数据导入):
Applying rows read from an Import File to a module's records as one
indivisible batch — either every row validates and writes, or none does. The
module doing the importing is named by the Import File's business prefix.
_Avoid_: Excel upload, batch upload, 数据上传.

**Import File** (导入文件):
The Stored File offered to a module as the row source of a Data Import; it
remains stored afterwards as the record of what was offered, identified
across modules by its Stored File id.
_Avoid_: source attachment, upload file, Excel 文件 (unqualified).

**Data Export** (数据导出):
A module's records rendered as a spreadsheet and delivered straight to the
requesting operator as a download. The system keeps no copy of what left —
the operation log is the only record that an export happened.
_Avoid_: export file (there is no file), download URL, 导出文件.
