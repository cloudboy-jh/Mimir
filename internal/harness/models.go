package harness

type ConnectionManifest struct {
	OpenAIBaseURL     string   `json:"openai_base_url"`
	AnthropicBaseURL  string   `json:"anthropic_base_url"`
	CredentialFile    string   `json:"credential_file"`
	CredentialCommand []string `json:"credential_command"`
	OptionalHeaders   []string `json:"optional_headers"`
}

type IntegrationState struct {
	State           string `json:"state"`
	Provider        string `json:"provider,omitempty"`
	Scope           string `json:"scope,omitempty"`
	RestartRequired bool   `json:"restart_required,omitempty"`
	Detail          string `json:"detail,omitempty"`
}

type IntegrationReport struct {
	OpenCode IntegrationState `json:"opencode"`
	Hermes   IntegrationState `json:"hermes"`
}

type Diagnostic struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Repair string `json:"repair,omitempty"`
}
