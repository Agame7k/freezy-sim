// Package redact blanks secrets (Wi-Fi keys, passwords, tokens) in JSON before it is published on the bus, which sends
// it to every GUI client and into recordings, the black box and bug-report bundles.
package redact

import (
	"bytes"
	"encoding/json"
	"strings"
)

// secretWords are what a field name contains when its value is a secret (matched case-insensitively, so "WpaKey",
// "wpaKey" and "wpa_key" all count).
var secretWords = []string{"wpakey", "wpa_key", "password", "passwd", "secret", "token", "apikey", "api_key", "authkey"}

// IsSecretKey reports whether a JSON field name holds a secret.
func IsSecretKey(name string) bool {
	lower := strings.ToLower(name)
	for _, word := range secretWords {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// JSON returns data with the value of every secret field (at any depth) blanked to "", keeping its shape. Data with
// no secret-looking field name in it, the common case, is returned as is without being parsed.
func JSON(data json.RawMessage) json.RawMessage {
	if !mightHoldSecret(data) {
		return data
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return data
	}
	if !scrub(value) {
		return data
	}
	out, err := json.Marshal(value)
	if err != nil {
		return data
	}
	return out
}

func mightHoldSecret(data []byte) bool {
	lower := bytes.ToLower(data)
	for _, word := range secretWords {
		if bytes.Contains(lower, []byte(word)) {
			return true
		}
	}
	return false
}

// scrub blanks secret fields in place and reports whether it changed anything.
func scrub(value any) bool {
	changed := false
	switch node := value.(type) {
	case map[string]any:
		for key, child := range node {
			if IsSecretKey(key) {
				if s, ok := child.(string); !ok || s != "" {
					node[key] = ""
					changed = true
				}
				continue
			}
			changed = scrub(child) || changed
		}
	case []any:
		for _, child := range node {
			changed = scrub(child) || changed
		}
	}
	return changed
}
