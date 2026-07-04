package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type PomodoroGroup struct {
	WorkMin  int `json:"work_min"`
	BreakMin int `json:"break_min"`
}

type Profile struct {
	Name         string          `json:"name"`
	Groups       []PomodoroGroup `json:"groups"`
	LongBreakMin int             `json:"long_break_min"`
}

type Config struct {
	Profiles       []Profile `json:"profiles"`
	Sound          string    `json:"sound"`
	SpotifyURI     string    `json:"spotify_uri"`
	SpotifyEnabled bool      `json:"spotify_enabled"`
	SpotifyVolume  int       `json:"spotify_volume"`
	AutoStart      bool      `json:"auto_start"`

	SpotifyClientID     string    `json:"spotify_client_id"`
	SpotifyClientSecret string    `json:"spotify_client_secret"`
	SpotifyAccessToken  string    `json:"spotify_access_token"`
	SpotifyRefreshToken string    `json:"spotify_refresh_token"`
	SpotifyTokenExpiry  time.Time `json:"spotify_token_expiry"`
}

func GetConfigPath() string {
	home, err := os.UserHomeDir()
	if err == nil {
		redirectionFile := filepath.Join(home, ".config", "gomodoro", "path.txt")
		if data, err := os.ReadFile(redirectionFile); err == nil {
			path := strings.TrimSpace(string(data))
			if path != "" {
				return path
			}
		}
	}

	if err == nil {
		dir := filepath.Join(home, ".config", "gomodoro")
		_ = os.MkdirAll(dir, 0755)
		return filepath.Join(dir, "profiles.json")
	}
	return ".gomodoro_profiles.json"
}

func SetConfigPath(newPath string) error {
	newPath = strings.TrimSpace(newPath)
	if newPath == "" {
		return fmt.Errorf("path cannot be empty")
	}

	currentPath := GetConfigPath()
	if currentPath == newPath {
		return nil
	}

	data, err := os.ReadFile(currentPath)
	if err != nil {
		data = []byte("{}")
	}

	newDir := filepath.Dir(newPath)
	err = os.MkdirAll(newDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}

	err = os.WriteFile(newPath, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write config to new location: %v", err)
	}

	home, err := os.UserHomeDir()
	if err == nil {
		redirDir := filepath.Join(home, ".config", "gomodoro")
		_ = os.MkdirAll(redirDir, 0755)
		redirectionFile := filepath.Join(redirDir, "path.txt")
		err = os.WriteFile(redirectionFile, []byte(newPath), 0644)
		if err != nil {
			return fmt.Errorf("failed to write redirection file: %v", err)
		}
	}

	return nil
}

func LoadConfig() (*Config, error) {
	path := GetConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	err = json.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func SaveConfig(cfg *Config) error {
	path := GetConfigPath()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
