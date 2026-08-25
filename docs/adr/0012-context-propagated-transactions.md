# Propagate PostgreSQL transactions through context

`infra/ent` owns a generated `Database` wrapper that stores an Ent transaction
in context and makes each entity accessor resolve that transaction-aware
client. Application Usecases open the transaction through their local
`Transaction.InTx` port; Domain ports and Infra repository methods receive the
propagated context and never take an explicit transaction parameter. An
already-propagated transaction joins the outer transaction. Application
Usecases are transaction roots, so nested Usecase calls are unsupported and a
callback error must propagate to the root for rollback.
