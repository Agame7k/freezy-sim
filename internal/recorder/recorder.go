package recorder

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/bus"
)

const (
	blackBoxRetention = 10 * time.Minute
	chunkLength       = 10 * time.Second
	worldInterval     = 50 * time.Millisecond // world snapshots are kept at 20 Hz (the live view gets 60 Hz)
	matchLead         = 5 * time.Second       // match recordings start this long before the match
	matchTail         = 10 * time.Second      // and run this long past its end (hub run-out, final score)
	keepRecordings    = 200
	fileExtension     = ".fsr"
)

// Source is a stream of binary world snapshots (the world itself, or the switch the GUI watches).
type Source interface {
	Subscribe() (<-chan []byte, func())
}

// Recorder is the always-on black box.
type Recorder struct {
	hub *bus.Hub
	dir string

	mu        sync.Mutex
	topics    map[string]json.RawMessage // latest value of every recorded topic
	chunks    []*Chunk
	open      []Entry
	openStart time.Time
	openKey   map[string]json.RawMessage
	lastWorld []byte
	lastKept  time.Time
	match     *matchInfo
	saved     int // recordings written this session

	sealMu sync.Mutex // serializes sealing so chunks stay in order
}

type matchInfo struct {
	label string
	teams [6]int
	start time.Time
}

// New creates the black box; match recordings are written to dir.
func New(hub *bus.Hub, dir string) *Recorder {
	recorder := &Recorder{hub: hub, dir: dir, topics: make(map[string]json.RawMessage)}
	for name, data := range hub.Topics() {
		if recorded(name) {
			recorder.topics[name] = data
		}
	}
	recorder.openStart = time.Now()
	recorder.openKey = recorder.copyTopics()
	hub.OnTopic(recorder.onTopic)
	hub.OnEvent(recorder.onEvent)
	return recorder
}

// recorded reports whether a topic belongs in recordings (the recorder's and player's own topics do not).
func recorded(topic string) bool {
	return !strings.HasPrefix(topic, "replay.") && !strings.HasPrefix(topic, "recorder.")
}

// Run starts recording the world stream and sealing chunks.
func (recorder *Recorder) Run(world Source) {
	go func() {
		for {
			frames, cancel := world.Subscribe()
			for frame := range frames {
				recorder.onWorld(frame)
			}
			cancel()
			time.Sleep(time.Second)
		}
	}()
	go func() {
		for range time.Tick(time.Second) {
			recorder.mu.Lock()
			due := time.Since(recorder.openStart) >= chunkLength
			recorder.mu.Unlock()
			if due {
				recorder.seal()
			}
		}
	}()
	go func() {
		for range time.Tick(2 * time.Second) {
			recorder.publishState()
		}
	}()
}

func (recorder *Recorder) onTopic(topic string, data json.RawMessage) {
	if !recorded(topic) {
		return
	}
	recorder.mu.Lock()
	recorder.topics[topic] = data
	recorder.open = append(recorder.open, Entry{At: time.Now(), Kind: KindTopic, Topic: topic, Data: data})
	recorder.mu.Unlock()
}

func (recorder *Recorder) onEvent(event bus.Event) {
	data, _ := json.Marshal(event)
	recorder.mu.Lock()
	recorder.open = append(recorder.open, Entry{At: event.Time, Kind: KindEvent, Data: data})
	recorder.mu.Unlock()
}

func (recorder *Recorder) onWorld(frame []byte) {
	now := time.Now()
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.lastWorld = frame
	if now.Sub(recorder.lastKept) < worldInterval {
		return
	}
	recorder.lastKept = now
	recorder.open = append(recorder.open, Entry{At: now, Kind: KindWorld, Data: frame})
}

func (recorder *Recorder) copyTopics() map[string]json.RawMessage {
	topics := make(map[string]json.RawMessage, len(recorder.topics))
	for name, data := range recorder.topics {
		topics[name] = data
	}
	return topics
}

