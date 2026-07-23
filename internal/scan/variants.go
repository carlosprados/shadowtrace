package scan

import (
	"sort"

	"github.com/godbus/dbus/v5"
)

func vString(m map[string]dbus.Variant, k string) string {
	if v, ok := m[k]; ok {
		if s, ok := v.Value().(string); ok {
			return s
		}
	}
	return ""
}

func vBool(m map[string]dbus.Variant, k string) bool {
	if v, ok := m[k]; ok {
		if b, ok := v.Value().(bool); ok {
			return b
		}
	}
	return false
}

// vInt returns a signed int for numeric variants (int16/uint16/int32/...), or nil.
func vInt(m map[string]dbus.Variant, k string) *int {
	v, ok := m[k]
	if !ok {
		return nil
	}
	var n int
	switch x := v.Value().(type) {
	case int16:
		n = int(x)
	case uint16:
		n = int(x)
	case int32:
		n = int(x)
	case uint32:
		n = int(x)
	case int8:
		n = int(x)
	case uint8:
		n = int(x)
	case int64:
		n = int(x)
	default:
		return nil
	}
	return &n
}

func vStrings(m map[string]dbus.Variant, k string) []string {
	if v, ok := m[k]; ok {
		if ss, ok := v.Value().([]string); ok {
			return ss
		}
	}
	return nil
}

// vServiceData reads a{sv} where each value is a byte array.
func vServiceData(m map[string]dbus.Variant) map[string][]byte {
	v, ok := m["ServiceData"]
	if !ok {
		return nil
	}
	raw, ok := v.Value().(map[string]dbus.Variant)
	if !ok {
		return nil
	}
	out := map[string][]byte{}
	for uuid, val := range raw {
		if b, ok := val.Value().([]byte); ok {
			out[uuid] = b
		}
	}
	return out
}

// vManufacturerData reads a{qv} where each value is a byte array.
func vManufacturerData(m map[string]dbus.Variant) map[int][]byte {
	v, ok := m["ManufacturerData"]
	if !ok {
		return nil
	}
	raw, ok := v.Value().(map[uint16]dbus.Variant)
	if !ok {
		return nil
	}
	out := map[int][]byte{}
	for company, val := range raw {
		if b, ok := val.Value().([]byte); ok {
			out[int(company)] = b
		}
	}
	return out
}

func companyOf(md map[int][]byte) *int {
	if len(md) == 0 {
		return nil
	}
	keys := make([]int, 0, len(md))
	for k := range md {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	c := keys[0]
	return &c
}

func keysOf(sd map[string][]byte) []string {
	if len(sd) == 0 {
		return nil
	}
	out := make([]string, 0, len(sd))
	for k := range sd {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
