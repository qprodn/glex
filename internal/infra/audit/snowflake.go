package auditinfra

import "glex/internal/infra/snowflake"

// newSnowflakeID returns a new snowflake ID for this module's records. The
// node itself lives in the shared snowflake package, installed once from
// config — replicas must hold distinct node numbers there (the old
// per-package hardcoded node 1 was a cross-replica collision path).
func newSnowflakeID() int64 {
	return snowflake.NewID()
}
