# WeChat Platforms and WeChat Apps

Status: ready-for-agent

## Problem Statement

The operator runs one back office and needs many WeChat official accounts and
mini programs connected to it. Some are granted by their administrators
through a WeChat third-party platform; the operator may register several of
those platforms. Others are held directly, by this system keeping that app's
own secret. Once an app is connected, the operator publishes its menu, adopts
templates, and sends one message to one person, and outside programs complete
Mini Program Login and Web Authorization.

WeChat's identity of a person must not become a System User or a Login
Session, and this work must not disturb WeChat Pay.

## Solution

A wechat module owns the WeChat Platforms, the WeChat Apps, and the Open API
calls those apps make. Each WeChat App is connected in one way, either a
Platform Authorization through exactly one platform or a Direct Hold, and
that connection is not edited in place. Callers and the back office use the
same operations once the app has a token; they do not choose a mode.

Service Accounts can keep a Custom Menu, send Template Messages and
Subscribe Notifications, and run Web Authorization. Mini programs can run
Mini Program Login and send Subscribe Messages. Subscription Accounts can be
connected, and those capabilities are refused on them. Messaging templates
come from WeChat's public library. Each send is one WeChat Dispatch to one
openid.

Mini Program Login and Web Authorization return the openid and, when WeChat
supplies one, the unionid. Web Authorization can also return a nickname and
an avatar when that app asked for them, and the caller claims the result
once with a Handoff Code. Nothing in those flows is stored as a WeChat User,
a System User, or a Login Session, and `session_key` is neither stored nor
returned.

Secrets stay ciphertext under a dedicated environment master key, using the
same scheme as merchant credentials. Ordinary reads show masks.

## User Stories

