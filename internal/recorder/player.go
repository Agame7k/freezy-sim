package recorder

import (
	"encoding/json"
	"errors"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/bus"
)

// Stream is the world stream the GUI watches: live snapshots normally, the player's while a replay is active.
type Stream struct {
	live Source

	mu     sync.Mutex
	active bool
	outs   map[chan []byte]struct{}
}

// NewStream wraps the live world stream.
func NewStream(live Source) *Stream {
	return &Stream{live: live, outs: make(map[chan []byte]struct{})}
}

// Subscribe implements gui.BinaryStream.
func (stream *Stream) Subscribe() (<-chan []byte, func()) {
	out := make(chan []byte, 4)
	frames, cancelLive := stream.live.Subscribe()
	stream.mu.Lock()
	stream.outs[out] = struct{}{}
	stream.mu.Unlock()
	go func() {
		for frame := range frames {
			stream.mu.Lock()
			if _, ok := stream.outs[out]; ok && !stream.active {
				select {
				case out <- frame:
				default:
				}
			}
			stream.mu.Unlock()
		}
	}()
	var once sync.Once
	return out, func() {
		once.Do(func() {
			cancelLive()
			stream.mu.Lock()
			delete(stream.outs, out)
			close(out)
			stream.mu.Unlock()
		})
	}
}

func (stream *Stream) setActive(active bool) {
	stream.mu.Lock()
	stream.active = active
	stream.mu.Unlock()
}

func (stream *Stream) inject(frame []byte) {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if !stream.active {
		return
	}
	for out := range stream.outs {
		select {
		case out <- frame:
		default:
		}
	}
}

const replayEventHistory = 200

// Player replays a recording into the GUI. Recorded topics are republished as "replay.t.<topic>" (the front end
// shows those in place of the live values while a replay is active) and world snapshots replace the live 3D stream.
type Player struct {
	hub    *bus.Hub
	stream *Stream

	mu        sync.Mutex
	name      string
	meta      Meta
	chunks    []*Chunk
	entries   []Entry
	chunkAt   []int // index into entries of each chunk's first entry
	events    []Entry
	cursor    int // next entry to apply
	position  time.Time
	playing   bool
	speed     float64
	published map[string]bool
	lastTick  time.Time
}

// NewPlayer creates an idle player that drives stream.
func NewPlayer(hub *bus.Hub, stream *Stream) *Player {
	player := &Player{hub: hub, stream: stream, speed: 1, published: make(map[string]bool)}
	go player.loop()
	player.publishState()
	return player
}

// Load replaces whatever is playing with recording and shows its first moment, paused.
func (player *Player) Load(name string, recording *Recording) error {
	var entries, events []Entry
	chunkAt := make([]int, 0, len(recording.Chunks))
	for _, chunk := range recording.Chunks {
		chunkEntries, err := chunk.Entries()
		if err != nil {
			return err
		}
		sort.SliceStable(chunkEntries, func(i, j int) bool { return chunkEntries[i].At.Before(chunkEntries[j].At) })
		chunkAt = append(chunkAt, len(entries))
		for _, entry := range chunkEntries {
			if entry.Kind == KindEvent {
				events = append(events, entry)
			}
		}
		entries = append(entries, chunkEntries...)
	}
	if len(entries) == 0 {
		return errors.New("the recording is empty")
	}
	player.mu.Lock()
	player.name, player.meta, player.chunks = name, recording.Meta, recording.Chunks
	player.entries, player.chunkAt, player.events = entries, chunkAt, events
	player.playing = false
	player.stream.setActive(true)
	player.seekLocked(recording.Meta.Start)
	player.mu.Unlock()
	player.publishState()
	return nil
}

// Stop leaves replay mode and hands the GUI back to the live simulator.
func (player *Player) Stop() {
	player.mu.Lock()
	player.chunks, player.entries, player.events, player.chunkAt = nil, nil, nil, nil
	player.playing = false
	player.name = ""
	for topic := range player.published {
		player.hub.Delete("replay.t." + topic)
	}
	player.published = make(map[string]bool)
	player.hub.Delete("replay.events")
	player.stream.setActive(false)
	player.mu.Unlock()
	player.publishState()
}

func (player *Player) active() bool { return player.entries != nil }

// seekLocked rebuilds the state at t from the nearest keyframe at or before it.
func (player *Player) seekLocked(t time.Time) {
	if t.Before(player.meta.Start) {
		t = player.meta.Start
	}
	if t.After(player.meta.End) {
		t = player.meta.End
	}
	chunk := 0
	for i, candidate := range player.chunks {
		if !candidate.Start.After(t) {
			chunk = i
		}
	}
	state := make(map[string]json.RawMessage, len(player.chunks[chunk].Keyframe))
	for name, data := range player.chunks[chunk].Keyframe {
		state[name] = data
	}
	var world []byte
	index := player.chunkAt[chunk]
	for ; index < len(player.entries) && !player.entries[index].At.After(t); index++ {
		entry := player.entries[index]
		switch entry.Kind {
		case KindTopic:
			state[entry.Topic] = entry.Data
		case KindWorld:
			world = entry.Data
		}
	}
	for name := range player.published {
		if _, ok := state[name]; !ok {
			player.hub.Delete("replay.t." + name)
			delete(player.published, name)
		}
	}
	for name, data := range state {
		player.publishTopic(name, data)
	}
	if world != nil {
		player.stream.inject(world)
	}
	player.cursor = index
	player.position = t
	player.publishEvents()
}

func (player *Player) publishTopic(name string, data json.RawMessage) {
	player.published[name] = true
	player.hub.Publish("replay.t."+name, data)
}

