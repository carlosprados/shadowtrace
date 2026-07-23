// Package model holds the shared data types passed between the scanner, the
// identification layer, the store and the watch/presence loops.
package model

// Observation is one device seen in a single scan window. Optional fields use
// pointers so "absent" is distinct from "zero".
type Observation struct {
	MAC              string
	Name             string
	Type             string // BLE | Classic | Unknown
	AddressType      string // public | random
	RSSI             *int
	Connected        bool
	Company          *int // Bluetooth SIG company id from manufacturer data
	UUIDs            []string
	ServiceUUIDs     []string // UUIDs that carried service data
	Appearance       *int
	TxPower          *int
	Icon             string // BlueZ freedesktop icon hint (e.g. audio-headset)
	ServiceData      map[string][]byte
	ManufacturerData map[int][]byte
}

// Identity is best-effort, human- and AI-readable identification of a device,
// derived from passive advertisement data (never by connecting).
type Identity struct {
	Vendor string `json:"vendor,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Model  string `json:"model,omitempty"`
	Note   string `json:"note,omitempty"`
}
