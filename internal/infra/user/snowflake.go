package userinfra

import sf "github.com/bwmarrin/snowflake"

// snowflakeNode generates globally-unique, sortable IDs. Node 1 is hardcoded
// for now (single dev instance); make this configurable before running multiple
// instances. NewNode fails only for a node number outside 0–1023, which the
// constant 1 cannot be, so the discarded error is unreachable.
var snowflakeNode, _ = sf.NewNode(1)

// newSnowflakeID returns a new snowflake ID for a user.
func newSnowflakeID() int64 {
	return snowflakeNode.Generate().Int64()
}
