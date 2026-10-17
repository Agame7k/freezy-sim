// Package recorder keeps a black box of everything the simulator saw (world snapshots, bus topics, events), saves
// every match as a recording, and replays recordings into the GUI.
package recorder

import (
	"bufio"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// Entry kinds.
const (
	KindWorld byte = 1 // binary world snapshot (world.encodeSnapshot format)
	KindTopic byte = 2 // bus topic value
	KindEvent byte = 3 // bus event (JSON)
)

// Entry is one recorded item.
type Entry struct {
	At    time.Time
	Kind  byte
	Topic string
	Data  []byte
}

// Chunk is a compressed run of entries. Keyframe holds every topic's value at Start, so playback can begin at any
// chunk without reading the ones before it.
type Chunk struct {
	Start    time.Time
	End      time.Time
	Keyframe map[string]json.RawMessage
	Count    int
	Data     []byte // flate-compressed entries (see encodeEntries)
}

// Meta describes a recording; it is stored uncompressed at the front of the file so listings stay cheap.
type Meta struct {
	Version int       `json:"version"`
	Label   string    `json:"label"`
	Kind    string    `json:"kind"` // "match" or "blackbox"
	Start   time.Time `json:"start"`
	End     time.Time `json:"end"`
	Teams   [6]int    `json:"teams"`
	Created time.Time `json:"created"`
}

// Duration is the recording length in seconds.
func (meta Meta) Duration() float64 { return meta.End.Sub(meta.Start).Seconds() }

// Recording is a saved slice of the black box.
type Recording struct {
	Meta   Meta
	Chunks []*Chunk
}

const fileMagic = "FSR1"

// encodeEntries packs entries as: u8 kind | i64 unix-nanos | u16 topic length | topic | u32 data length | data,
// then compresses the lot.
func encodeEntries(entries []Entry) []byte {
	var raw bytes.Buffer
	var header [15]byte
	for _, entry := range entries {
		header[0] = entry.Kind
		binary.LittleEndian.PutUint64(header[1:9], uint64(entry.At.UnixNano()))
		binary.LittleEndian.PutUint16(header[9:11], uint16(len(entry.Topic)))
		raw.Write(header[:11])
		raw.WriteString(entry.Topic)
		binary.LittleEndian.PutUint32(header[11:15], uint32(len(entry.Data)))
		raw.Write(header[11:15])
		raw.Write(entry.Data)
	}
	var compressed bytes.Buffer
	writer, _ := flate.NewWriter(&compressed, flate.BestSpeed)
	_, _ = writer.Write(raw.Bytes())
	_ = writer.Close()
	return compressed.Bytes()
}

// Entries decompresses the chunk.
func (chunk *Chunk) Entries() ([]Entry, error) {
	raw, err := io.ReadAll(flate.NewReader(bytes.NewReader(chunk.Data)))
	if err != nil {
		return nil, fmt.Errorf("corrupt chunk: %w", err)
	}
	entries := make([]Entry, 0, chunk.Count)
	for offset := 0; offset < len(raw); {
		if offset+11 > len(raw) {
			return nil, errors.New("truncated chunk entry")
		}
		kind := raw[offset]
		at := time.Unix(0, int64(binary.LittleEndian.Uint64(raw[offset+1:offset+9])))
		topicLength := int(binary.LittleEndian.Uint16(raw[offset+9 : offset+11]))
		offset += 11
		if offset+topicLength+4 > len(raw) {
			return nil, errors.New("truncated chunk entry")
		}
		topic := string(raw[offset : offset+topicLength])
		offset += topicLength
		dataLength := int(binary.LittleEndian.Uint32(raw[offset : offset+4]))
		offset += 4
		if offset+dataLength > len(raw) {
			return nil, errors.New("truncated chunk entry")
		}
		entries = append(entries, Entry{At: at, Kind: kind, Topic: topic, Data: raw[offset : offset+dataLength]})
		offset += dataLength
	}
	return entries, nil
}

// Size is the compressed size of the recording in bytes.
func (recording *Recording) Size() int {
	size := 0
	for _, chunk := range recording.Chunks {
		size += len(chunk.Data)
	}
	return size
}

// Save writes the recording: "FSR1" | u32 meta length | meta JSON | gob-encoded chunks.
func Save(path string, recording *Recording) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := Write(file, recording); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// Write encodes the recording in the file format Save uses.
func Write(out io.Writer, recording *Recording) error {
	writer := bufio.NewWriter(out)
	meta, _ := json.Marshal(recording.Meta)
	writer.WriteString(fileMagic)
	var length [4]byte
	binary.LittleEndian.PutUint32(length[:], uint32(len(meta)))
	writer.Write(length[:])
	writer.Write(meta)
	if err := gob.NewEncoder(writer).Encode(recording.Chunks); err != nil {
		return err
	}
	return writer.Flush()
}

// ReadMeta reads only the header of a recording file.
func ReadMeta(path string) (Meta, error) {
	file, err := os.Open(path)
	if err != nil {
		return Meta{}, err
	}
	defer file.Close()
	return readMeta(bufio.NewReader(file))
}

func readMeta(reader io.Reader) (Meta, error) {
	var header [8]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return Meta{}, fmt.Errorf("not a recording: %w", err)
	}
	if string(header[:4]) != fileMagic {
		return Meta{}, errors.New("not a freezy-sim recording")
	}
	length := binary.LittleEndian.Uint32(header[4:])
	if length > 1<<20 {
		return Meta{}, errors.New("recording header is too large")
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(reader, data); err != nil {
		return Meta{}, err
	}
	var meta Meta
	err := json.Unmarshal(data, &meta)
	return meta, err
}

// Load reads a whole recording.
func Load(path string) (*Recording, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	meta, err := readMeta(reader)
	if err != nil {
		return nil, err
	}
	recording := &Recording{Meta: meta}
	if err := gob.NewDecoder(reader).Decode(&recording.Chunks); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return recording, nil
}
