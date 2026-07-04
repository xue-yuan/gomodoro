package main

import (
	"os"
	"testing"
	"time"
)

func TestConfigProfilesLoadSave(t *testing.T) {
	path := GetConfigPath()
	var backupData []byte
	backupExists := false
	if _, err := os.Stat(path); err == nil {
		backupData, _ = os.ReadFile(path)
		backupExists = true
		_ = os.Remove(path)
	}

	defer func() {
		if backupExists {
			_ = os.WriteFile(path, backupData, 0644)
		} else {
			_ = os.Remove(path)
		}
	}()

	expiry := time.Now().Add(1 * time.Hour)

	cfg := &Config{
		Profiles: []Profile{
			{
				Name: "Standard",
				Groups: []PomodoroGroup{
					{WorkMin: 25, BreakMin: 5},
				},
				LongBreakMin: 20,
			},
		},
		Sound:               "Ping",
		SpotifyURI:          "spotify:playlist:37i9dQZF1DX8Ueb2vPS3m6",
		SpotifyEnabled:      true,
		SpotifyVolume:       70,
		AutoStart:           true,
		SpotifyClientID:     "my_client_id",
		SpotifyClientSecret: "my_client_secret",
		SpotifyAccessToken:  "my_access_token",
		SpotifyRefreshToken: "my_refresh_token",
		SpotifyTokenExpiry:  expiry,
	}

	err := SaveConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if !loaded.AutoStart {
		t.Errorf("Expected AutoStart to be true, got false")
	}

	if loaded.SpotifyVolume != 70 {
		t.Errorf("Expected SpotifyVolume to be 70, got %d", loaded.SpotifyVolume)
	}

	if loaded.SpotifyClientID != "my_client_id" || loaded.SpotifyClientSecret != "my_client_secret" {
		t.Errorf("Client ID or Secret mismatch")
	}
}
