package store

import "context"

// TxManager is the concrete infrastructure implementation of an Application
// transaction boundary.
type TxManager struct {
	data *Data
}

// NewTxManager constructs the shared transaction manager. The composition
// root binds it to each Application-owned Transaction port that needs it.
func NewTxManager(data *Data) *TxManager {
	return &TxManager{data: data}
}

// InTx propagates the database transaction through ctx.
func (m *TxManager) InTx(ctx context.Context, fn func(context.Context) error) error {
	return m.data.DB.InTx(ctx, fn)
}