1. As an operator, I want to register a WeChat Platform with a display name, component AppID, component secret, Token, and EncodingAESKey, so that this system can receive grants and event pushes for that platform.
2. As an operator, I want each registered platform to show its own callback address, so that I can paste that address into that platform's WeChat configuration.
3. As an operator, I want a newly registered platform to stay unready until WeChat has pushed it a verify ticket, so that I do not start grants against a platform that cannot yet take a token.
4. As an operator, I want a verify ticket pushed to one platform to make that platform ready and leave every other platform unchanged, so that the platforms stay independent.
5. As an operator, I want a later verify ticket for the same platform to replace the one it holds, so that the platform keeps using WeChat's current ticket.
6. As an operator, I want the loss of a platform's verify ticket to make that platform unready without lapsing any WeChat App, so that a missing ticket is a platform problem and not a cancelled grant.
7. As an operator, I want ordinary reads of a platform to show masks for the component secret, Token, and EncodingAESKey, so that those secrets are not sitting in the back office or in API responses.
8. As an operator, I want to replace a platform's component secret, Token, and EncodingAESKey on that same platform, so that I can rotate them while authorized apps still connect through it.
9. As an operator, I want a platform's component AppID to stay fixed for the life of that row, so that the platform I authorized apps onto cannot be silently retargeted.
10. As an operator, I want to correct a platform's display name, so that the list stays recognizable without becoming a different platform.
11. As an operator, I want a WeChat Connectivity Test on a ready platform, so that I can prove that platform's registration still yields a token.
12. As an operator, I want a platform connectivity test that cannot obtain a token to fail and leave every WeChat App as it was, so that a platform failure does not lapse grants.
13. As an operator, I want a successful platform connectivity test to leave every WeChat App as it was, so that the test is only evidence about the platform.
14. As an operator, I want WeChat's full-network release test traffic on a platform's callback to be answered and to create neither a WeChat App nor a WeChat Dispatch, so that the platform can be released without polluting the back office.
15. As an operator, I want to withdraw a platform that has no non-withdrawn app connected through it, so that a platform I no longer use leaves the live list.
16. As an operator, I want withdrawal of a platform to be refused while any non-withdrawn app still connects through it, so that I cannot orphan a live grant.
17. As an operator, I want a withdrawn platform to stay withdrawn, with the already withdrawn apps and their dispatches still in history, so that removing a platform does not rewrite the past.
18. As an operator, I want a second live platform with the same component AppID to be refused, so that one component AppID identifies one live platform.
19. As an operator, I want to register the same component AppID again after the previous platform row has been withdrawn, and to get a new platform row, so that a component AppID can return without reviving the old row.
20. As an operator, I want the live platform list to omit withdrawn platforms, so that the working set is the platforms I can still use.
21. As an operator, I want to connect a mini program by Direct Hold by entering its name, AppID, and AppSecret, so that I can keep a token for an app this system itself holds.
22. As an operator, I want to connect an official account by Direct Hold only when I state whether it is a Service Account or a Subscription Account, so that later capability checks have a kind to enforce.
23. As an operator, I want a new Direct Hold to generate a Push Secret and return the Token and EncodingAESKey in full in that creation response, so that I can paste them into WeChat and select encrypted pushes.
24. As an operator, I want every later read of a Direct Hold to show the Push Secret and the AppSecret as masks, so that the one-time reveal is the only time the generated pair is shown.
25. As an operator, I want each Direct Hold to show its own callback address, so that I can paste it into that app's WeChat configuration.
26. As an operator, I want to regenerate a Direct Hold's Push Secret and receive the new pair in full once, so that I can recover from a lost copy.
27. As an operator, I want regeneration to invalidate the previous Push Secret immediately and leave the app unlapsed, so that a local rotation is not mistaken for WeChat rejecting the app.
28. As an operator, I want regeneration of a Push Secret to be refused on a Platform Authorization, so that platform messaging keys stay on the platform that owns them.
29. As an operator, I want to submit a replacement AppSecret on a Direct Hold and have that same app restored if it had lapsed, so that a replaced secret continues the same app.
30. As an operator, I want a Platform Authorization to refuse an AppSecret, so that a granted app never starts holding the authorizer's secret.
31. As an operator, I want a second live WeChat App with the same AppID to be refused, whether the existing row is a Direct Hold or a Platform Authorization, so that one AppID identifies one live app.
32. As an operator, I want the same AppID to be connectable again only after the previous app row has been withdrawn, and only as a new row, so that a return of the AppID does not revive the withdrawn app.
33. As an operator, I want to start a Platform Authorization only by choosing one ready platform, so that the grant lands on a platform that can take a token.
34. As an operator, I want starting a Platform Authorization on an unready or withdrawn platform to be refused, so that a grant is not attached to a platform that cannot refresh tokens.
35. As an operator, I want a completed grant to create a WeChat App from what WeChat reports — AppID, name, official account or mini program, Service Account or Subscription Account for an official account, and the granted capabilities — bound to the platform I selected, so that the row matches WeChat rather than a typed fiction.
36. As an operator, I want an official-account grant that does not report Service Account or Subscription Account to create no WeChat App, so that a row without a kind cannot pass capability checks later.
37. As an operator, I want a completed grant for an AppID that already has a live app on that same platform to restore that same app, clear a lapse, and refresh the granted capabilities, so that re-authorization continues one app.
38. As an operator, I want re-authorization to be refused while that app's platform is unready or withdrawn, so that I cannot restore a grant the platform cannot yet carry.
39. As an operator, I want a grant for an AppID that already belongs to a live app on another platform, or to a live Direct Hold, to leave that existing app where it is and create no second row, so that moving an app is a deliberate withdraw-and-create.
40. As an operator, I want a Direct Hold to be unable to turn into a Platform Authorization, and a Platform Authorization to be unable to turn into a Direct Hold or to move to another platform, so that the connection I see on the row is the connection that will be used.
41. As an operator, I want to withdraw a WeChat App, so that it leaves the live list without being erased.
42. As an operator, I want a withdrawn app's WeChat Dispatches, saved Custom Menu, and Adopted Templates to remain with that app, so that history stays on the row that produced it.
43. As an operator, I want the live app list to omit withdrawn apps, so that the working set is the apps I can still operate.
44. As an operator, I want to suspend a WeChat App and later resume it, so that I can stop its use without withdrawing it or waiting for WeChat to refuse it.
45. As an operator, I want a suspended app to refuse publishing a menu, adopting a template, sending a dispatch, Mini Program Login, and Web Authorization, and still accept a WeChat Connectivity Test, so that suspension is an operator switch and not a lapse.
46. As an operator, I want resuming an app to leave a lapse in place, so that clearing my switch does not pretend WeChat has accepted the app again.
47. As an operator, I want a WeChat Connectivity Test on an app to call WeChat with that app's current token and to create no WeChat Dispatch, so that I can prove the token without sending a message.
48. As an operator, I want a successful app connectivity test to leave the app's suspension, lapse, and connection as they were, so that a passing probe is not a state change.
49. As an operator, I want a connectivity test, publish, send, login, or web authorization that WeChat definitively refuses — the grant is cancelled or the Direct Hold secret is rejected — to lapse that same app and still create no successful dispatch, so that a dead grant stops being used.
50. As an operator, I want a timeout or an unavailable WeChat call to fail that call and leave the app unlapsed, so that a blip is not recorded as WeChat cancelling the grant.
51. As an operator, I want a business rejection, such as a person who has not subscribed or a js_code WeChat will not exchange, to fail that call, leave a rejected dispatch only when the call was a send, and leave the app unlapsed, so that ordinary WeChat refusals stay on the attempt.
52. As an operator, I want a lapsed app to refuse publishing, dispatch, login, web authorization, and connectivity testing until it is restored, so that I do not keep probing a token WeChat has already rejected.
53. As an operator, I want a withdrawn app to refuse those same calls, so that withdrawal is final for operations.
54. As an operator, I want an unready platform to fail token refresh and every token-using call for apps authorized through it, and to leave those apps unlapsed, so that the outage stays on that platform.
55. As an operator, I want apps on other platforms, and every Direct Hold, to keep working while one platform is unready, so that one platform's ticket does not take down the module.
56. As an operator, I want a Subscription Account to be connectable, and to have Custom Menu, Template Message, Subscribe Notification, and Web Authorization refused locally, so that I do not call WeChat for capabilities that kind does not have.
57. As an operator, I want a mini program to refuse Custom Menu, Template Message, Subscribe Notification, and Web Authorization locally, so that those official-account capabilities stay on official accounts.
58. As an operator, I want an official account to refuse Mini Program Login and Subscribe Message locally, so that those mini-program capabilities stay on mini programs.
59. As an operator, I want a Platform Authorization that was granted only some of this phase's capabilities to refuse the missing ones locally, so that a partial grant fails on the capability it lacks and still performs the others.
60. As an operator, I want a local capability refusal to happen before any WeChat call and to leave the app unlapsed, so that a kind or permission mismatch is not recorded as WeChat cancelling the app.
61. As an operator, I want to edit a Service Account's Custom Menu and save it in this system without calling WeChat, so that a draft is not yet the menu followers see.
62. As an operator, I want a menu button to open a web page or a mini program, and a button to hold sub-buttons instead of opening anything itself, so that the saved menu can use the structure of WeChat's default menu.
63. As an operator, I want publishing to send the whole saved menu and replace that Service Account's default menu at WeChat, so that WeChat shows one menu and not a patchwork of edits.
64. As an operator, I want a refused or timed-out publish to leave the app unlapsed and leave the previously published menu as the last one WeChat accepted, so that a failed publish is not reported as a new live menu.
65. As an operator, I want to adopt a template from WeChat's public library onto one WeChat App, so that later sends fill a template that app actually has.
66. As an operator, I want adopting the same library template again on the same app to be refused, so that the app has one Adopted Template for that library entry.
67. As an operator, I want template adoption to use the app's token and to be refused when the app is suspended, lapsed, withdrawn, or its platform is unready, so that adoption is a real WeChat call and not a local fiction.
68. As an operator, I want to send one Template Message, Subscribe Notification, or Subscribe Message to one openid and wait until WeChat accepts or rejects it, so that the back office shows the outcome of that attempt before I continue.
69. As an operator, I want a send without an Adopted Template, or without an openid, to be refused and to create no WeChat Dispatch, so that there is no empty attempt on record.
70. As an operator, I want an accepted send to record a new WeChat Dispatch with WeChat's msgid, the openid, the template, the filled content, and any web URL or mini-program page I set, so that I can tell acceptance apart from delivery.
71. As an operator, I want a send that sets both a web URL and a mini-program page to record both and hand both to WeChat, so that WeChat's own preference for the mini program is the one that applies.
72. As an operator, I want a rejected send, including a person who has not subscribed, to record a new WeChat Dispatch as rejected and without an msgid, so that the refusal is visible and is not treated as delivered.
73. As an operator, I want pressing send again to create another WeChat Dispatch, so that a second click is a second attempt even if an earlier timeout may already have reached the person.
74. As an operator, I want a send timeout to fail the call, leave the app unlapsed, and not record an accepted dispatch, so that an unknown outcome is not shown as WeChat having accepted it.
75. As an operator, I want a later delivery report that carries an msgid to update that same dispatch, including when the app has since been withdrawn, so that acceptance and delivery stay on one attempt.
76. As an operator, I want a delivery report that matches no dispatch to be acknowledged with no new row, so that an unknown msgid does not become a fake send.
77. As an operator, I want follow, text, and other verified pushes to be acknowledged with no stored row and no automatic reply, so that this phase does not grow a conversation inbox.
78. As an operator, I want a plaintext or compatible-mode push, on a platform callback or a Direct Hold callback, to be rejected and to leave the app unlapsed, so that only encrypted pushes are accepted.
79. As an operator, I want a push that fails verification to be rejected and to change no app and no dispatch, so that a bad signature cannot lapse an app or forge a delivery.
80. As an operator, I want the dispatch list to show attempts for live and withdrawn apps and to be filterable by app, so that I can review what was sent after an app is gone.
81. As an operator, I want three back-office entries — WeChat Platforms, WeChat Apps, and WeChat Dispatches — with menu editing, template adoption, and sending on the app, so that the daily work sits on the app it belongs to.
82. As an operator, I want the back office to keep using the existing authenticated Principal and the existing menu permissions, so that WeChat administration is granted the same way as the other modules.
83. As an operator, I want secret fields scrubbed from Oper Logs, so that a platform secret, AppSecret, Push Secret, or token is not written into the audit trail.
84. As an operator, I want WeChat identity calls to leave no Login Log, so that a mini-program exchange is not recorded as someone signing into the back office.
85. As an operator, I want configuration to be refused when the WeChat master key is absent, so that the module cannot fall back to storing secrets in plaintext.
86. As a caller, I want to exchange a mini program's AppID and one-time login code for that app's openid and unionid, so that I learn who opened the mini program without a back-office session.
87. As a caller, I want the unionid to be empty when WeChat omits it, so that I can tell "WeChat did not send one" from a made-up value.
88. As a caller, I want Mini Program Login to work the same for a Direct Hold and a Platform Authorization, so that I do not branch on how the app was connected.
89. As a caller, I want Mini Program Login to return no `session_key`, no system credential, and no Login Session, so that the result cannot be reused as a back-office login.
90. As a caller, I want Mini Program Login for an unknown, withdrawn, suspended, lapsed, or non-mini-program AppID to fail and create no stored person, so that only a usable mini program yields an identity.
91. As an operator, I want a Service Account's Web Authorization to default to openid only, and to be switchable to also asking for nickname and avatar, so that I decide how much profile that app requests.
92. As an operator, I want to set the return URL on the Service Account that will run Web Authorization, so that the browser comes back to a place this app owns.
93. As a caller, I want Web Authorization to be refused when that app has no return URL, so that there is no flow with nowhere to land.
94. As a caller, I want the browser, after WeChat accepts the grant, to arrive at that app's stored return URL with a single-use Handoff Code, so that the code is not delivered to an address supplied in the request.
95. As a caller, I want to redeem a Handoff Code once for the openid and unionid that grant just yielded, plus nickname and avatar only when that app asked for them, so that I collect the identity without holding a WeChat user record.
96. As a caller, I want a second redeem of the same Handoff Code, or a code this system did not issue, to fail, so that the code cannot be replayed or guessed into an identity.
97. As a caller, I want Web Authorization and its redemption to create no WeChat User, System User, or Login Session, so that a web grant stays a one-time identity claim.
98. As a caller, I want Mini Program Login, the start of Web Authorization, and Handoff Code redemption to be usable without a back-office Principal, so that a mini program and a browser can complete them without an operator session.
99. As an operator, I want those identity calls to keep enforcing suspension, lapse, withdrawal, kind, granted capabilities, and platform readiness, so that a public entry cannot bypass the app's state.
100. As an operator, I want this back office to be the only place menus, templates, and dispatches are operated, so that sending stays an operator action and is not implied by a login.

