package recorder

import (
	"bytes"
	"testing"
	"time"
)

// FuzzRecording loads arbitrary bytes as a recording file and decodes every chunk: a corrupt or hostile .fsr must
// fail with an error, not crash the simulator.
func FuzzRecording(f *testing.F) {
	start := time.Unix(1700000000, 0)
	recording := &Recording{Meta: Meta{Start: start, End: start.Add(time.Second)}}
	recording.Chunks = append(recording.Chunks, &Chunk{Start: start, End: start, Count: 1,
		Data: encodeEntries([]Entry{{At: start, Kind: 1, Topic: "world.state", Data: []byte(`{}`)}})})
	var file bytes.Buffer
	if err := Write(&file, recording); err != nil {
		f.Fatal(err)
	}
	f.Add(file.Bytes())
	f.Add([]byte(fileMagic))
	f.Fuzz(func(t *testing.T, data []byte) {
		loaded, err := read(bytes.NewReader(data))
		if err != nil {
			return
		}
		for _, chunk := range loaded.Chunks {
			if chunk != nil {
				_, _ = chunk.Entries()
			}
		}
	})
}

// FuzzChunk decodes arbitrary chunk payloads.
func FuzzChunk(f *testing.F) {
	start := time.Unix(1700000000, 0)
	f.Add(encodeEntries([]Entry{{At: start, Kind: 1, Topic: "a", Data: []byte("b")}}), 1)
	f.Add(encodeEntries([]Entry{{At: start, Kind: 1, Topic: "a", Data: []byte("b")}}), -1) // a negative count from a damaged file used to panic in make
	f.Fuzz(func(t *testing.T, data []byte, count int) {
		_, _ = (&Chunk{Data: data, Count: count}).Entries()
	})
}
