// Package snowflake owns the process's single snowflake node — the one
// place a replica's node number lives. Every module's ID minting
// (per-package newSnowflakeID helpers) delegates here; before this package
// existed each package hardcoded node 1, which meant two replicas minting
// in the same millisecond produced identical IDs — a real collision path
// now that the job scheduler's symmetric replicas all write job_log rows.
// Init is called once from store.NewData with the configured node; 0 falls
// back to 1 (the historical single-instance value) so an unset config
// changes nothing for existing deployments.
package snowflake

import (
	"fmt"
	"sync"

	sf "github.com/bwmarrin/snowflake"
)

var (
	mu   sync.Mutex
	node *sf.Node
)

// Init installs the process's node (0–1023; outside that range the snowflake
// library rejects it and Init returns the error — a deployment misconfiguration
// that must fail fast rather than mint collisions).
func Init(nodeID int64) error {
	n, err := sf.NewNode(nodeID)
	if err != nil {
		return fmt.Errorf("snowflake node %d: %w", nodeID, err)
	}
	mu.Lock()
	node = n
	mu.Unlock()
	return nil
}

// NewID mints one ID. If Init never ran (unit tests constructing repos by
// hand), it lazily installs the historical default node 1 rather than panic —
// tests never care about cross-process uniqueness.
func NewID() int64 {
	mu.Lock()
	n := node
	mu.Unlock()
	if n == nil {
		if err := Init(1); err != nil {
			// Unreachable: 1 is always a legal node number.
			panic(err)
		}
		n = node
	}
	return n.Generate().Int64()
}
