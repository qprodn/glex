# Declare every RPC auth policy explicitly in proto metadata

Every glex RPC will carry a method option classifying it as public, optionally
authenticated, authenticated-only, or protected by one Functional Permission;
an unspecified policy is a startup error. Optional authentication exists only
for RuoYi-compatible idempotent logout: a valid Session is revoked, a missing or
confirmed-invalid credential succeeds, and a session-store failure is reported.
The middleware evaluates the proto operation rather than an HTTP path, so HTTP
routing can change without changing security policy. This adds a small amount
of contract metadata and validation machinery in exchange for preventing new
endpoints from being accidentally public or merely login-protected. The first
version deliberately omits compound permission and role expressions; the
`superadmin` Role supplies the permission wildcard.
