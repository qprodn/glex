# WeChat apps are their own module, one connection each

glex connects many WeChat official accounts and mini programs for a single
operator (ADR 0001). A WeChat login is easy to mistake for a System User
login, and WeChat Pay is already a Channel in the pay module. Decided
2026-11-05 while planning the wechat module:

- Each WeChat App is connected in exactly one way: a Platform Authorization
  through exactly one of the system's WeChat Platforms, or a Direct Hold of
  that app's own secret. Callers do not choose a mode; the same calls run
  once the app has a token. The connection is not changed in place —
  switching withdraws the app and connects a new one. Among apps not
  withdrawn, one AppID belongs to one app. Among platforms not withdrawn,
  one component AppID belongs to one platform. The system may register
  several WeChat Platforms; each has its own credentials, verify ticket,
  and callback. A platform cannot be withdrawn while an app still connects
  through it. Re-authorization restores the same app on the same platform.
- The wechat module owns the platforms, the apps, and their Open API calls,
  which it makes over HTTP with no Open-platform SDK. Pay keeps WeChat Pay,
  including its official SDK. WeChat Pay service-provider mode stays out of
  scope.
- Mini Program Login and Web Authorization return the app's openid and,
  when WeChat supplies one, the unionid. They do not create a System User,
  a Login Session, or a stored WeChat User.
- The platform registrations, a Direct Hold secret, and messaging keys are
  stored only as ciphertext, under a dedicated environment master key,
  using the same scheme as merchant credentials (ADR 0018). Ordinary reads
  return masks. The key is not `PAY_MASTER_KEY`. A Direct Hold's generated
  messaging Token and EncodingAESKey are returned in full only in the
  response that generated or regenerated them, so the operator can paste
  them into WeChat. Regenerating invalidates the previous pair and does not
  lapse the app.

Considered and rejected: platform authorization alone, which cannot keep a
token for an app this system itself holds; Direct Hold alone, which means
collecting every customer's AppSecret and cannot fan authorized accounts
through one platform callback; treating the WeChat identity as a System
User; placing the apps in pay, or encrypting them with `PAY_MASTER_KEY`,
which ties two unrelated secret domains together.

Consequences: a platform that is not ready blocks new authorizations onto
that platform and token refresh for the apps authorized through it. Other
platforms, and every Direct Hold, keep working.
Only a definitive refusal from WeChat lapses an app. Re-authorization or a
replaced secret restores the same app on the same platform; moving it to
another platform withdraws it and connects a new one. A withdrawn app stays
withdrawn, and its dispatches stay with it. Storing WeChat Users later does
not make them System Users.
