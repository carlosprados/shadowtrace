package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestLoadNightFallbackAndParsing(t *testing.T) {
	v := viper.New()
	v.Set(KeyRSSIMin, -70)
	v.Set(KeyRSSIMinNight, 0) // 0 => fall back to RSSIMin
	v.Set(KeyMode, "WATCH")
	v.Set(KeyHomeMACs, "aa:bb:cc:dd:ee:ff, 11:22:33:44:55:66")
	v.Set(KeyBaselineFile, "~/x.json")

	c := Load(v)
	if c.RSSIMinNight != -70 {
		t.Errorf("night threshold should fall back to -70, got %d", c.RSSIMinNight)
	}
	if c.Mode != "watch" {
		t.Errorf("mode should be lowercased, got %q", c.Mode)
	}
	if len(c.HomeMACs) != 2 || c.HomeMACs[0] != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("home macs should be trimmed+uppercased, got %v", c.HomeMACs)
	}
	if strings.HasPrefix(c.BaselineFile, "~") {
		t.Errorf("~ should be expanded, got %q", c.BaselineFile)
	}
}

func TestTagPrefix(t *testing.T) {
	if (Config{AppName: "ShadowTrace"}).TagPrefix() != "ShadowTrace" {
		t.Error("bare app name expected")
	}
	if (Config{AppName: "ShadowTrace", LocationTag: "[Office]"}).TagPrefix() != "ShadowTrace [Office]" {
		t.Error("tag should be appended")
	}
}
