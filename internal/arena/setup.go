package arena

import (
	"bufio"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Checkbox fields in the arena settings form: present as "on" when checked, absent otherwise.
var settingsCheckboxes = map[string]bool{
	"selectionShowUnpickedTeams": true, "tbaDownloadEnabled": true, "tbaPublishingEnabled": true,
	"autoAudienceDisplayEnabled": true, "nexusEnabled": true, "nexusAutoQueueEnabled": true,
	"networkSecurityEnabled": true, "sccManagementEnabled": true, "useLiteUdpPort": true,
	"alternateIOEnabled": true, "flashDSEnabled": true,
}

var (
	tagPattern  = regexp.MustCompile(`(?is)<(/?)(input|select|option|textarea|form)\b([^>]*)>`)
	attrPattern = regexp.MustCompile(`([a-zA-Z_][-a-zA-Z0-9_:]*)\s*(?:=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+)))?`)
)

func parseAttrs(text string) map[string]string {
	attrs := make(map[string]string)
	for _, match := range attrPattern.FindAllStringSubmatch(text, -1) {
		name := strings.ToLower(match[1])
		value := match[2] + match[3] + match[4]
		attrs[name] = html.UnescapeString(value)
	}
	return attrs
}

// ParseForm extracts the submitted values of the form posting to action, the way a browser would serialize it.
func ParseForm(page, action string) (url.Values, error) {
	values := url.Values{}
	inForm := false
	found := false
	var selectName, selectFirst string
	var selectChosen bool
	var textareaName string
	textareaStart := -1

	for _, loc := range tagPattern.FindAllStringSubmatchIndex(page, -1) {
		closing := page[loc[2]:loc[3]] == "/"
		tag := strings.ToLower(page[loc[4]:loc[5]])
		attrs := parseAttrs(page[loc[6]:loc[7]])

		if tag == "form" {
			if closing {
				if inForm {
					inForm = false
				}
				continue
			}
			inForm = attrs["action"] == action
			found = found || inForm
			continue
		}
		if !inForm {
			continue
		}
		switch tag {
		case "input":
			name := attrs["name"]
			if name == "" || closing {
				continue
			}
			_, checked := attrs["checked"]
			switch strings.ToLower(attrs["type"]) {
			case "checkbox":
				if checked {
					value := attrs["value"]
					if value == "" {
						value = "on"
					}
					values.Add(name, value)
				}
			case "radio":
				if checked {
					values.Set(name, attrs["value"])
				}
			case "submit", "button", "file", "reset":
			default:
				values.Set(name, attrs["value"])
			}
		case "select":
			if closing {
				if selectName != "" && !selectChosen {
					values.Set(selectName, selectFirst)
				}
				selectName = ""
				continue
			}
			selectName = attrs["name"]
			selectFirst = ""
			selectChosen = false
		case "option":
			if closing || selectName == "" {
				continue
			}
			value, hasValue := attrs["value"]
			if !hasValue {
				end := strings.Index(page[loc[1]:], "<")
				if end >= 0 {
					value = strings.TrimSpace(html.UnescapeString(page[loc[1] : loc[1]+end]))
				}
			}
			value = strings.TrimSpace(value)
			if selectFirst == "" {
				selectFirst = value
			}
			if _, selected := attrs["selected"]; selected && !selectChosen {
				values.Set(selectName, value)
				selectChosen = true
			}
		case "textarea":
			if !closing {
				textareaName = attrs["name"]
				textareaStart = loc[1]
			} else if textareaName != "" && textareaStart >= 0 {
				content := html.UnescapeString(page[textareaStart:loc[0]])
				content = strings.TrimPrefix(content, "\n")
				values.Set(textareaName, content)
				textareaName = ""
				textareaStart = -1
			}
		}
	}
	if !found {
		return nil, fmt.Errorf("form %q not found on page", action)
	}
	return values, nil
}

func (client *Client) getPage(path string) (string, error) {
	request, err := http.NewRequest("GET", client.cfg.ArenaURL()+path, nil)
	if err != nil {
		return "", err
	}
	for key, values := range client.authHeader() {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	httpClient := http.Client{Timeout: 5 * time.Second}
	response, err := httpClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		return "", fmt.Errorf("GET %s: %s", path, response.Status)
	}
	body, err := io.ReadAll(response.Body)
	return string(body), err
}

// ReadSettings returns the arena's current settings form values.
func (client *Client) ReadSettings() (url.Values, error) {
	page, err := client.getPage("/setup/settings")
	if err != nil {
		return nil, err
	}
	return ParseForm(page, "/setup/settings")
}

// ApplySettings reads the full settings form, applies only the given changes, and posts the whole form back so no
// unrelated setting is reset. For checkbox fields use "on" to check and "" to uncheck.
func (client *Client) ApplySettings(changes map[string]string) ([]string, error) {
	values, err := client.ReadSettings()
	if err != nil {
		return nil, err
	}
	var changed []string
	keys := make([]string, 0, len(changes))
	for key := range changes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := changes[key]
		old := values.Get(key)
		if settingsCheckboxes[key] {
			if value == "" {
				values.Del(key)
			} else {
				values.Set(key, "on")
				value = "on"
			}
		} else {
			values.Set(key, value)
		}
		if old != value {
			changed = append(changed, fmt.Sprintf("%s: %q → %q", key, old, value))
		}
	}
	if len(changed) == 0 {
		return nil, nil
	}
	if err := client.PostForm("/setup/settings", values); err != nil {
		return nil, err
	}
	// Read the form back: the arena silently ignores values it rejects, and a setting that did not stick means a
	// simulated device the arena will never talk to.
	after, err := client.ReadSettings()
	if err != nil {
		return changed, fmt.Errorf("settings saved but could not be re-read: %w", err)
	}
	var mismatched []string
	for _, key := range keys {
		want := values.Get(key)
		if got := after.Get(key); got != want {
			mismatched = append(mismatched, fmt.Sprintf("%s is %q, wanted %q", key, got, want))
		}
	}
	if len(mismatched) > 0 {
		return changed, fmt.Errorf("the arena did not keep these settings: %s", strings.Join(mismatched, "; "))
	}
	return changed, nil
}

