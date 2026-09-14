package eventbus

// ConsumerOption carries the per-consumer options; its fields are read by
// backend implementations.
type ConsumerOption struct {
	// Orderly requests ordered delivery where the backend supports it;
	// the in-memory backend is always ordered within a group, so it
	// ignores the flag.
	Orderly *bool
}

// ConsumerOpt customises one consumer registration.
type ConsumerOpt func(*ConsumerOption)

// WithOrderly requests (or drops) ordered delivery.
func WithOrderly(orderly bool) ConsumerOpt {
	return func(o *ConsumerOption) { v := orderly; o.Orderly = &v }
}
