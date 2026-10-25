package extensions

import (
	"encoding/json"
	"fmt"
	"io"
	"just-nas/internal/config"
	"net"
	"net/http"
	"os"
	"strings"
)

const NativeStatesFile = "native-apps.json"
const NativeStateSocket = "/run/just-nas/native-state.sock"

type NativeStateRecord struct {
	Version        int             `json:"version"`
	DataReferences []string        `json:"data_references"`
	State          json.RawMessage `json:"state"`
}

func StartNativeStateService(save func(string, json.RawMessage) error) error {
	if e := os.MkdirAll("/run/just-nas", 0700); e != nil {
		return e
	}
	os.Remove(NativeStateSocket)
	l, e := net.Listen("unix", NativeStateSocket)
	if e != nil {
		return e
	}
	if e = os.Chmod(NativeStateSocket, 0600); e != nil {
		l.Close()
		return e
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/state/", func(w http.ResponseWriter, q *http.Request) {
		id := strings.TrimPrefix(q.URL.Path, "/state/")
		if id != "vm-manager" {
			http.NotFound(w, q)
			return
		}
		if q.Method == "GET" {
			var states map[string]json.RawMessage
			if e := config.ReadJSON(NativeStatesFile, &states); e != nil && !os.IsNotExist(e) {
				http.Error(w, e.Error(), 409)
				return
			}
			raw := states[id]
			if len(raw) == 0 {
				http.NotFound(w, q)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			var record NativeStateRecord
			if json.Unmarshal(raw, &record) != nil || record.Version != 1 {
				http.Error(w, "原生設定版本無效", 409)
				return
			}
			w.Write(record.State)
			return
		}
		if q.Method != "PUT" {
			w.WriteHeader(405)
			return
		}
		raw, e := io.ReadAll(http.MaxBytesReader(w, q.Body, 4<<20))
		if e == nil && !json.Valid(raw) {
			e = fmt.Errorf("原生應用設定不是有效 JSON")
		}
		if e == nil {
			e = save(id, raw)
		}
		if e != nil {
			http.Error(w, e.Error(), 409)
			return
		}
		w.Write([]byte("{}"))
	})
	go http.Serve(l, mux)
	return nil
}
func SaveNativeState(id string, raw json.RawMessage) error {
	var record NativeStateRecord
	if e := strict(raw, &record); e != nil {
		return e
	}
	if record.Version != 1 || !json.Valid(record.State) || len(record.DataReferences) > 16 {
		return fmt.Errorf("原生設定無效")
	}
	for _, ds := range record.DataReferences {
		parts := strings.Split(ds, "/")
		if len(parts) < 2 || !poolIdentifier.MatchString(parts[0]) || strings.ContainsAny(ds, "@\\\x00") || strings.Contains(ds, "..") {
			return fmt.Errorf("原生資料集參照無效")
		}
	}
	states := map[string]json.RawMessage{}
	if e := config.ReadJSON(NativeStatesFile, &states); e != nil && !os.IsNotExist(e) {
		return e
	}
	states[id] = raw
	return persist(NativeStatesFile, states)
}
func NativeReferences(id string) []string {
	var states map[string]NativeStateRecord
	if config.ReadJSON(NativeStatesFile, &states) != nil {
		return nil
	}
	return states[id].DataReferences
}

func nativeStateBackup(id string) (json.RawMessage, error) {
	var states map[string]json.RawMessage
	if e := config.ReadJSON(NativeStatesFile, &states); e != nil && !os.IsNotExist(e) {
		return nil, e
	}
	return states[id], nil
}
