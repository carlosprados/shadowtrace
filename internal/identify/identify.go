// Package identify turns raw advertisement data into (a) a stable-ish fingerprint
// used as the tracking/baseline key, and (b) best-effort human/AI-readable
// identification (vendor, kind, model) — all passively, without connecting.
package identify

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/carlosprados/shadowtrace/internal/model"
)

var macRe = regexp.MustCompile(`^([0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}$`)

// LooksLikeMAC reports whether an advertised name is really just a MAC address
// (some devices do this). Such names rotate with the MAC and are useless as identity.
func LooksLikeMAC(name string) bool {
	return name != "" && macRe.MatchString(strings.TrimSpace(name))
}

// Fingerprint is a MAC-rotation-resistant identity built from the name (unless
// it is a MAC), the manufacturer company id, and the union of service UUIDs.
// Falls back to "mac:<addr>" when the device exposes nothing stable.
func Fingerprint(o model.Observation) string {
	parts := []string{}
	if o.Name != "" && !LooksLikeMAC(o.Name) {
		parts = append(parts, "n="+o.Name)
	}
	if o.Company != nil {
		parts = append(parts, fmt.Sprintf("c=%d", *o.Company))
	}
	uuids := map[string]struct{}{}
	for _, u := range o.UUIDs {
		uuids[strings.ToLower(u)] = struct{}{}
	}
	for _, u := range o.ServiceUUIDs {
		uuids[strings.ToLower(u)] = struct{}{}
	}
	if len(uuids) > 0 {
		list := make([]string, 0, len(uuids))
		for u := range uuids {
			list = append(list, u)
		}
		sort.Strings(list)
		parts = append(parts, "u="+strings.Join(list, ","))
	}
	if len(parts) == 0 {
		return "mac:" + o.MAC
	}
	return "fp:" + strings.Join(parts, "|")
}

// Bluetooth SIG company identifiers (common subset).
var companies = map[int]string{
	2:    "Intel",
	6:    "Microsoft",
	15:   "Broadcom",
	76:   "Apple",
	89:   "Nordic",
	117:  "Samsung",
	135:  "Garmin",
	224:  "Google",
	301:  "Sony",
	911:  "Xiaomi",
	1177: "Amazon",
}

// VendorFunc resolves a MAC to a vendor via an external OUI database (see
// internal/oui). It is injected so this package stays pure and testable.
type VendorFunc func(mac string) string

// ouisFallback is a tiny embedded subset used only when the external OUI database
// is unavailable (offline / not yet downloaded).
var ouisFallback = map[string]string{
	"00:25:D1": "Garmin",
	"F4:95:CE": "Garmin",
	"00:11:32": "Synology",
	"7C:D9:F4": "Apple",
}

// Apple Continuity message types (manufacturer data 0x004C, first byte).
var appleTypes = map[byte]string{
	0x02: "iBeacon",
	0x05: "AirDrop",
	0x07: "Proximity Pairing (AirPods)",
	0x08: "Hey Siri",
	0x09: "AirPlay",
	0x0B: "Watch",
	0x0C: "Handoff",
	0x0D: "Instant Hotspot",
	0x0F: "Nearby Action",
	0x10: "Nearby Info",
}

func vendorFromOUI(mac string) string {
	if len(mac) < 8 {
		return ""
	}
	return ouisFallback[strings.ToUpper(mac[:8])]
}

// printableRun extracts the longest printable-ASCII run (>=3 chars) from a byte
// slice — often a device/model name embedded in service data (e.g. "xiaomi 15T").
func printableRun(b []byte) string {
	best, cur := "", strings.Builder{}
	flush := func() {
		if cur.Len() >= 3 && cur.Len() > len(best) {
			best = cur.String()
		}
		cur.Reset()
	}
	for _, c := range b {
		if c >= 0x20 && c < 0x7f && unicode.IsPrint(rune(c)) {
			cur.WriteByte(c)
		} else {
			flush()
		}
	}
	flush()
	return best
}

// Identify derives best-effort vendor/kind/model from passive data. oui may be nil
// (then only the SIG company id and a tiny embedded OUI fallback are used).
func Identify(o model.Observation, oui VendorFunc) model.Identity {
	id := model.Identity{}

	// Vendor: company id first (most reliable for BLE), then OUI for public MACs.
	if o.Company != nil {
		if name, ok := companies[*o.Company]; ok {
			id.Vendor = name
		} else {
			id.Vendor = fmt.Sprintf("company:%d", *o.Company)
		}
	}
	if id.Vendor == "" && strings.EqualFold(o.AddressType, "public") {
		if oui != nil {
			id.Vendor = oui(o.MAC)
		}
		if id.Vendor == "" {
			id.Vendor = vendorFromOUI(o.MAC)
		}
	}

	// Kind: BlueZ icon hint, else GAP appearance category.
	if o.Icon != "" {
		id.Kind = o.Icon
	} else if o.Appearance != nil {
		id.Kind = appearanceKind(*o.Appearance)
	}

	// Model / note: Apple continuity, Fast Pair model id, or an embedded string.
	if o.ManufacturerData != nil {
		if payload, ok := o.ManufacturerData[76]; ok && len(payload) > 0 {
			if t, ok := appleTypes[payload[0]]; ok {
				id.Note = "Apple " + t
			}
		}
	}
	for uuid, data := range o.ServiceData {
		lu := strings.ToLower(uuid)
		if strings.HasPrefix(lu, "0000fe2c") && len(data) >= 3 { // Google Fast Pair
			id.Model = "FastPair:" + fmt.Sprintf("%02x%02x%02x", data[0], data[1], data[2])
		}
		if id.Model == "" {
			if s := printableRun(data); s != "" {
				id.Model = s
			}
		}
	}
	return id
}

func appearanceKind(a int) string {
	switch a {
	case 961:
		return "keyboard"
	case 962:
		return "input-mouse"
	}
	switch a >> 6 { // category = appearance / 64
	case 1:
		return "phone"
	case 2:
		return "computer"
	case 3:
		return "watch"
	case 5:
		return "display"
	case 15:
		return "hid"
	}
	return fmt.Sprintf("appearance:%d", a)
}
