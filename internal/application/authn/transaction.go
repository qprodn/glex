package authn

import "context"

// Transaction owns the Application Usecase transaction boundary (ADR 0005).
// The callback receives a context carrying the infrastructure transaction
// and must pass it to every domain-service call in the critical section;
// service methods and the repositories under them join the ambient
// transaction through that context.
//
// Application Usecases are transaction roots and must not call another
// Usecase from inside this callback.
type Transaction interface {
	InTx(ctx context.Context, fn func(context.Context) error) error
}
