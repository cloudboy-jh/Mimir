package mimircli

import (
	"context"
	"net/http"
	"time"

	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

// httpClient bounds every Worker request so CLI and MCP calls cannot hang
// indefinitely on a stalled connection.
var httpClient = &http.Client{Timeout: 30 * time.Second}

func remoteRequest(ctx context.Context, method, path string, body any) ([]byte, error) {
	p, err := loadPointer()
	if err != nil {
		return nil, err
	}
	return remoteRequestWithPointer(ctx, p, method, path, body)
}

func remoteRequestWithPointer(ctx context.Context, p mimirapi.Pointer, method, path string, body any) ([]byte, error) {
	return (mimirapi.Client{HTTPClient: httpClient, Pointer: p}).Request(ctx, method, path, body)
}
