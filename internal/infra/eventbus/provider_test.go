package eventbusinfra

import (
	"log/slog"
	"testing"

	payinfra "glex/internal/infra/pay"

	"go.uber.org/fx"
)

// The subscription wiring's boot gate: the registration invoke and the
// subscription providers must agree on the value-group tag — a plain
// []eventbus.Subscription parameter resolves nothing and the app dies at
// fx.New ("missing type"). Validating the real modules together proves
// the two ends meet; fx constructs only what the invoke demands, so the
// repo adapters in payinfra stay dormant and no store is needed.
func TestSubscriptionsWiringValidates(t *testing.T) {
	if err := fx.ValidateApp(
		fx.Supply(slog.New(slog.DiscardHandler)),
		Module,
		payinfra.Module,
	); err != nil {
		t.Fatalf("eventbus subscription wiring does not validate: %v", err)
	}
}
