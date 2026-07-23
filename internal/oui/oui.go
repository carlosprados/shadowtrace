// Package oui resolves a MAC address prefix to a hardware vendor using a locally
// cached IEEE/Wireshark "manuf" database, refreshed on demand or when it goes stale.
package oui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultURL is the Wireshark-maintained manuf database (OUI + long vendor names).
const DefaultURL = "https://www.wireshark.org/download/automated/data/manuf"

// DB maps a 3-octet OUI prefix ("AA:BB:CC") to a vendor name.
type DB struct{ m map[string]string }

// Vendor returns the vendor for a MAC, or "" if unknown. Nil-safe.
func (d *DB) Vendor(mac string) string {
	if d == nil || len(mac) < 8 {
		return ""
	}
	return d.m[strings.ToUpper(mac[:8])]
}

// Len reports how many OUI prefixes are loaded. Nil-safe.
func (d *DB) Len() int {
	if d == nil {
		return 0
	}
	return len(d.m)
}

func parse(r io.Reader) map[string]string {
	m := map[string]string{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			fields = strings.Fields(line)
		}
		if len(fields) < 2 {
			continue
		}
		prefix := strings.ToUpper(strings.TrimSpace(fields[0]))
		if slash := strings.IndexByte(prefix, '/'); slash >= 0 {
			prefix = prefix[:slash] // drop mask; keep only plain /24 OUIs below
		}
		if len(prefix) != 8 { // "AA:BB:CC" only; skip longer MA-M/MA-S allocations
			continue
		}
		vendor := strings.TrimSpace(fields[1])
		if len(fields) >= 3 {
			if long := strings.TrimSpace(fields[2]); long != "" {
				vendor = long
			}
		}
		if vendor != "" {
			m[prefix] = vendor
		}
	}
	return m
}

// Load reads a cached database. A missing file yields an empty (usable) DB.
func Load(path string) (*DB, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &DB{m: map[string]string{}}, nil
		}
		return nil, err
	}
	defer f.Close()
	return &DB{m: parse(f)}, nil
}

// Update downloads the database to path (atomic write).
func Update(ctx context.Context, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if d := filepath.Dir(path); d != "" {
		_ = os.MkdirAll(d, 0o755)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Age returns how old the cached file is, and whether it exists.
func Age(path string) (time.Duration, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	return time.Since(fi.ModTime()), true
}

// EnsureFresh loads the DB, downloading first when the cache is missing, older
// than maxAge, or force is set. A failed download is non-fatal: it falls back to
// whatever is cached (possibly empty), so identification degrades gracefully.
func EnsureFresh(ctx context.Context, path, url string, maxAge time.Duration, force bool) (*DB, error) {
	age, exists := Age(path)
	stale := force || !exists || (maxAge > 0 && age > maxAge)
	if stale {
		if err := Update(ctx, url, path); err != nil {
			fmt.Println("[WARN] OUI update failed, using cached/empty:", err)
		}
	}
	return Load(path)
}
