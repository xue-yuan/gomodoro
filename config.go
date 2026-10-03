package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultSpotifyVolume = 50

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
	Profiles            []Profile `json:"profiles"`
	Sound               string    `json:"sound"`
	SpotifyURI          string    `json:"spotify_uri"`
	SpotifyEnabled      bool      `json:"spotify_enabled"`
	SpotifyVolume       *int      `json:"spotify_volume_percent,omitempty"`
	LegacySpotifyVolume int       `json:"spotify_volume,omitempty"`
	AutoStart           bool      `json:"auto_start"`
	QuickStartProfile   string    `json:"quick_start_profile,omitempty"`
	SpotifyDeviceID     string    `json:"spotify_device_id,omitempty"`
	SpotifyDeviceName   string    `json:"spotify_device_name,omitempty"`

	SpotifyClientID     string    `json:"spotify_client_id"`
	SpotifyClientSecret string    `json:"spotify_client_secret"`
	SpotifyAccessToken  string    `json:"spotify_access_token"`
	SpotifyRefreshToken string    `json:"spotify_refresh_token"`
	SpotifyTokenExpiry  time.Time `json:"spotify_token_expiry"`
}

func (c *Config) Volume() int {
	if c == nil || c.SpotifyVolume == nil {
		return defaultSpotifyVolume
	}
	return *c.SpotifyVolume
}

func (c *Config) SetVolume(v int) {
	c.SpotifyVolume = &v
}

func (c *Config) SpotifyDevice() spotifyDeviceRef {
	return spotifyDeviceRef{ID: c.SpotifyDeviceID, Name: c.SpotifyDeviceName}
}

func (c *Config) SetSpotifyDevice(d spotifyDeviceRef) {
	c.SpotifyDeviceID, c.SpotifyDeviceName = d.ID, d.Name
}

var configDir = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "gomodoro"), nil
}

func redirectionFilePath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "path.txt"), nil
}

func configPathFromRedirect() string {
	redirectionFile, err := redirectionFilePath()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(redirectionFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func GetConfigPath() string {
	if path := configPathFromRedirect(); path != "" {
		return path
	}
	dir, err := configDir()
	if err != nil {
		return ".gomodoro_profiles.json"
	}
	return filepath.Join(dir, "profiles.json")
}

func expandPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("path cannot be empty")
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot resolve home directory: %v", err)
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("path must be absolute (or start with ~/)")
	}
	return filepath.Clean(p), nil
}

func SetConfigPath(newPath string, cfg *Config) error {
	newPath, err := expandPath(newPath)
	if err != nil {
		return err
	}

	if GetConfigPath() == newPath {
		return nil
	}

	info, err := os.Stat(newPath)
	switch {
	case err == nil:
		if info.IsDir() {
			return fmt.Errorf("%s is a directory", newPath)
		}
		if _, err := readConfigFile(newPath); err != nil {
			return fmt.Errorf("%s already exists and is not a valid gomodoro config; refusing to overwrite it", newPath)
		}
	case errors.Is(err, fs.ErrNotExist):
		if cfg == nil {
			cfg = &Config{}
		}
		if err := writeConfigFile(newPath, cfg); err != nil {
			return fmt.Errorf("failed to write config to new location: %v", err)
		}
	default:
		return fmt.Errorf("cannot access %s: %v", newPath, err)
	}

	redirectionFile, err := redirectionFilePath()
	if err != nil {
		return fmt.Errorf("cannot resolve config directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(redirectionFile), 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %v", err)
	}
	if err := writeFileAtomic(redirectionFile, []byte(newPath)); err != nil {
		return fmt.Errorf("failed to write redirection file: %v", err)
	}
	return nil
}

func LoadConfig() (*Config, error) {
	path := GetConfigPath()
	cfg, err := readConfigFile(path)
	if errors.Is(err, fs.ErrNotExist) && configPathFromRedirect() == "" {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, nil
}

func readConfigFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.SpotifyVolume == nil && cfg.LegacySpotifyVolume > 0 {
		cfg.SetVolume(cfg.LegacySpotifyVolume)
	}
	cfg.LegacySpotifyVolume = 0
	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func validateConfig(cfg *Config) error {
	for i, p := range cfg.Profiles {
		label := fmt.Sprintf("profile #%d (%q)", i+1, p.Name)
		if strings.TrimSpace(p.Name) == "" {
			return fmt.Errorf("profile #%d has an empty name", i+1)
		}
		if len(p.Groups) == 0 {
			return fmt.Errorf("%s has no groups", label)
		}
		if p.LongBreakMin <= 0 {
			return fmt.Errorf("%s: long_break_min must be > 0", label)
		}
		for j, g := range p.Groups {
			if g.WorkMin <= 0 || g.BreakMin <= 0 {
				return fmt.Errorf("%s group %d: work_min and break_min must be > 0", label, j+1)
			}
		}
	}
	if cfg.SpotifyVolume != nil && (*cfg.SpotifyVolume < 0 || *cfg.SpotifyVolume > 100) {
		return fmt.Errorf("spotify_volume_percent must be between 0 and 100")
	}
	return nil
}

func SaveConfig(cfg *Config) error {
	return writeConfigFile(GetConfigPath(), cfg)
}

func writeConfigFile(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func tightenPermissions() {
	if dir, err := configDir(); err == nil {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			_ = os.Chmod(dir, 0700)
		}
	}
	if redirectionFile, err := redirectionFilePath(); err == nil {
		_ = os.Chmod(redirectionFile, 0600)
	}
	_ = os.Chmod(GetConfigPath(), 0600)
}
