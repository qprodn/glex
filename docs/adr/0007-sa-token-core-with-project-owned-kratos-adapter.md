# Use sa-token-go core with a project-owned Kratos adapter

glex will use `sa-token-go` core for token and login-session mechanics, but will
not import its `integrations/kratos` package. The project will own strict token
extraction, Kratos operation policies, request-context identity, authorization,
and error mapping because those boundaries must follow glex's default-protected
RBAC model and distinguish authentication failures, authorization denials, and
backend failures. This deliberately gives up the integration package's
convenience in exchange for explicit policy composition, typed context values,
and transport-independent application interfaces. The dependency is pinned to
`sa-token-go` v0.2.4 and isolated behind data-layer repository contracts. HTTP accepts only a strict
`Authorization: Bearer` credential; the credential is an opaque random token
generated with `core.TokenStyleSimple`, while externally visible session
management uses a separate non-secret Session ID. A project-owned Redis storage
adapter supplies namespacing, failure classification, and observability.
