// Package store persists the learned baseline, the forensic event log (JSONL)
// and the legacy presence state, with atomic writes.
package store

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
)

func ensureDir(path string) error {
	d := filepath.Dir(path)
	if d == "" {
		return nil
	}
	return os.MkdirAll(d, 0o755)
}

func writeAtomic(path string, data []byte) error {
	if err := ensureDir(path); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ---------------- Baseline ----------------

// BaselineEntry is one learned device, keyed by fingerprint.
type BaselineEntry struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Vendor    string   `json:"vendor,omitempty"`
	Kind      string   `json:"kind,omitempty"`
	Model     string   `json:"model,omitempty"`
	MACs      []string `json:"macs"`
	FirstSeen string   `json:"first_seen"`
	LastSeen  string   `json:"last_seen"`
	Count     int      `json:"count"`
}

// Baseline is the hand-editable allowlist of habitual devices.
type Baseline struct {
	Meta         map[string]string         `json:"_meta"`
	Fingerprints map[string]*BaselineEntry `json:"fingerprints"`
}

func LoadBaseline(path string) *Baseline {
	b := &Baseline{Meta: map[string]string{}, Fingerprints: map[string]*BaselineEntry{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return b
	}
	_ = json.Unmarshal(data, b)
	if b.Meta == nil {
		b.Meta = map[string]string{}
	}
	if b.Fingerprints == nil {
		b.Fingerprints = map[string]*BaselineEntry{}
	}
	return b
}

func SaveBaseline(path string, b *Baseline) error {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, data)
}

// ---------------- Event log ----------------

// Event is one forensic record. Pointers omit when absent so appear and leave
// events serialise cleanly. Keep field names stable — the trainer reads them.
type Event struct {
	TS       string `json:"ts"`
	Event    string `json:"event"` // appear | leave
	FP       string `json:"fp"`
	Name     string `json:"name"`
	MAC      string `json:"mac"`
	Type     string `json:"type"`
	Vendor   string `json:"vendor,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Model    string `json:"model,omitempty"`
	RSSI     *int   `json:"rssi,omitempty"`
	RSSIMax  *int   `json:"rssi_max,omitempty"`
	RSSIMin  *int   `json:"rssi_min,omitempty"`
	Duration *int   `json:"duration_s,omitempty"`
	Known    bool   `json:"known"`
	Learning *bool  `json:"learning,omitempty"`
}

// AppendEvent appends one JSON line to the log.
func AppendEvent(path string, ev Event) error {
	if err := ensureDir(path); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	return err
}

// LoadEvents reads the whole JSONL log (skips malformed lines).
func LoadEvents(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev Event
		if json.Unmarshal(line, &ev) == nil {
			out = append(out, ev)
		}
	}
	return out, sc.Err()
}

// ---------------- Presence state ----------------

type StateEntry struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	RSSI     *int   `json:"rssi"`
	LastSeen string `json:"last_seen"`
	Status   string `json:"status"`
}

func LoadState(path string) map[string]*StateEntry {
	st := map[string]*StateEntry{}
	data, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	_ = json.Unmarshal(data, &st)
	return st
}

func SaveState(path string, st map[string]*StateEntry) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, data)
}
