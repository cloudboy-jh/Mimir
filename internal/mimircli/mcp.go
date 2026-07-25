package mimircli

import (
	"context"
	"io"

	"github.com/cloudboy-jh/mimir/internal/mcp"
	searchpkg "github.com/cloudboy-jh/mimir/internal/search"
)

type mcpOptions struct {
	In  io.Reader
	Out io.Writer
}

func currentSearchService() searchpkg.Service { return searchpkg.New(apiRequester{}) }

func currentMCPServer() mcp.Server {
	search := currentSearchService()
	return mcp.New(versionString(), apiRequester{}, currentSessionService(), search.Search)
}

func serveMCP(ctx context.Context, opts mcpOptions) error {
	return currentMCPServer().Serve(ctx, opts.In, opts.Out)
}
