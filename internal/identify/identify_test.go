package identify

import (
	"testing"

	"github.com/carlosprados/shadowtrace/internal/model"
)

func ptr(i int) *int { return &i }

func TestFingerprintSurvivesMACRotation(t *testing.T) {
	a := Fingerprint(model.Observation{MAC: "AA:BB:CC:00:00:01", Name: "Pixel", Company: ptr(224), UUIDs: []string{"0000fe9f"}})
	b := Fingerprint(model.Observation{MAC: "FF:EE:DD:00:00:02", Name: "Pixel", Company: ptr(224), UUIDs: []string{"0000fe9f"}})
	if a != b {
		t.Fatalf("fingerprint changed across MAC rotation: %q vs %q", a, b)
	}
	if a[:3] != "fp:" {
		t.Fatalf("want fp: prefix, got %q", a)
	}
}

func TestFingerprintUnionOfUUIDsOrderIndependent(t *testing.T) {
	a := Fingerprint(model.Observation{MAC: "AA", UUIDs: []string{"b"}, ServiceUUIDs: []string{"a"}})
	b := Fingerprint(model.Observation{MAC: "AA", UUIDs: []string{"a"}, ServiceUUIDs: []string{"b"}})
	if a != b {
		t.Fatalf("uuid order/source should not matter: %q vs %q", a, b)
	}
}

func TestFingerprintMACShapedNameIgnored(t *testing.T) {
	fp := Fingerprint(model.Observation{MAC: "24:EB:90:67:E7:AA", Name: "24-EB-90-67-E7-AA", ServiceUUIDs: []string{"svc"}})
	if fp != "fp:u=svc" {
		t.Fatalf("want fp:u=svc, got %q", fp)
	}
}

func TestFingerprintFallsBackToMAC(t *testing.T) {
	if fp := Fingerprint(model.Observation{MAC: "AA:BB:CC:DD:EE:FF"}); fp != "mac:AA:BB:CC:DD:EE:FF" {
		t.Fatalf("want mac fallback, got %q", fp)
	}
}

func TestLooksLikeMAC(t *testing.T) {
	for _, s := range []string{"24-EB-90-67-E7-AA", "AA:BB:CC:DD:EE:FF"} {
		if !LooksLikeMAC(s) {
			t.Errorf("%q should look like MAC", s)
		}
	}
	if LooksLikeMAC("MP1_FDE349") {
		t.Error("MP1_FDE349 should not look like MAC")
	}
}

func TestIdentifyAppleContinuity(t *testing.T) {
	id := Identify(model.Observation{MAC: "7C:D9:F4:10:8F:E8", Company: ptr(76),
		ManufacturerData: map[int][]byte{76: {0x02, 0x15, 0xff}}}, nil)
	if id.Vendor != "Apple" {
		t.Errorf("want Apple vendor, got %q", id.Vendor)
	}
	if id.Note != "Apple iBeacon" {
		t.Errorf("want Apple iBeacon note, got %q", id.Note)
	}
}

func TestIdentifyEmbeddedString(t *testing.T) {
	id := Identify(model.Observation{MAC: "AA", Company: ptr(911),
		ServiceData: map[string][]byte{"0000fdaa": append([]byte{0x83, 0x0c}, []byte("xiaomi 15T")...)}}, nil)
	if id.Vendor != "Xiaomi" {
		t.Errorf("want Xiaomi, got %q", id.Vendor)
	}
	if id.Model != "xiaomi 15T" {
		t.Errorf("want embedded model, got %q", id.Model)
	}
}
