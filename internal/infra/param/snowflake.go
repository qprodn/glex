package paraminfra

import "glex/internal/infra/snowflake"

// newSnowflakeID returns a new snowflake ID for this module's records. The
// node itself lives in the shared snowflake package, installed once from
// config — replicas must hold distinct node numbers there.
func newSnowflakeID() int64 {
	return snowflake.NewID()
}
