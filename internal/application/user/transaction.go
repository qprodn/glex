package user

import "context"

// Transaction owns the Application Usecase transaction boundary. The
// callback receives a context carrying the infrastructure transaction and
// must pass it to every repository call in the critical section.
//
// Application Usecases are transaction roots and must not call another
// Usecase from inside this callback.
type Transaction interface {
	InTx(ctx context.Context, fn func(context.Context) error) error
}
