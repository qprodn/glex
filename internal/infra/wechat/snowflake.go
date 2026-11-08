package wechatinfra

import "glex/internal/infra/snowflake"

// newSnowflakeID returns a new snowflake ID for this module's records.
func newSnowflakeID() int64 {
	return snowflake.NewID()
}
