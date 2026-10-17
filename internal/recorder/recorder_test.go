package recorder

import (
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agame7k/freezy-sim/internal/bus"
)

// fakeWorld is a Source the test pushes frames into.
type fakeWorld struct {
	mu   sync.Mutex
	subs []chan []byte
}

func (world *fakeWorld) Subscribe() (<-chan []byte, func()) {
	channel := make(chan []byte, 16)
	world.mu.Lock()
	world.subs = append(world.subs, channel)
	world.mu.Unlock()
	return channel, func() {}
}

func (world *fakeWorld) push(frame []byte) {
	world.mu.Lock()
	defer world.mu.Unlock()
	for _, sub := range world.subs {
		sub <- frame
	}
}

func TestEntriesRoundTrip(t *testing.T) {
	now := time.Now()
	entries := []Entry{
		{At: now, Kind: KindTopic, Topic: "a.b", Data: []byte(`{"x":1}`)},
		{At: now.Add(time.Millisecond), Kind: KindWorld, Data: []byte{1, 2, 3}},
		{At: now.Add(2 * time.Millisecond), Kind: KindEvent, Data: []byte(`{}`)},
	}
	chunk := &Chunk{Count: len(entries), Data: encodeEntries(entries)}
	decoded, err := chunk.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 3 || decoded[0].Topic != "a.b" || string(decoded[0].Data) != `{"x":1}` ||
		decoded[1].Kind != KindWorld || len(decoded[1].Data) != 3 || !decoded[2].At.Equal(entries[2].At) {
		t.Fatalf("round trip mismatch: %+v", decoded)
	}
}

func TestRecordSaveLoadReplay(t *testing.T) {
	hub := bus.New()
	hub.Publish("score", map[string]int{"red": 0})
	recorder := New(hub, t.TempDir())
	time.Sleep(5 * time.Millisecond) // keep the first change clear of the chunk start on coarse clocks
	hub.Publish("score", map[string]int{"red": 5})
	hub.Logf("test", "five points")
	time.Sleep(20 * time.Millisecond)
	hub.Publish("score", map[string]int{"red": 9})
	hub.Publish("replay.t.ignored", 1) // the player's own topics are never recorded

	recording, err := recorder.BlackBox(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(recorder.dir, "test"+fileExtension)
	if err := Save(path, recording); err != nil {
		t.Fatal(err)
	}
	meta, err := ReadMeta(path)
	if err != nil || meta.Kind != "blackbox" {
		t.Fatalf("meta %+v, %v", meta, err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Chunks) != len(recording.Chunks) {
		t.Fatalf("chunks %d != %d", len(loaded.Chunks), len(recording.Chunks))
	}
	for _, chunk := range loaded.Chunks {
		entries, _ := chunk.Entries()
		for _, entry := range entries {
			if entry.Topic == "replay.t.ignored" {
				t.Fatal("recorded a replay topic")
			}
		}
	}

	world := &fakeWorld{}
	stream := NewStream(world)
	view, cancel := stream.Subscribe()
	defer cancel()
	player := NewPlayer(hub, stream)
	if err := player.Load("test", loaded); err != nil {
		t.Fatal(err)
	}
	// Loading shows the first moment: the keyframe value from before recording started.
	var score map[string]int
	_ = json.Unmarshal(hub.Get("replay.t.score"), &score)
	if score["red"] != 0 {
		t.Fatalf("at start want red 0, got %v", score)
	}
	if err := player.Seek(loaded.Meta.Duration()); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(hub.Get("replay.t.score"), &score)
	if score["red"] != 9 {
		t.Fatalf("at end want red 9, got %v", score)
	}
	var events []bus.Event
	_ = json.Unmarshal(hub.Get("replay.events"), &events)
	if len(events) == 0 || events[len(events)-1].Msg != "five points" {
		t.Fatalf("events not replayed: %+v", events)
	}

	// Live frames are held back while the replay owns the view, and flow again once it stops.
	world.push([]byte("live-1"))
	select {
	case frame := <-view:
		t.Fatalf("live frame leaked into replay: %q", frame)
	case <-time.After(50 * time.Millisecond):
	}
	player.Stop()
	if hub.Get("replay.t.score") != nil {
		t.Fatal("replay topics not cleared on stop")
	}
	world.push([]byte("live-2"))
	select {
	case frame := <-view:
		if string(frame) != "live-2" {
			t.Fatalf("got %q", frame)
		}
	case <-time.After(time.Second):
		t.Fatal("live frames did not resume")
	}
}
