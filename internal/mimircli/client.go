package mimircli

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

// httpClient bounds every Worker request so CLI calls cannot hang
// indefinitely on a stalled connection.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// errQueryAPIMissing marks a Worker deployed before the query API (exit 6).
var errQueryAPIMissing = errors.New("deployed Mimir Worker lacks required capability query_api; run mimir deploy")

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

// apiRequest calls a query API route. A 404 from a Worker whose /whoami does
// not advertise query_api is reported as an incompatible Worker.
func apiRequest(ctx context.Context, method, path string, body any) ([]byte, error) {
	pointer, err := loadPointer()
	if err != nil {
		return nil, err
	}
	client := mimirapi.Client{HTTPClient: httpClient, Pointer: pointer}
	data, err := client.Request(ctx, method, path, body)
	var apiErr *mimirapi.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
		if whoami, whoErr := client.WhoAmI(ctx); whoErr == nil && !whoami.HasCapability("query_api") {
			return nil, errQueryAPIMissing
		}
	}
	return data, err
}

type apiRequester struct{}

func (apiRequester) Request(ctx context.Context, method, path string, body any) ([]byte, error) {
	return remoteRequest(ctx, method, path, body)
}
