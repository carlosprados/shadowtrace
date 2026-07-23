package scan

import "testing"

func TestFindAdapterPathPrefers(t *testing.T) {
	paths := []string{"/org/bluez/hci0", "/org/bluez/hci1"}
	got, ok := findAdapterPath(paths, "hci1")
	if !ok || got != "/org/bluez/hci1" {
		t.Fatalf("want hci1, got %q ok=%v", got, ok)
	}
}

func TestFindAdapterPathFallback(t *testing.T) {
	got, ok := findAdapterPath([]string{"/org/bluez/hci0"}, "hci9")
	if !ok || got != "/org/bluez/hci0" {
		t.Fatalf("want fallback hci0, got %q ok=%v", got, ok)
	}
}

func TestFindAdapterPathEmpty(t *testing.T) {
	if _, ok := findAdapterPath(nil, ""); ok {
		t.Fatal("expected not found for empty set")
	}
}
