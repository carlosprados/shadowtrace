// Package config centralises runtime configuration. Values come from CLI flags,
// then environment variables (legacy ShadowTrace names), then a config file,
// resolved by Viper. Config is decoupled from Cobra: it just reads a *viper.Viper.
package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Config is the fully-resolved runtime configuration.
type Config struct {
	AppName     string
	LocationTag string
	Mode        string // watch | presence

	TelegramToken  string
	TelegramChatID string

	Adapter         string // e.g. hci1; empty = first available
	Transport       string // auto | le | bredr
	Continuous      bool
	WindowSeconds   int
	IntervalSeconds int

	// watch mode
	RSSIMin       int
	RSSIMinNight  int
	ConfirmHits   int
	GoneAfter     int
	LearnSeconds  int
	AlertCooldown int
	AlertHours    string
	HomeMACs      []string
	BaselineFile  string
	EventLog      string
	ModelFile     string

	// OUI vendor database
	OUIFile       string
	OUIURL        string
	OUIMaxAgeDays int
	OUIAuto       bool

	// presence mode
	PresenceGoneAfter int
	StateFile         string
	NameWhitelist     []string
	IgnoreMACs        []string
	WifiHosts         []string
	MDNS              bool
	ARP               bool
	ARPSubnets        []string
	ARPSweep          bool
	ARPSweepLimit     int
	ARPTimeoutMS      int

	Debug bool
}

// Keys are the canonical Viper/flag keys. Bindings to legacy env names live in
// the cmd package so `shadowtrace.env` keeps working unchanged.
const (
	KeyAppName       = "app-name"
	KeyLocationTag   = "location-tag"
	KeyMode          = "mode"
	KeyTgToken       = "telegram-token"
	KeyTgChat        = "telegram-chat-id"
	KeyAdapter       = "adapter"
	KeyTransport     = "transport"
	KeyContinuous    = "continuous"
	KeyWindow        = "window"
	KeyInterval      = "interval"
	KeyRSSIMin       = "rssi-min"
	KeyRSSIMinNight  = "rssi-min-night"
	KeyConfirmHits   = "confirm-hits"
	KeyGoneAfter     = "gone-after"
	KeyLearnSeconds  = "learn-seconds"
	KeyAlertCooldown = "alert-cooldown"
	KeyAlertHours    = "alert-hours"
	KeyHomeMACs      = "home-macs"
	KeyBaselineFile  = "baseline-file"
	KeyEventLog      = "event-log"
	KeyModelFile     = "model-file"
	KeyOUIFile       = "oui-file"
	KeyOUIURL        = "oui-url"
	KeyOUIMaxAge     = "oui-max-age-days"
	KeyOUIAuto       = "oui-auto"
	KeyPresGoneAfter = "presence-gone-after"
	KeyStateFile     = "state-file"
	KeyNameAllowlist = "name-whitelist"
	KeyIgnoreMACs    = "ignore-macs"
	KeyWifiHosts     = "wifi-hosts"
	KeyMDNS          = "mdns"
	KeyARP           = "arp"
	KeyARPSubnets    = "arp-subnets"
	KeyARPSweep      = "arp-sweep"
	KeyARPSweepLimit = "arp-sweep-limit"
	KeyARPTimeoutMS  = "arp-timeout-ms"
	KeyDebug         = "debug"
)

func expand(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func splitList(s string) []string {
	out := []string{}
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func upperList(s string) []string {
	out := splitList(s)
	for i := range out {
		out[i] = strings.ToUpper(out[i])
	}
	return out
}

// Load resolves the configuration from a Viper instance.
func Load(v *viper.Viper) Config {
	c := Config{
		AppName:         v.GetString(KeyAppName),
		LocationTag:     strings.TrimSpace(v.GetString(KeyLocationTag)),
		Mode:            strings.ToLower(strings.TrimSpace(v.GetString(KeyMode))),
		TelegramToken:   strings.TrimSpace(v.GetString(KeyTgToken)),
		TelegramChatID:  strings.TrimSpace(v.GetString(KeyTgChat)),
		Adapter:         strings.TrimSpace(v.GetString(KeyAdapter)),
		Transport:       strings.ToLower(v.GetString(KeyTransport)),
		Continuous:      v.GetBool(KeyContinuous),
		WindowSeconds:   v.GetInt(KeyWindow),
		IntervalSeconds: v.GetInt(KeyInterval),

		RSSIMin:       v.GetInt(KeyRSSIMin),
		RSSIMinNight:  v.GetInt(KeyRSSIMinNight),
		ConfirmHits:   v.GetInt(KeyConfirmHits),
		GoneAfter:     v.GetInt(KeyGoneAfter),
		LearnSeconds:  v.GetInt(KeyLearnSeconds),
		AlertCooldown: v.GetInt(KeyAlertCooldown),
		AlertHours:    strings.TrimSpace(v.GetString(KeyAlertHours)),
		HomeMACs:      upperList(v.GetString(KeyHomeMACs)),
		BaselineFile:  expand(v.GetString(KeyBaselineFile)),
		EventLog:      expand(v.GetString(KeyEventLog)),
		ModelFile:     expand(v.GetString(KeyModelFile)),

		OUIFile:       expand(v.GetString(KeyOUIFile)),
		OUIURL:        v.GetString(KeyOUIURL),
		OUIMaxAgeDays: v.GetInt(KeyOUIMaxAge),
		OUIAuto:       v.GetBool(KeyOUIAuto),

		PresenceGoneAfter: v.GetInt(KeyPresGoneAfter),
		StateFile:         expand(v.GetString(KeyStateFile)),
		NameWhitelist:     splitList(v.GetString(KeyNameAllowlist)),
		IgnoreMACs:        upperList(v.GetString(KeyIgnoreMACs)),
		WifiHosts:         splitList(v.GetString(KeyWifiHosts)),
		MDNS:              v.GetBool(KeyMDNS),
		ARP:               v.GetBool(KeyARP),
		ARPSubnets:        splitList(v.GetString(KeyARPSubnets)),
		ARPSweep:          v.GetBool(KeyARPSweep),
		ARPSweepLimit:     v.GetInt(KeyARPSweepLimit),
		ARPTimeoutMS:      v.GetInt(KeyARPTimeoutMS),

		Debug: v.GetBool(KeyDebug),
	}
	if c.RSSIMinNight == 0 {
		c.RSSIMinNight = c.RSSIMin
	}
	return c
}

// TagPrefix is the "AppName [Tag]" prefix used in alerts.
func (c Config) TagPrefix() string {
	if c.LocationTag != "" {
		return c.AppName + " " + c.LocationTag
	}
	return c.AppName
}