// seal compresses the open entries into a chunk and drops chunks older than the retention window. Compression runs
// outside the lock so publishers are never held up by it.
func (recorder *Recorder) seal() {
	recorder.sealMu.Lock()
	defer recorder.sealMu.Unlock()
	now := time.Now()
	recorder.mu.Lock()
	entries, start, keyframe := recorder.open, recorder.openStart, recorder.openKey
	recorder.open = nil
	recorder.openStart = now
	recorder.openKey = recorder.copyTopics()
	if recorder.lastWorld != nil {
		// Start every chunk with the current world so playback that begins there has something to show.
		recorder.open = append(recorder.open, Entry{At: now, Kind: KindWorld, Data: recorder.lastWorld})
	}
	recorder.mu.Unlock()

	chunk := &Chunk{Start: start, End: now, Keyframe: keyframe, Count: len(entries), Data: encodeEntries(entries)}
	recorder.mu.Lock()
	recorder.chunks = append(recorder.chunks, chunk)
	cutoff := now.Add(-blackBoxRetention)
	drop := 0
	for drop < len(recorder.chunks) && recorder.chunks[drop].End.Before(cutoff) {
		drop++
	}
	recorder.chunks = append([]*Chunk(nil), recorder.chunks[drop:]...)
	recorder.mu.Unlock()
}

// Snapshot returns the recorded span between from and to (clamped to what the black box still holds).
func (recorder *Recorder) Snapshot(from, to time.Time, kind, label string) (*Recording, error) {
	recorder.seal()
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recording := &Recording{Meta: Meta{Version: 1, Kind: kind, Label: label, Created: time.Now()}}
	for _, chunk := range recorder.chunks {
		if chunk.End.Before(from) || chunk.Start.After(to) {
			continue
		}
		recording.Chunks = append(recording.Chunks, chunk)
	}
	if len(recording.Chunks) == 0 {
		return nil, errors.New("the black box holds nothing for that time span")
	}
	recording.Meta.Start = from
	if first := recording.Chunks[0].Start; first.After(from) {
		recording.Meta.Start = first
	}
	recording.Meta.End = to
	if last := recording.Chunks[len(recording.Chunks)-1].End; last.Before(to) {
		recording.Meta.End = last
	}
	return recording, nil
}

// BlackBox returns the last span of the black box (all of it if span is zero).
func (recorder *Recorder) BlackBox(span time.Duration) (*Recording, error) {
	if span <= 0 || span > blackBoxRetention {
		span = blackBoxRetention
	}
	now := time.Now()
	return recorder.Snapshot(now.Add(-span), now, "blackbox",
		fmt.Sprintf("Black box, last %.0f min (%s)", span.Minutes(), now.Format("15:04:05")))
}

// MatchStarted marks the start of a match recording.
func (recorder *Recorder) MatchStarted(label string, teams [6]int) {
	recorder.mu.Lock()
	recorder.match = &matchInfo{label: label, teams: teams, start: time.Now()}
	recorder.mu.Unlock()
	recorder.publishState()
}

