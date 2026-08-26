# Authn Infrastructure

This package implements the ports declared by `internal/domain/authn` —
Credentials (the only reader/writer of the credential column), Login Sessions
and their Redis state, the login backend, and the sa-token Manager/storage
machinery. Persistent objects, driver errors, and key layout remain inside
this adapter package. Writes join the context-propagated transaction opened by
an Application Usecase.
