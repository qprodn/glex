package impl

import "go.uber.org/fx"

// Module contains the dict crossdomain contract adapters.
var Module = fx.Module("dict-crossdomain",
	fx.Provide(
		NewVocabularyReader,
	),
)
