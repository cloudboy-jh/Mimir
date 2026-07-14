package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type connectionManifest struct {
	OpenAIBaseURL     string   `json:"openai_base_url"`
	AnthropicBaseURL  string   `json:"anthropic_base_url"`
	CredentialFile    string   `json:"credential_file"`
	CredentialCommand []string `json:"credential_command"`
	MCPCommand        []string `json:"mcp_command"`
	OptionalHeaders   []string `json:"optional_headers"`
}

func currentConnectionManifest(url string) (connectionManifest, error) {
	credential, err := tokenPath()
	if err != nil {
		return connectionManifest{}, err
	}
	base := strings.TrimRight(url, "/")
	return connectionManifest{
		OpenAIBaseURL:     base + "/v1",
		AnthropicBaseURL:  base,
		CredentialFile:    credential,
		CredentialCommand: []string{"cat", credential},
		MCPCommand:        []string{"mimir", "serve"},
		OptionalHeaders:   []string{"x-mimir-session", "x-mimir-repo", "x-mimir-harness"},
	}, nil
}

func writeConnectionManifest(out io.Writer) error {
	pointer, err := loadPointer()
	if err != nil {
		return err
	}
	manifest, err := currentConnectionManifest(pointer.URL)
	if err != nil {
		return err
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(data))
	return err
}

func addConnectionManifest(result map[string]any, url string) map[string]any {
	if manifest, err := currentConnectionManifest(url); err == nil {
		result["connection"] = manifest
	}
	return result
}
