package sessionimport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var safeToolName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func canonicalID(harness, value string) string {
	if safeID.MatchString(value) {
		return value
	}
	sum := sha256.Sum256([]byte(value))
	return harness + "-" + hex.EncodeToString(sum[:16])
}

func deterministicID(prefix string, values ...string) string {
	hash := sha256.New()
	for index, value := range values {
		if index != 0 {
			_, _ = hash.Write([]byte{0})
		}
		_, _ = hash.Write([]byte(value))
	}
	return prefix + hex.EncodeToString(hash.Sum(nil)[:20])
}

func truncateUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func parseTime(value any) time.Time {
	switch typed := value.(type) {
	case string:
		parsed, _ := time.Parse(time.RFC3339Nano, typed)
		return parsed
	case float64:
		if typed <= 0 {
			return time.Time{}
		}
		return time.UnixMilli(int64(typed)).UTC()
	case json.Number:
		milliseconds, _ := typed.Int64()
		if milliseconds > 0 {
			return time.UnixMilli(milliseconds).UTC()
		}
	}
	return time.Time{}
}

func object(value any) map[string]any {
	result, _ := value.(map[string]any)
	return result
}

func array(value any) []any {
	result, _ := value.([]any)
	return result
}

func text(value any) string {
	result, _ := value.(string)
	return strings.TrimSpace(result)
}

func number(value any) int64 {
	switch typed := value.(type) {
	case float64:
		if typed > 0 {
			return int64(typed)
		}
	case json.Number:
		result, _ := typed.Int64()
		if result > 0 {
			return result
		}
	}
	return 0
}

func boundedJSON(value any, depth int, budget *int) any {
	if *budget <= 0 || depth >= 8 {
		return nil
	}
	*budget--
	switch typed := value.(type) {
	case nil, bool, json.Number:
		return typed
	case float64:
		return typed
	case string:
		return truncateUTF8(typed, 64<<10)
	case []any:
		result := make([]any, 0, min(len(typed), 512))
		for _, item := range typed {
			if len(result) == 512 || *budget <= 0 {
				break
			}
			result = append(result, boundedJSON(item, depth+1, budget))
		}
		return result
	case map[string]any:
		result := make(map[string]any)
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for index, key := range keys {
			if index == 512 || *budget <= 0 {
				break
			}
			result[truncateUTF8(key, 64<<10)] = boundedJSON(typed[key], depth+1, budget)
		}
		return result
	default:
		return nil
	}
}

func boundedValue(value any) any {
	budget := 512
	return boundedJSON(value, 0, &budget)
}

func fitExchange(exchange Exchange, maxBytes int) (Exchange, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxExchangeBytes
	}
	data, err := json.Marshal(exchange)
	if err != nil {
		return Exchange{}, err
	}
	if len(data) <= maxBytes {
		return exchange, nil
	}
	request := object(exchange.Request)
	messages := array(request["messages"])
	for len(data) > maxBytes && len(messages) > 1 {
		messages = messages[1:]
		request["messages"] = messages
		exchange.Request = request
		data, err = json.Marshal(exchange)
		if err != nil {
			return Exchange{}, err
		}
	}
	if len(data) > maxBytes {
		return Exchange{}, fmt.Errorf("exchange %s exceeds %d bytes", exchange.ExchangeID, maxBytes)
	}
	return exchange, nil
}

func repoName(directory string) string {
	directory = strings.TrimRight(directory, `/\\`)
	if directory == "" {
		return ""
	}
	parts := strings.FieldsFunc(directory, func(r rune) bool { return r == '/' || r == '\\' })
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func uniqueSorted(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
