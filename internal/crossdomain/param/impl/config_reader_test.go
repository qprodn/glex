package impl

import (
	"context"
	"errors"
	"testing"

	"glex/internal/domain/param"
	paramentity "glex/internal/domain/param/entity"
	paramservice "glex/internal/domain/param/service"

	"glex/pkg/listq"
)

// fakeConfigs adapts a canned result to the param domain service shape the
// reader delegates to.
type fakeConfigs struct {
	c   *paramentity.Config
	err error
}

func (f fakeConfigs) Get(context.Context, int64) (*paramentity.Config, error) {
	return f.c, f.err
}
func (f fakeConfigs) FindByKey(context.Context, string) (*paramentity.Config, error) {
	return f.c, f.err
}
func (f fakeConfigs) List(context.Context, listq.Query) ([]*paramentity.Config, error) {
	return nil, f.err
}
func (f fakeConfigs) Create(context.Context, *paramentity.Config) (*paramentity.Config, error) {
	return f.c, f.err
}
func (f fakeConfigs) Update(context.Context, *paramentity.Config) (*paramentity.Config, error) {
	return f.c, f.err
}
func (f fakeConfigs) Delete(context.Context, *paramservice.DeleteRequest) error {
	return f.err
}

// compile-time check that the fake satisfies the service seam the reader binds.
var _ paramservice.Configs = fakeConfigs{}

func TestConfigReaderReturnsTheValue(t *testing.T) {
	reader := NewConfigReader(fakeConfigs{c: &paramentity.Config{ConfigKey: "sys.account.registerUser", ConfigValue: "false"}})

	v, err := reader.ValueByKey(context.Background(), "sys.account.registerUser")
	if err != nil {
		t.Fatalf("ValueByKey: %v", err)
	}
	if v != "false" {
		t.Fatalf("value = %q, want %q", v, "false")
	}
}

func TestConfigReaderCrossesTheDomainSentinel(t *testing.T) {
	reader := NewConfigReader(fakeConfigs{err: param.ErrConfigNotFound})

	if _, err := reader.ValueByKey(context.Background(), "sys.absent"); !errors.Is(err, param.ErrConfigNotFound) {
		t.Fatalf("got %v, want ErrConfigNotFound", err)
	}
}
