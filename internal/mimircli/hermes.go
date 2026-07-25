package mimircli

import (
	"context"

	hermesintegration "github.com/cloudboy-jh/mimir/internal/harness/hermes"
)

var runHermesPluginCommand = func(ctx context.Context, home string, args ...string) error {
	service := hermesintegration.New()
	return service.RunPluginCommand(ctx, home, args...)
}

var listHermesPlugins = func(ctx context.Context, home string) (string, error) {
	service := hermesintegration.New()
	return service.ListPlugins(ctx, home)
}
