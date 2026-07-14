package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func remoteRequest(ctx context.Context, method, path string, body any) ([]byte, error) {
	p, err := loadPointer()
	if err != nil {
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
