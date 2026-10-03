package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func useTempConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := configDir
	configDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { configDir = orig })
	return dir
}

func TestConfigProfilesLoadSave(t *testing.T) {
	useTempConfigDir(t)

	expiry := time.Now().Add(1 * time.Hour).Round(0)
	cfg := &Config{
		Profiles: []Profile{
			{
				Name:         "Standard",
				Groups:       []PomodoroGroup{{WorkMin: 25, BreakMin: 5}},
				LongBreakMin: 20,
			},
		},
		Sound:               "Ping",
		SpotifyURI:          "spotify:playlist:37i9dQZF1DX8Ueb2vPS3m6",
		SpotifyEnabled:      true,
		AutoStart:           true,
		SpotifyClientID:     "my_client_id",
		SpotifyClientSecret: "my_client_secret",
		SpotifyAccessToken:  "my_access_token",
		SpotifyRefreshToken: "my_refresh_token",
		SpotifyTokenExpiry:  expiry,
	}
	cfg.SetVolume(70)

	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	info, err := os.Stat(GetConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("config file permissions = %o, want 600", perm)
	}

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}
	if !loaded.AutoStart {
		t.Errorf("Expected AutoStart to be true, got false")
	}
	if loaded.Volume() != 70 {
		t.Errorf("Expected volume 70, got %d", loaded.Volume())
	}
	if loaded.SpotifyClientID != "my_client_id" || loaded.SpotifyClientSecret != "my_client_secret" {
		t.Errorf("Client ID or Secret mismatch")
	}
}

func TestLoadConfigMissingFileReturnsEmpty(t *testing.T) {
	useTempConfigDir(t)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg == nil || len(cfg.Profiles) != 0 {
		t.Fatalf("LoadConfig() = %+v, want empty config", cfg)
	}
}

func TestLoadConfigRejectsBrokenFiles(t *testing.T) {
	tests := map[string]string{
		"corrupt json":    `{"profiles": [`,
		"no groups":       `{"profiles": [{"name": "x", "groups": [], "long_break_min": 5}]}`,
		"zero work":       `{"profiles": [{"name": "x", "groups": [{"work_min": 0, "break_min": 5}], "long_break_min": 5}]}`,
		"negative break":  `{"profiles": [{"name": "x", "groups": [{"work_min": 5, "break_min": -1}], "long_break_min": 5}]}`,
		"empty name":      `{"profiles": [{"name": " ", "groups": [{"work_min": 5, "break_min": 5}], "long_break_min": 5}]}`,
		"volume too high": `{"spotify_volume_percent": 150}`,
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			useTempConfigDir(t)
			if err := os.WriteFile(GetConfigPath(), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadConfig(); err == nil {
				t.Fatal("LoadConfig() succeeded, want error")
			}
		})
	}
}

func TestLoadConfigMissingRedirectTargetIsError(t *testing.T) {
	dir := useTempConfigDir(t)
	missing := filepath.Join(dir, "elsewhere", "profiles.json")
	if err := os.WriteFile(filepath.Join(dir, "path.txt"), []byte(missing), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig() succeeded for a missing redirected config, want error")
	}
}

func TestSpotifyVolumeMigration(t *testing.T) {
	tests := []struct {
		content string
		want    int
	}{
		{`{"spotify_volume": 70}`, 70},
		{`{"spotify_volume": 0}`, defaultSpotifyVolume},
		{`{}`, defaultSpotifyVolume},
		{`{"spotify_volume_percent": 0}`, 0},
		{`{"spotify_volume": 30, "spotify_volume_percent": 80}`, 80},
	}
	for _, tt := range tests {
		useTempConfigDir(t)
		if err := os.WriteFile(GetConfigPath(), []byte(tt.content), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("%s: LoadConfig() error = %v", tt.content, err)
		}
		if got := cfg.Volume(); got != tt.want {
			t.Errorf("%s: Volume() = %d, want %d", tt.content, got, tt.want)
		}
	}
}

func TestSetConfigPathCopiesToNewFile(t *testing.T) {
	dir := useTempConfigDir(t)
	cfg := &Config{Sound: "Ping"}
	newPath := filepath.Join(dir, "sub", "custom.json")

	if err := SetConfigPath(newPath, cfg); err != nil {
		t.Fatalf("SetConfigPath() error = %v", err)
	}
	if got := GetConfigPath(); got != newPath {
		t.Fatalf("GetConfigPath() = %q, want %q", got, newPath)
	}
	loaded, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Sound != "Ping" {
		t.Errorf("Sound = %q, want Ping", loaded.Sound)
	}
}

func TestSetConfigPathNeverOverwritesForeignFile(t *testing.T) {
	dir := useTempConfigDir(t)
	target := filepath.Join(dir, "important.txt")
	original := []byte("do not touch")
	if err := os.WriteFile(target, original, 0644); err != nil {
		t.Fatal(err)
	}

	if err := SetConfigPath(target, &Config{}); err == nil {
		t.Fatal("SetConfigPath() succeeded, want error")
	}
	data, _ := os.ReadFile(target)
	if string(data) != string(original) {
		t.Errorf("target was modified: %q", data)
	}
	if GetConfigPath() == target {
		t.Error("config path was switched to the foreign file")
	}
}

func TestSetConfigPathUsesExistingConfig(t *testing.T) {
	dir := useTempConfigDir(t)
	target := filepath.Join(dir, "shared.json")
	if err := os.WriteFile(target, []byte(`{"sound": "Hero"}`), 0600); err != nil {
		t.Fatal(err)
	}

	if err := SetConfigPath(target, &Config{Sound: "Ping"}); err != nil {
		t.Fatalf("SetConfigPath() error = %v", err)
	}
	loaded, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Sound != "Hero" {
		t.Errorf("existing config was overwritten: Sound = %q, want Hero", loaded.Sound)
	}
}

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	got, err := expandPath("  ~/gomodoro/p.json ")
	if err != nil || got != filepath.Join(home, "gomodoro", "p.json") {
		t.Errorf("expandPath(~/...) = %q, %v", got, err)
	}
	if _, err := expandPath("relative/p.json"); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Errorf("expandPath(relative) error = %v, want absolute-path error", err)
	}
	if _, err := expandPath("   "); err == nil {
		t.Error("expandPath(empty) succeeded, want error")
	}
}
