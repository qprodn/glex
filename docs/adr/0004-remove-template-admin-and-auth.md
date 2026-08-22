# Remove the template admin resource and pkg/auth

The repo shipped a template-generated `admin` resource (JWT-in-cookie auth,
bcrypt, ent/MySQL CRUD) plus its `pkg/auth` middleware. We removed **both
entirely**, leaving only the System User module.

Supersedes [ADR-0002](./0002-system-user-fresh-module.md), which had kept
`admin` in place as legacy. Two things changed the decision:

- `pkg/auth`'s global middleware required a token on every non-login path,
  including the new `/iam/user/*` endpoints — a chicken-and-egg blocker for
  testing the user module before any auth module exists.
- `admin` was the template's *example* auth+user module, not something we
  intended to keep. "Rewrite later" is cheaper from a clean slate than from a
  half-stripped zombie (login removed, CRUD left behind).

Consequences: all HTTP/gRPC endpoints are currently **unauthenticated** —
acceptable because nothing is deployed yet. The real auth module will add a
middleware back. `golang-jwt` and the `mysql` driver were dropped from go.mod;
`bcrypt` stays (used by the user usecase).
