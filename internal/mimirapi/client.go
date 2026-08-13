package mimirapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Error struct {
	StatusCode int
	Status     string
	Body       string
}

func (e *Error) Error() string {
	return fmt.Sprintf("Mimir API %s: %s", e.Status, e.Body)
}

type Client struct {
	HTTPClient *http.Client
	Pointer    Pointer
}

type WhoAmI struct {
	Service      string   `json:"service"`
	APIVersion   int      `json:"api_version"`
	Capabilities []string `json:"capabilities"`
}

func (w WhoAmI) HasCapability(capability string) bool {
	for _, available := range w.Capabilities {
		if available == capability {
			return true
		}
	}
	return false
}

type MachineAssociation struct {
	Version        int    `json:"version"`
	InstallationID string `json:"installation_id"`
	Name           string `json:"name"`
	Platform       string `json:"platform"`
	Arch           string `json:"arch"`
}

func (c Client) WhoAmI(ctx context.Context) (WhoAmI, error) {
	data, err := c.Request(ctx, http.MethodGet, "/whoami", nil)
	if err != nil {
		return WhoAmI{}, err
	}
	var identity WhoAmI
	if err := json.Unmarshal(data, &identity); err != nil {
		return WhoAmI{}, fmt.Errorf("decoding Mimir identity: %w", err)
	}
	return identity, nil
}

func (c Client) AssociateMachine(ctx context.Context, association MachineAssociation) error {
	_, err := c.Request(ctx, http.MethodPost, "/machine/associate", association)
	return err
}

func (c Client) Verify(ctx context.Context) error {
	var last error
	for attempt := 0; attempt < 8; attempt++ {
		if _, err := c.WhoAmI(ctx); err == nil {
			return nil
		} else {
			last = err
		}
		timer := time.NewTimer(time.Duration(attempt+1) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return last
}

func New(pointer Pointer) Client {
	return Client{HTTPClient: &http.Client{Timeout: 30 * time.Second}, Pointer: pointer}
}

func (c Client) Request(ctx context.Context, method, path string, body any) ([]byte, error) {
	if err := ValidateDeploymentURL(c.Pointer.URL); err != nil {
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
	req, err := http.NewRequestWithContext(ctx, method, c.Pointer.URL+path, input)
	if err != nil {
		return nil, err
	}
	req.Header.Set("authorization", "Bearer "+c.Pointer.Token)
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, &Error{StatusCode: res.StatusCode, Status: res.Status, Body: strings.TrimSpace(string(data))}
	}
	return data, nil
}

func ValidateDeploymentURL(raw string) error {
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
