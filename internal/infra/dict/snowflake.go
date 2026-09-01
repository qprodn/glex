package dictinfra

import sf "github.com/bwmarrin/snowflake"

// snowflakeNode generates sortable IDs for dict's rows. Node 1 is hardcoded
// for now (single dev instance); make this configurable before running
// multiple instances. Each package mints IDs for its own tables only, so
// per-package nodes cannot collide within a key space. NewNode fails only
// for a node number outside 0–1023, which the constant 1 cannot be, so the
// discarded error is unreachable.
var snowflakeNode, _ = sf.NewNode(1)

// newSnowflakeID returns a new snowflake ID for a dict record.
func newSnowflakeID() int64 {
	return snowflakeNode.Generate().Int64()
}
