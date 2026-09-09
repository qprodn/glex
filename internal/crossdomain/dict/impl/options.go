package impl

import (
	"context"

	"glex/internal/crossdomain/dict"
	"glex/internal/crossdomain/dict/model"
	dictservice "glex/internal/domain/dict/service"
)

// vocabularyReader satisfies the dict vocabulary contract by delegating to
// the provider's dict domain service: the active-entry consumption read
// lives there, beside the module's ordering rules.
type vocabularyReader struct {
	dicts dictservice.Dict
}

// NewVocabularyReader adapts the dict domain service to the crossdomain
// vocabulary contract.
func NewVocabularyReader(dicts dictservice.Dict) dict.VocabularyReader {
	return &vocabularyReader{dicts: dicts}
}

// ValueLabels maps the provider's active entries to contract records,
// keeping the (dict_sort, id) order. Errors cross with the dict domain's
// kratos reasons; the domain service logged its breadcrumb.
func (o *vocabularyReader) ValueLabels(ctx context.Context, dictType string) ([]model.Entry, error) {
	entries, err := o.dicts.OptionsByType(ctx, dictType)
	if err != nil {
		return nil, err
	}
	out := make([]model.Entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, model.Entry{Value: e.DictValue, Label: e.DictLabel})
	}
	return out, nil
}
