package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactedConfigBlanksPasswords(t *testing.T) {
	cfg := map[string]any{
		"arena": map[string]any{"adminPassword": "hunter2", "host": "127.0.0.1"},
		"scc":   []any{map[string]any{"password": "simpass", "username": "admin"}},
	}
	data, _ := json.Marshal(redactedConfig(cfg))
	text := string(data)
	if strings.Contains(text, "hunter2") || strings.Contains(text, "simpass") {
		t.Fatalf("password left in %s", text)
	}
	if !strings.Contains(text, "127.0.0.1") || !strings.Contains(text, "admin") {
		t.Fatalf("non-secret fields lost: %s", text)
	}
}

func TestRedactedConfigBlanksTokens(t *testing.T) {
	cfg := map[string]any{"gui": map[string]any{"remote": "lan", "token": "a1b2c3d4"}}
	data, _ := json.Marshal(redactedConfig(cfg))
	if text := string(data); strings.Contains(text, "a1b2c3d4") || !strings.Contains(text, "lan") {
		t.Fatalf("token not removed (or other fields lost): %s", text)
	}
}

func TestScrubSecretsRemovesAccessTokens(t *testing.T) {
	for _, line := range []string{
		"Remote access (LAN): http://192.168.1.5:8090/?token=0123abcd",
		`{"urls": ["http://10.0.0.2:8090/?panel=match&token=0123abcd"]}`,
		"GET /?TOKEN=0123abcd&x=1",
	} {
		if scrubbed := scrubSecrets(line); strings.Contains(scrubbed, "0123abcd") || !strings.Contains(scrubbed, "(removed)") {
			t.Errorf("scrubSecrets(%q) = %q", line, scrubbed)
		}
	}
	if line := "tokens are fine; so is token count 3"; scrubSecrets(line) != line {
		t.Errorf("scrubSecrets changed a line with no token value: %q", scrubSecrets(line))
	}
}