// MatchEnded saves the match (plus a short tail) once the tail has been recorded.
func (recorder *Recorder) MatchEnded() {
	recorder.mu.Lock()
	match := recorder.match
	recorder.match = nil
	recorder.mu.Unlock()
	if match == nil {
		return
	}
	end := time.Now().Add(matchTail)
	time.AfterFunc(matchTail+500*time.Millisecond, func() {
		recording, err := recorder.Snapshot(match.start.Add(-matchLead), end, "match", match.label)
		if err != nil {
			recorder.hub.Warnf("recorder", "Could not record %s: %v", match.label, err)
			return
		}
		recording.Meta.Teams = match.teams
		name, err := recorder.write(recording, match.label)
		if err != nil {
			recorder.hub.Errorf("recorder", "Could not save the recording of %s: %v", match.label, err)
			return
		}
		recorder.hub.Logf("recorder", "Saved %s (%.0f s, %.1f MB) as %s", match.label, recording.Meta.Duration(),
			float64(recording.Size())/1e6, name)
		recorder.prune()
		recorder.publishState()
	})
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func (recorder *Recorder) write(recording *Recording, label string) (string, error) {
	if err := os.MkdirAll(recorder.dir, 0o755); err != nil {
		return "", err
	}
	slug := strings.Trim(unsafeName.ReplaceAllString(label, "-"), "-")
	if len(slug) > 40 {
		slug = slug[:40]
	}
	name := recording.Meta.Created.Format("20060102-150405") + "_" + slug + fileExtension
	if err := Save(filepath.Join(recorder.dir, name), recording); err != nil {
		return "", err
	}
	recorder.mu.Lock()
	recorder.saved++
	recorder.mu.Unlock()
	return name, nil
}

// prune keeps only the newest match recordings (saved black boxes are kept until deleted).
func (recorder *Recorder) prune() {
	list, err := recorder.List()
	if err != nil {
		return
	}
	matches := 0
	for _, item := range list {
		if item.Meta.Kind != "match" {
			continue
		}
		matches++
		if matches > keepRecordings {
			_ = os.Remove(filepath.Join(recorder.dir, item.Name))
		}
	}
}

// ListItem is one saved recording.
type ListItem struct {
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	Duration float64 `json:"duration"`
	Meta     Meta    `json:"meta"`
}

// List returns the saved recordings, newest first.
func (recorder *Recorder) List() ([]ListItem, error) {
	files, err := filepath.Glob(filepath.Join(recorder.dir, "*"+fileExtension))
	if err != nil {
		return nil, err
	}
	list := []ListItem{}
	for _, path := range files {
		meta, err := ReadMeta(path)
		if err != nil {
			continue
		}
		info, _ := os.Stat(path)
		item := ListItem{Name: filepath.Base(path), Duration: meta.Duration(), Meta: meta}
		if info != nil {
			item.Size = info.Size()
		}
		list = append(list, item)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Meta.Created.After(list[j].Meta.Created) })
	return list, nil
}

// Path resolves a recording name inside the recordings folder, rejecting anything that would escape it.
func (recorder *Recorder) Path(name string) (string, error) {
	if name == "" || filepath.Base(name) != name || !strings.HasSuffix(name, fileExtension) {
		return "", fmt.Errorf("invalid recording name %q", name)
	}
	return filepath.Join(recorder.dir, name), nil
}

// State is published on recorder.state.
type State struct {
	Seconds   float64 `json:"seconds"` // black box span currently held
	Bytes     int     `json:"bytes"`
	Chunks    int     `json:"chunks"`
	Recording string  `json:"recording"` // label of the match being recorded, if any
	Saved     int     `json:"saved"`
	Dir       string  `json:"dir"`
}

func (recorder *Recorder) publishState() {
	recorder.mu.Lock()
	state := State{Chunks: len(recorder.chunks), Saved: recorder.saved, Dir: recorder.dir}
	for _, chunk := range recorder.chunks {
		state.Bytes += len(chunk.Data)
	}
	if len(recorder.chunks) > 0 {
		state.Seconds = time.Since(recorder.chunks[0].Start).Seconds()
	} else {
		state.Seconds = time.Since(recorder.openStart).Seconds()
	}
	if recorder.match != nil {
		state.Recording = recorder.match.label
	}
	recorder.mu.Unlock()
	state.Seconds = float64(int(state.Seconds))
	recorder.hub.Publish("recorder.state", state)
}

// RegisterCommands adds the recorder.* commands.
func (recorder *Recorder) RegisterCommands() {
	recorder.hub.Handle("recorder.list", func(_ []byte) (any, error) { return recorder.List() })
	recorder.hub.Handle("recorder.saveBlackBox", func(args []byte) (any, error) {
		var request struct {
			Minutes float64 `json:"minutes"`
		}
		_ = json.Unmarshal(args, &request)
		recording, err := recorder.BlackBox(time.Duration(request.Minutes * float64(time.Minute)))
		if err != nil {
			return nil, err
		}
		name, err := recorder.write(recording, "blackbox")
		if err != nil {
			return nil, err
		}
		recorder.hub.Logf("recorder", "Saved the black box (%.0f s) as %s", recording.Meta.Duration(), name)
		recorder.publishState()
		return map[string]string{"name": name}, nil
	})
	recorder.hub.Handle("recorder.delete", func(args []byte) (any, error) {
		var request struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		path, err := recorder.Path(request.Name)
		if err != nil {
			return nil, err
		}
		return nil, os.Remove(path)
	})
}