// ListTeams returns the team numbers registered in the arena.
func (client *Client) ListTeams() ([]int, error) {
	page, err := client.getPage("/reports/csv/teams")
	if err != nil {
		return nil, err
	}
	var teams []int
	scanner := bufio.NewScanner(strings.NewReader(page))
	for scanner.Scan() {
		line := scanner.Text()
		comma := strings.IndexByte(line, ',')
		if comma <= 0 {
			continue
		}
		if number, err := strconv.Atoi(line[:comma]); err == nil {
			teams = append(teams, number)
		}
	}
	return teams, nil
}

// AddTeams registers any of the given teams that the arena does not have yet.
func (client *Client) AddTeams(teams []int) ([]int, error) {
	existing, err := client.ListTeams()
	if err != nil {
		return nil, err
	}
	have := make(map[int]bool, len(existing))
	for _, team := range existing {
		have[team] = true
	}
	var missing []string
	var added []int
	for _, team := range teams {
		if team > 0 && !have[team] {
			missing = append(missing, strconv.Itoa(team))
			added = append(added, team)
			have[team] = true
		}
	}
	if len(missing) == 0 {
		return nil, nil
	}
	err = client.PostForm("/setup/teams", url.Values{"teamNumbers": {strings.Join(missing, "\r\n")}})
	return added, err
}

// SubstituteTeams fills the current (test/practice/playoff) match with the given teams.
func (client *Client) SubstituteTeams(teams [6]int) error {
	return client.SendMatchPlay("substituteTeams", map[string]int{
		"Red1": teams[0], "Red2": teams[1], "Red3": teams[2], "Blue1": teams[3], "Blue2": teams[4], "Blue3": teams[5],
	})
}
