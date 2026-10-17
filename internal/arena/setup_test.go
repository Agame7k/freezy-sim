package arena

import "testing"

const sampleSettingsPage = `
<form id="other" action="/setup/db/clear/playoff" method="POST"><input name="junk" value="x"></form>
<form id="settingsForm" method="POST" action="/setup/settings">
  <input type="hidden" name="activeSettingsTab" value="general">
  <input type="text" class="form-control" name="name" value="Sim &amp; Co">
  <input type="radio" name="playoffType" value="DoubleEliminationPlayoff"
    checked>
  <input type="radio" name="playoffType" value="SingleEliminationPlayoff"
    >
  <input type="checkbox" id="tbaDownloadEnabled"
    name="tbaDownloadEnabled"  checked>
  <input type="checkbox" name="alternateIOEnabled" >
  <select class="form-select" name="apChannel" value="37">
    <option value="5">5</option>
    <option value="37"
      selected>
      37
    </option>
  </select>
  <select name="ledUniverseMode">
    <option value="single" >Single Universe</option>
    <option value="two" >Two Universes</option>
  </select>
  <textarea class="form-control" name="sccUpCommands" rows="8">line one
line &lt;two&gt;</textarea>
  <input type="text" name="autoDurationSec" value="20">
</form>`

func TestParseForm(t *testing.T) {
	values, err := ParseForm(sampleSettingsPage, "/setup/settings")
	if err != nil {
		t.Fatal(err)
	}
	expect := map[string]string{
		"activeSettingsTab": "general", "name": "Sim & Co", "playoffType": "DoubleEliminationPlayoff",
		"tbaDownloadEnabled": "on", "apChannel": "37", "ledUniverseMode": "single",
		"sccUpCommands": "line one\nline <two>", "autoDurationSec": "20",
	}
	for key, want := range expect {
		if got := values.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if values.Has("alternateIOEnabled") {
		t.Error("unchecked checkbox should be absent")
	}
	if values.Has("junk") {
		t.Error("field from another form leaked in")
	}
}
