package oui

import (
	"strings"
	"testing"
)

func TestParseAndLookup(t *testing.T) {
	in := strings.Join([]string{
		"# a comment line",
		"00:00:0C\tCisco\tCisco Systems, Inc",   // long name preferred
		"AA:BB:CC\tAcme",                        // short name only
		"00:50:C2:00:00:00/36\tIeee\tIEEE MA-S", // longer mask -> skipped
		"",
	}, "\n")
	db := &DB{m: parse(strings.NewReader(in))}

	if got := db.Vendor("00:00:0c:11:22:33"); got != "Cisco Systems, Inc" {
		t.Errorf("want long Cisco name, got %q", got)
	}
	if got := db.Vendor("AA:BB:CC:DD:EE:FF"); got != "Acme" {
		t.Errorf("want Acme, got %q", got)
	}
	if got := db.Vendor("00:50:C2:00:00:01"); got != "" {
		t.Errorf("masked MA-S entry should be skipped, got %q", got)
	}
	if db.Len() != 2 {
		t.Errorf("want 2 prefixes, got %d", db.Len())
	}
}

func TestVendorNilSafe(t *testing.T) {
	var db *DB
	if db.Vendor("AA:BB:CC:DD:EE:FF") != "" || db.Len() != 0 {
		t.Error("nil DB must be safe and empty")
	}
}
