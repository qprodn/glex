package iaminfra

import sf "github.com/bwmarrin/snowflake"

// snowflakeNode generates globally-unique, sortable IDs. Node 1 is hardcoded
// for now (single dev instance); make this configurable before running multiple
// instances.
var snowflakeNode, _ = sf.NewNode(1)

// newSnowflakeID returns a new snowflake ID for a user.
func newSnowflakeID() int64 {
	return snowflakeNode.Generate().Int64()
}
