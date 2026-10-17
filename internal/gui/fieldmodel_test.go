package gui

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func glb(declared, actual int) []byte {
	data := make([]byte, actual)
	copy(data, "glTF")
	binary.LittleEndian.PutUint32(data[4:], 2)
	binary.LittleEndian.PutUint32(data[8:], uint32(declared))
	return data
}

func TestImportFieldModel(t *testing.T) {
	saved := FieldModelPath
	FieldModelPath = filepath.Join(t.TempDir(), "models", "field-cad.glb")
	defer func() { FieldModelPath = saved }()

	if err := importFieldModel(bytes.NewReader([]byte("solid ascii stl"))); err == nil {
		t.Fatal("a non-glb file was accepted")
	}
	if err := importFieldModel(bytes.NewReader(glb(64, 40))); err == nil {
		t.Fatal("a truncated glb was accepted")
	}
	if currentFieldModel().Installed {
		t.Fatal("a rejected import left a field model behind")
	}
	if err := importFieldModel(bytes.NewReader(glb(64, 64))); err != nil {
		t.Fatalf("a valid glb was rejected: %v", err)
	}
	if status := currentFieldModel(); !status.Installed || status.Bytes != 64 {
		t.Fatalf("status after import = %+v", status)
	}
	entries, _ := os.ReadDir(filepath.Dir(FieldModelPath))
	if len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}
