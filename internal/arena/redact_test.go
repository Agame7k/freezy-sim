package arena

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agame7k/freezy-sim/internal/bus"
	"github.com/Agame7k/freezy-sim/internal/config"
)

// A match load carries each team's Wi-Fi key; the published topic (sent to the GUI, recorded, bundled) must not.
func TestMatchLoadTopicHasNoWifiKeys(t *testing.T) {
	hub := bus.New()
	client := NewClient(config.Default(), hub)
	load := json.RawMessage(`{"Match":{"Id":3,"LongName":"Qualification 3"},` +
		`"Teams":{"R1":{"Id":254,"Nickname":"Cheesy Poofs","WpaKey":"secretkey254"},"B1":{"Id":118,"WpaKey":"secretkey118"}}}`)
	client.handleMatchPlayMessage("matchLoad", load)

	topic := string(hub.Get("arena.matchLoad"))
	if strings.Contains(topic, "secretkey") {
		t.Fatalf("the published match load still has a Wi-Fi key: %s", topic)
	}
	if !strings.Contains(topic, "Cheesy Poofs") || !strings.Contains(topic, "Qualification 3") {
		t.Fatalf("redaction lost ordinary fields: %s", topic)
	}
	if team := client.MatchLoad().Teams["R1"]; team == nil || team.Id != 254 {
		t.Fatalf("the client's own match load lost the team: %+v", team)
	}
}
