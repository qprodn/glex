package eventbus

// SendOption carries the per-send options; its fields are read by backend
// implementations.
type SendOption struct {
	// ShardingKey is the partition key broker backends may use to keep
	// same-key events ordered within a topic; the in-memory backend
	// ignores it (its groups are FIFO by construction).
	ShardingKey *string
}

// SendOpt customises a single Send call.
type SendOpt func(*SendOption)

// WithShardingKey sets the send's sharding key.
func WithShardingKey(key string) SendOpt {
	return func(o *SendOption) { k := key; o.ShardingKey = &k }
}
