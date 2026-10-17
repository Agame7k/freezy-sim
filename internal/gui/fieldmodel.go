package gui

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// The official field CAD mesh is not shipped with the simulator: FIRST publishes the field STEP for teams, and each
// user converts their own copy with tools/cad/export_field.py (docs/FIELD_MODEL.md) and imports the .glb here. The
// 3D view loads it from /models/field-cad.glb and falls back to the simple built field when it is missing.

// FieldModelPath is where an imported field mesh is kept (relative to the simulator folder).
var FieldModelPath = filepath.Join("run", "models", "field-cad.glb")

const maxFieldModelBytes = 512 << 20

type fieldModelStatus struct {
	Installed bool      `json:"installed"`
	Bytes     int64     `json:"bytes,omitempty"`
	Modified  time.Time `json:"modified,omitempty"`
	Path      string    `json:"path"`
}

func currentFieldModel() fieldModelStatus {
	status := fieldModelStatus{Path: FieldModelPath}
	if info, err := os.Stat(FieldModelPath); err == nil && !info.IsDir() {
		status.Installed, status.Bytes, status.Modified = true, info.Size(), info.ModTime()
	}
	return status
}

func (server *Server) handleFieldModelFile(w http.ResponseWriter, r *http.Request) {
	if !currentFieldModel().Installed {
		http.Error(w, "no field model imported (see docs/FIELD_MODEL.md)", http.StatusNotFound)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, FieldModelPath)
}

func (server *Server) handleFieldModel(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut, http.MethodPost:
		if err := importFieldModel(http.MaxBytesReader(w, r.Body, maxFieldModelBytes)); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		server.hub.Logf("gui", "Imported the field model into %s", FieldModelPath)
	case http.MethodDelete:
		if err := os.Remove(FieldModelPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, currentFieldModel())
}

// importFieldModel checks that body is a binary glTF 2.0 file and saves it as the field model.
func importFieldModel(body io.Reader) error {
	var header [12]byte
	if _, err := io.ReadFull(body, header[:]); err != nil {
		return errors.New("that is not a .glb file (too short)")
	}
	if !bytes.Equal(header[:4], []byte("glTF")) || binary.LittleEndian.Uint32(header[4:8]) != 2 {
		return errors.New("that is not a binary glTF 2.0 (.glb) file; export one with tools/cad/export_field.py")
	}
	if err := os.MkdirAll(filepath.Dir(FieldModelPath), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(FieldModelPath), "field-cad-*.part")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name()) // no-op after the rename
	written, err := io.Copy(temp, io.MultiReader(bytes.NewReader(header[:]), body))
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("could not save the field model: %w", err)
	}
	if declared := int64(binary.LittleEndian.Uint32(header[8:12])); declared != written {
		return fmt.Errorf("the .glb file is incomplete (%d of %d bytes)", written, declared)
	}
	return os.Rename(temp.Name(), FieldModelPath)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