## Implementation Decisions

- The work is the wechat module. It owns WeChat Platforms, WeChat Apps, Custom Menus, Adopted Templates, WeChat Dispatches, Open API calls, and the inbound pushes. Pay keeps WeChat Pay, the official pay SDK, and its out-of-scope service-provider mode. No Open-platform SDK is added; the module speaks HTTP itself.
- Application services call the domain service. They stamp `create_by` and `update_by` from the authenticated Principal. Updates caused by WeChat — a verify ticket, a delivery report, a lapse from a refusal on a push — are stamped as the system itself. Client payloads never carry those audit fields. Withdrawal sets `deleted_at`. Rows are not hard-deleted.
- Layer-facing operations take a context plus one business object and return one value plus an error. A missing row is the module's not-found sentinel. A page of platforms, apps, or dispatches is one page result.
- A WeChat Platform row holds its display name, component AppID, and the ciphertext for its component secret, Token, EncodingAESKey, and current verify ticket. Among rows not withdrawn, one component AppID identifies one platform. The component AppID is immutable. The display name and the operator-supplied secrets can be replaced. A platform is ready only when those secrets are stored and a verify ticket is held. Each platform has its own callback address.
- A WeChat App row holds its display name, AppID, whether it is an official account or a mini program, the Service Account or Subscription Account kind for an official account, and exactly one connection. A Platform Authorization stores the platform it is bound to, the capabilities WeChat granted, and the ciphertext of the authorizer refresh material. It stores no authorizer AppSecret. A Direct Hold stores the ciphertext of its AppSecret and its generated Push Secret, and no platform. Among rows not withdrawn, one AppID identifies one app. AppID, connection, and platform binding are immutable. The display name can be corrected.
- Suspension is a flag on the app. Lapse is a separate flag, set only when a call classifies as a definitive refusal, and cleared only when a Direct Hold receives a replacement AppSecret or a Platform Authorization is granted again on the same platform. An unready platform, a timeout, a business rejection, a bad signature, and a plaintext or compatible-mode push do not lapse the app.
- The saved Custom Menu is one document on the Service Account, including nested sub-buttons. Saving it writes the document and does not call WeChat. Publishing sends that whole document as the replacement of the default menu. The last menu WeChat accepted stays recorded, so a failed publish does not claim a new live menu. The prototype's ceiling of three top-level buttons is not this document's rule. A parent button holds sub-buttons and does not itself open a page; a leaf opens a web page or a mini program.
- An Adopted Template is a row on one app, taken from WeChat's public library through that app's token. The same library template is not adopted twice onto the same live app. This module does not invent templates and does not implement the in-client subscribe step.
- A WeChat Dispatch is one row per send attempt: the app, one openid, the Adopted Template, the filled content, the optional web URL, the optional mini-program page, whether WeChat accepted or rejected, the msgid when accepted, and a later delivery update on that same row. Sending is synchronous. A timeout records no accepted dispatch. A second send is a new row. Withdrawing the app does not withdraw its dispatches, menu, or adopted templates. The dispatch list includes attempts whose app is withdrawn and can be filtered by app. Ordinary platform and app lists omit withdrawn rows.
- The domain service reaches WeChat through one Open API port. The port's result is already classified as success, business rejection, timeout or unavailability, or definitive refusal. Only the last of those lapses an app. Which WeChat codes fall into which class is left unset; the port is the classification boundary. Token refresh happens when a call needs a token. An unready platform fails that refresh for its own apps only.
- Inbound pushes hit the callback of one platform, or of one Direct Hold. The domain service sees a push only after it has been authenticated as an encrypted push. A plaintext or compatible-mode body, or a body that fails verification, is rejected and does not change an app or a dispatch. A verified delivery with a known msgid updates that dispatch, including after the app is withdrawn. Any other verified event is acknowledged and stored nowhere. Full-network release test traffic is answered on the platform callback and creates no app and no dispatch. The test identifiers are WeChat's published ones and are not frozen here.
- Mini Program Login resolves the live app by AppID, requires a usable mini program, and returns openid and unionid. Web Authorization is offered only on a usable Service Account that has a return URL and the capability. The browser returns to the return URL stored on that app; a request cannot substitute another URL. The Handoff Code is single-use and ephemeral, stored the way a Login Challenge is stored, and is not a WeChat User, a Credential, or a Login Session. Redeeming it returns openid and unionid, and nickname and avatar only when that app's scope asked for them. This spec sets no lifetime for the code. `session_key` is dropped at the port and never stored.
- Mini Program Login, the start of Web Authorization, and Handoff Code redemption do not require a back-office Principal. They still pass through the app's capability and state gates. Back-office mutations require a Principal. No cross-module contract is added; other modules are not given a WeChat identity API in this phase, and a dispatch is not attached to a payment or any other business document.
- Ciphertext uses the merchant-credential scheme: AES-256-GCM, a fresh nonce per write, and a key version beside the ciphertext. The master key is a 32-byte value in the environment named `WECHAT_MASTER_KEY`. It is not `PAY_MASTER_KEY` and it is not committed in configuration. An absent key refuses the operations that would write or use secrets, and does not degrade to plaintext. Access tokens exist only as ciphertext or in memory for the duration of a call. Ordinary reads return the same style of mask merchant credentials return. The generated Push Secret is returned in full only in the response that generated or regenerated it.
- Oper Logs record back-office mutations through the existing middleware, with secret fields scrubbed. Identity calls and WeChat callbacks do not write Login Logs.
- Back-office permissions follow the existing list / create / update / remove / probe split. Platforms use `wechat:platform:list`, `create`, `update`, `remove`, and `probe`. Apps use `wechat:app:list`, `create`, `update`, `remove`, and `probe`. Dispatches use `wechat:dispatch:list` and `wechat:dispatch:create` for the send. The seed adds a directory and the three entries, and grants them to the superadmin the same way the pay menus are granted. The Chinese labels are 微信管理, 微信平台, 微信应用, and 微信下发.
- Authorization asks WeChat for the capability set this phase uses, for the kind of app being granted. The numeric permission-set ids are left unset. The granted set is stored on the app and is what the local capability check consults.
- API contracts are produced by the repository's generator. Generated sources are not hand-edited. Errors follow the module's sentinels: unexpected infrastructure failures are logged and mapped once at the adapter, and a business rejection propagates as a warning breadcrumb.

