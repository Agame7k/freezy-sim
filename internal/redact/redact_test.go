package redact

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestJSONBlanksSecretsAtAnyDepth(t *testing.T) {
	in := json.RawMessage(`{"Match":{"LongName":"Qualification 3"},"Teams":{"R1":{"Id":254,"Nickname":"Cheesy Poofs",` +
		`"WpaKey":"hunter22"},"B1":null},"Settings":[{"adminPassword":"pw","apiToken":"tok"}]}`)
	out := string(JSON(in))
	for _, secret := range []string{"hunter22", `"pw"`, `"tok"`} {
		if strings.Contains(out, secret) {
			t.Fatalf("secret %s survived: %s", secret, out)
		}
	}
	for _, kept := range []string{"Qualification 3", "Cheesy Poofs", `"Id":254`, `"WpaKey":""`} {
		if !strings.Contains(out, kept) {
			t.Fatalf("lost %s: %s", kept, out)
		}
	}
}

func TestJSONLeavesOrdinaryMessagesAlone(t *testing.T) {
	in := json.RawMessage(`{"Red":{"ScoreSummary":{"Score":42}}}`)
	if out := JSON(in); &out[0] != &in[0] {
		t.Fatal("a message with nothing secret in it was re-encoded")
	}
	empty := json.RawMessage(`{"WpaKey":""}`)
	if out := JSON(empty); &out[0] != &empty[0] {
		t.Fatal("an already-blank secret was re-encoded")
	}
	if !IsSecretKey("wpa_key") || !IsSecretKey("TbaSecret") || IsSecretKey("Nickname") {
		t.Fatal("IsSecretKey misclassified a field name")
	}
}
