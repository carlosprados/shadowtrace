// Package scan performs Bluetooth (BLE + Classic) discovery via BlueZ over the
// system D-Bus, plus the legacy Wi-Fi/mDNS/ARP transports (see net.go).
package scan

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/carlosprados/shadowtrace/internal/config"
	"github.com/carlosprados/shadowtrace/internal/model"
	"github.com/godbus/dbus/v5"
)

const (
	bluez     = "org.bluez"
	adapterIf = "org.bluez.Adapter1"
	deviceIf  = "org.bluez.Device1"
	omIf      = "org.freedesktop.DBus.ObjectManager"
	propsIf   = "org.freedesktop.DBus.Properties"
)

type managedObjects map[dbus.ObjectPath]map[string]map[string]dbus.Variant

// Scanner owns the D-Bus connection and the selected adapter.
type Scanner struct {
	conn        *dbus.Conn
	adapterPath dbus.ObjectPath
	cfg         config.Config
}

// NewScanner connects, selects the adapter (cfg.Adapter or first available) and
// powers it on.
func NewScanner(cfg config.Config) (*Scanner, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("connect system bus: %w", err)
	}
	objs, err := getManagedObjects(conn)
	if err != nil {
		return nil, err
	}
	path, err := findAdapter(objs, cfg.Adapter)
	if err != nil {
		return nil, err
	}
	s := &Scanner{conn: conn, adapterPath: path, cfg: cfg}
	if err := s.ensurePowered(); err != nil {
		return nil, err
	}
	return s, nil
}

// AdapterName returns the trailing element of the adapter path (e.g. "hci1").
func (s *Scanner) AdapterName() string {
	parts := strings.Split(string(s.adapterPath), "/")
	return parts[len(parts)-1]
}

func (s *Scanner) Close() error { return s.conn.Close() }

func getManagedObjects(conn *dbus.Conn) (managedObjects, error) {
	obj := conn.Object(bluez, "/")
	var objs managedObjects
	if err := obj.Call(omIf+".GetManagedObjects", 0).Store(&objs); err != nil {
		return nil, fmt.Errorf("GetManagedObjects: %w", err)
	}
	return objs, nil
}

// findAdapterPath picks an adapter path, preferring one whose tail matches prefer.
func findAdapterPath(paths []string, prefer string) (string, bool) {
	if len(paths) == 0 {
		return "", false
	}
	if prefer != "" {
		for _, p := range paths {
			if strings.HasSuffix(strings.TrimRight(p, "/"), prefer) {
				return p, true
			}
		}
	}
	sort.Strings(paths)
	return paths[0], true
}

func findAdapter(objs managedObjects, prefer string) (dbus.ObjectPath, error) {
	var paths []string
	for p, ifaces := range objs {
		if _, ok := ifaces[adapterIf]; ok {
			paths = append(paths, string(p))
		}
	}
	got, ok := findAdapterPath(paths, prefer)
	if !ok {
		return "", fmt.Errorf("no BlueZ adapter (hciX) found")
	}
	return dbus.ObjectPath(got), nil
}

func (s *Scanner) adapter() dbus.BusObject { return s.conn.Object(bluez, s.adapterPath) }

func (s *Scanner) getProp(iface, name string) (dbus.Variant, error) {
	var v dbus.Variant
	err := s.adapter().Call(propsIf+".Get", 0, iface, name).Store(&v)
	return v, err
}

func (s *Scanner) ensurePowered() error {
	v, err := s.getProp(adapterIf, "Powered")
	if err == nil {
		if on, ok := v.Value().(bool); ok && on {
			return nil
		}
	}
	return s.adapter().Call(propsIf+".Set", 0, adapterIf, "Powered", dbus.MakeVariant(true)).Err
}

func (s *Scanner) isDiscovering() bool {
	v, err := s.getProp(adapterIf, "Discovering")
	if err != nil {
		return false
	}
	on, _ := v.Value().(bool)
	return on
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func (s *Scanner) runWindow(ctx context.Context) {
	filter := map[string]dbus.Variant{
		"Transport":     dbus.MakeVariant(s.cfg.Transport),
		"DuplicateData": dbus.MakeVariant(true),
	}
	if err := s.adapter().Call(adapterIf+".SetDiscoveryFilter", 0, filter).Err; err != nil {
		fmt.Println("[WARN] SetDiscoveryFilter failed:", err)
	}
	window := time.Duration(s.cfg.WindowSeconds) * time.Second
	if s.cfg.Continuous {
		if !s.isDiscovering() {
			_ = s.adapter().Call(adapterIf+".StartDiscovery", 0).Err
		}
		sleepCtx(ctx, window)
		return
	}
	_ = s.adapter().Call(adapterIf+".StartDiscovery", 0).Err
	sleepCtx(ctx, window)
	_ = s.adapter().Call(adapterIf+".StopDiscovery", 0).Err
}

// ScanOnce runs one discovery window and returns every device that reported RSSI
// this window or is currently connected.
func (s *Scanner) ScanOnce(ctx context.Context) ([]model.Observation, error) {
	s.runWindow(ctx)
	objs, err := getManagedObjects(s.conn)
	if err != nil {
		return nil, err
	}
	var out []model.Observation
	for _, ifaces := range objs {
		dev, ok := ifaces[deviceIf]
		if !ok {
			continue
		}
		mac := strings.ToUpper(vString(dev, "Address"))
		if mac == "" {
			continue
		}
		rssi := vInt(dev, "RSSI")
		connected := vBool(dev, "Connected")
		if rssi == nil && !connected {
			continue
		}
		name := vString(dev, "Name")
		if name == "" {
			name = vString(dev, "Alias")
		}
		sd := vServiceData(dev)
		md := vManufacturerData(dev)
		out = append(out, model.Observation{
			MAC:              mac,
			Name:             name,
			Type:             inferType(dev),
			AddressType:      vString(dev, "AddressType"),
			RSSI:             rssi,
			Connected:        connected,
			Company:          companyOf(md),
			UUIDs:            vStrings(dev, "UUIDs"),
			ServiceUUIDs:     keysOf(sd),
			Appearance:       vInt(dev, "Appearance"),
			TxPower:          vInt(dev, "TxPower"),
			Icon:             vString(dev, "Icon"),
			ServiceData:      sd,
			ManufacturerData: md,
		})
	}
	return out, nil
}

func inferType(dev map[string]dbus.Variant) string {
	if _, ok := dev["AddressType"]; ok {
		return "BLE"
	}
	if _, ok := dev["Class"]; ok {
		return "Classic"
	}
	return "Unknown"
}