## Testing Decisions

A good test asserts an outcome a caller or an operator can observe: which platform or app exists, whether a call was refused, whether an app lapsed, what identity came back, whether a dispatch was created or updated, and whether a push was accepted. It does not assert SQL, HTTP paths, ciphertext bytes, or a table of WeChat error codes.

One seam: the wechat domain service. Tests stand up that service with in-memory repositories and a scripted Open API port. The port is scripted to return success, a business rejection, a timeout, or a definitive refusal. Push authentication is a real collaborator of that service, so a plaintext body, a compatible-mode body, and a bad signature are rejected with no state change, and a valid encrypted delivery updates the matching dispatch. Masking is asserted by reading the app or platform back after a write and after a regenerate.

This is the same seam the pay domain service already uses: in-memory repositories and a scripted gateway, with the service's observable result as the assertion. Application services, transport handlers, generated contracts, and the HTML prototype are not a second behavioral suite.

## Out of Scope

- A tenant per customer, or any change to the single-tenant decision.
- WeChat Pay, the pay SDK, pay service-provider mode, and any change to how a Merchant Account takes payment.
- An Open-platform SDK, and splitting official accounts and mini programs into two modules.
- Storing WeChat Users, creating System Users or Login Sessions from WeChat identity, returning or storing `session_key`, and writing Login Logs for these calls.
- A mini program or a web frontend in this repository. The module supplies the server exchange and the browser redirect only.
- Personalized menus, menu buttons that click, scan, photograph, or send a location, customer-service messages, and automatic replies.
- Mass send, a send queue, and binding a dispatch to another module's document.
- Building the screen on which a person subscribes inside WeChat. A missing subscription arrives as WeChat's rejection of that dispatch.
- Inventing templates locally, and persisting follow, text, or other non-delivery pushes.
- Changing a connection or a platform binding in place. Moving is a withdrawal plus a new row.
- A productized "guided scenario" like the prototype's walkthrough. The prototype is throwaway.
- Choosing a Handoff Code lifetime, the numeric permission-set ids, or the WeChat error codes that count as a definitive refusal. Those were left unset on purpose.
- Automated master-key rotation. The ciphertext carries a key version so rotation stays possible later; performing a rotation is not this work.
- Hard-deleting rows, truncating history, or exporting it.

## Further Notes

The canonical vocabulary is the wechat section of the glossary. The hard-to-reverse decisions are ADR 0029: one connection per app, the wechat module separate from pay, identity returned without becoming a System User, and a dedicated master key. This spec does not add an ADR.

The closed design includes several platforms, re-authorization restoring the same app on the same platform, a platform being non-withdrawable while a live app still connects through it, and component AppID uniqueness among platforms that are not withdrawn. Encrypted pushes are the only accepted mode; the one-word confirmation of that point was taken as agreement.

The HTML prototype is a throwaway reducer used to walk the back office. Its in-memory secrets, its three-button menu ceiling, and its guided scenario are not requirements. Where the prototype asks the operator to type an AppID during a grant, that is a stand-in for WeChat's authorization callback; the module takes those fields from WeChat.
