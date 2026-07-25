package jsonconfig

import (
	"encoding/json"
	"os"
	"strings"
)

func Read(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var config map[string]any
	if err := json.Unmarshal(StripJSONC(data), &config); err != nil {
		return nil, err
	}
	return config, nil
}

func Write(path string, config map[string]any) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func UpdateVars(path string, vars map[string]string) error {
	config, err := Read(path)
	if err != nil {
		return err
	}
	existing, _ := config["vars"].(map[string]any)
	if existing == nil {
		existing = map[string]any{}
	}
	for key, value := range vars {
		existing[key] = value
	}
	config["vars"] = existing
	return Write(path, config)
}

func StripJSONC(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString, escaped := false, false
	for i := 0; i < len(data); i++ {
		c := data[i]
		switch {
		case inString:
			out = append(out, c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
		case c == '"':
			inString = true
			out = append(out, c)
		case c == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
			if i < len(data) {
				out = append(out, '\n')
			}
		case c == '/' && i+1 < len(data) && data[i+1] == '*':
			i += 2
			for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
				i++
			}
			i++
		case c == ',':
			j := i + 1
			for j < len(data) && strings.ContainsRune(" \t\r\n", rune(data[j])) {
				j++
			}
			if j >= len(data) || (data[j] != '}' && data[j] != ']') {
				out = append(out, c)
			}
		default:
			out = append(out, c)
		}
	}
	return out
}
