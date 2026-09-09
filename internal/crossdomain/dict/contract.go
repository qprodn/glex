// Package dict publishes the dict module's cross-module contracts in the
// anti-corruption shape of ADR 0005: contract.go holds the consumer-shaped
// interface, model/ holds flat record types, impl/ adapts the provider's
// domain capability, and dictmock holds the mockgen-generated test double.
// Contracts are consumed by constructor injection into the calling
// application service; cmd stays the only composition root.
package dict

import (
	"context"

	"glex/internal/crossdomain/dict/model"
)

//go:generate go tool mockgen -package dictmock -destination dictmock/vocabulary_reader.go glex/internal/crossdomain/dict VocabularyReader

// VocabularyReader snapshots dict vocabularies for consumers that
// translate stored values into display labels and back — Data Import and
// Data Export's spreadsheet columns (ADR 0017). The name follows the
// contract family's capability nouns (URLResolver, ContentReader,
// DeptReader).
type VocabularyReader interface {
	// ValueLabels returns one Dict Type's active entries in (dict_sort,
	// id) order — the consumption read. An unknown key yields an empty
	// list, not an error (the provider's semantics); the consumer decides
	// whether a missing vocabulary is an empty template or sour rows.
	ValueLabels(ctx context.Context, dictType string) ([]model.Entry, error)
}