func (player *Player) publishEvents() {
	count := sort.Search(len(player.events), func(i int) bool { return player.events[i].At.After(player.position) })
	from := max(0, count-replayEventHistory)
	events := make([]json.RawMessage, 0, count-from)
	for _, entry := range player.events[from:count] {
		events = append(events, entry.Data)
	}
	player.hub.Publish("replay.events", events)
}

// advanceLocked applies every entry up to t; only the newest world snapshot in the step is sent.
func (player *Player) advanceLocked(t time.Time) {
	if t.After(player.meta.End) {
		t = player.meta.End
		player.playing = false
	}
	var world []byte
	sawEvent := false
	for ; player.cursor < len(player.entries) && !player.entries[player.cursor].At.After(t); player.cursor++ {
		entry := player.entries[player.cursor]
		switch entry.Kind {
		case KindTopic:
			player.publishTopic(entry.Topic, entry.Data)
		case KindWorld:
			world = entry.Data
		case KindEvent:
			sawEvent = true
		}
	}
	if world != nil {
		player.stream.inject(world)
	}
	player.position = t
	if sawEvent {
		player.publishEvents()
	}
}

func (player *Player) loop() {
	ticker := time.NewTicker(15 * time.Millisecond)
	defer ticker.Stop()
	lastState := time.Now()
	last := time.Now()
	for now := range ticker.C {
		elapsed := now.Sub(last)
		last = now
		player.mu.Lock()
		changed := false
		if player.active() && player.playing {
			player.advanceLocked(player.position.Add(time.Duration(float64(elapsed) * player.speed)))
			changed = true
		}
		player.mu.Unlock()
		if changed && now.Sub(lastState) > 200*time.Millisecond {
			lastState = now
			player.publishState()
		}
	}
}

// PlayerState is published on replay.state.
type PlayerState struct {
	Active   bool    `json:"active"`
	Name     string  `json:"name"`
	Meta     Meta    `json:"meta"`
	Duration float64 `json:"duration"`
	Position float64 `json:"position"` // seconds from the start of the recording
	Playing  bool    `json:"playing"`
	Speed    float64 `json:"speed"`
	// Clock is the wall time at the playhead (when it happened).
	Clock time.Time `json:"clock"`
}

// State returns the current player state.
func (player *Player) State() PlayerState {
	player.mu.Lock()
	defer player.mu.Unlock()
	state := PlayerState{Active: player.active(), Speed: player.speed}
	if state.Active {
		state.Name, state.Meta, state.Playing, state.Clock = player.name, player.meta, player.playing, player.position
		state.Duration = player.meta.Duration()
		state.Position = math.Round(player.position.Sub(player.meta.Start).Seconds()*10) / 10
	}
	return state
}

func (player *Player) publishState() { player.hub.Publish("replay.state", player.State()) }

var errNoReplay = errors.New("no recording is loaded")

// Play resumes playback (from the start if it had reached the end).
func (player *Player) Play() error {
	player.mu.Lock()
	if !player.active() {
		player.mu.Unlock()
		return errNoReplay
	}
	if !player.position.Before(player.meta.End) {
		player.seekLocked(player.meta.Start)
	}
	player.playing = true
	player.mu.Unlock()
	player.publishState()
	return nil
}

// Pause holds the playhead.
func (player *Player) Pause() {
	player.mu.Lock()
	player.playing = false
	player.mu.Unlock()
	player.publishState()
}

// Seek moves the playhead to seconds from the start of the recording.
func (player *Player) Seek(seconds float64) error {
	player.mu.Lock()
	if !player.active() {
		player.mu.Unlock()
		return errNoReplay
	}
	player.seekLocked(player.meta.Start.Add(time.Duration(seconds * float64(time.Second))))
	player.mu.Unlock()
	player.publishState()
	return nil
}

// SetSpeed sets the playback rate (0.1x to 16x).
func (player *Player) SetSpeed(speed float64) {
	player.mu.Lock()
	player.speed = math.Max(0.1, math.Min(16, speed))
	player.mu.Unlock()
	player.publishState()
}

// RegisterCommands adds the replay.* commands; loading reads recordings through recorder.
func (player *Player) RegisterCommands(recorder *Recorder) {
	player.hub.Handle("replay.load", func(args []byte) (any, error) {
		var request struct {
			Name     string `json:"name"`
			BlackBox bool   `json:"blackBox"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		var recording *Recording
		var err error
		name := request.Name
		if request.BlackBox {
			recording, err = recorder.BlackBox(0)
			name = "black box"
		} else {
			var path string
			if path, err = recorder.Path(request.Name); err == nil {
				recording, err = Load(path)
			}
		}
		if err != nil {
			return nil, err
		}
		if err := player.Load(name, recording); err != nil {
			return nil, err
		}
		player.hub.Logf("replay", "Replaying %s (%.0f s)", recording.Meta.Label, recording.Meta.Duration())
		return player.State(), nil
	})
	player.hub.Handle("replay.play", func(_ []byte) (any, error) { return nil, player.Play() })
	player.hub.Handle("replay.pause", func(_ []byte) (any, error) { player.Pause(); return nil, nil })
	player.hub.Handle("replay.stop", func(_ []byte) (any, error) { player.Stop(); return nil, nil })
	player.hub.Handle("replay.seek", func(args []byte) (any, error) {
		var request struct {
			Position float64 `json:"position"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		return nil, player.Seek(request.Position)
	})
	player.hub.Handle("replay.speed", func(args []byte) (any, error) {
		var request struct {
			Speed float64 `json:"speed"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		player.SetSpeed(request.Speed)
		return nil, nil
	})
}
