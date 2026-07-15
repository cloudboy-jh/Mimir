package mimircli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func remoteRequest(ctx context.Context, method, path string, body any) ([]byte, error) {
	p, err := loadPointer()
	if err != nil {
		return nil, err
	}
	return remoteRequestWithPointer(ctx, p, method, path, body)
}

func remoteRequestWithPointer(ctx context.Context, p Pointer, method, path string, body any) ([]byte, error) {
	if err := validateDeploymentURL(p.URL); err != nil {
		return nil, err
	}
	var input io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		input = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.URL+path, input)
	if err != nil {
		return nil, err
	}
	req.Header.Set("authorization", "Bearer "+p.Token)
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, fmt.Errorf("Mimir API %s: %s", res.Status, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func validateDeploymentURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("invalid Mimir deployment URL")
	}
	host := parsed.Hostname()
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "::1")) {
		return fmt.Errorf("Mimir deployment URL must use HTTPS")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("invalid Mimir deployment URL")
	}
	return nil
}

func federatedSearch(ctx context.Context, query string) ([]byte, error) {
	remote, err := remoteRequest(ctx, "POST", "/search", map[string]any{"query": query})
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	if err := json.Unmarshal(remote, &result); err != nil {
		return nil, err
	}
	if code, err := queryRecall(ctx, ".", query, 4000); err == nil {
		result["code"] = code
	}
	return json.MarshalIndent(result, "", "  ")
}
