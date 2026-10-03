package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var spotifyHTTPClient = &http.Client{Timeout: 10 * time.Second}
var spotifyAPIBase = "https://api.spotify.com/v1/me/player"
var errNoActiveDevice = errors.New("no active Spotify device")
var errNoSpotifyDevice = errors.New("No Spotify device found. Open Spotify, signed in to the account you linked, then press space again.")

type SpotifyErrorResponse struct {
	Error struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
		Reason  string `json:"reason"`
	} `json:"error"`
}

type SpotifyDevice struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	IsActive      bool   `json:"is_active"`
	IsRestricted  bool   `json:"is_restricted"`
	VolumePercent *int   `json:"volume_percent"`
}

type spotifyDeviceRef struct {
	ID   string
	Name string
}

func (r spotifyDeviceRef) isAuto() bool { return r.ID == "" }

func (r spotifyDeviceRef) label() string {
	if r.isAuto() {
		return "Automatic"
	}
	return r.Name
}

type SpotifyPlayback struct {
	IsPlaying  bool          `json:"is_playing"`
	ProgressMS int           `json:"progress_ms"`
	Device     SpotifyDevice `json:"device"`
}

func playerURL(path string, q url.Values, deviceID string) string {
	if q == nil {
		q = url.Values{}
	}
	if deviceID != "" {
		q.Set("device_id", deviceID)
	}
	u := spotifyAPIBase + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

func spotifyPut(ctx context.Context, accessToken, endpoint string, body []byte, ignoreForbidden bool) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := spotifyHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted {
		return nil
	}
	if ignoreForbidden && resp.StatusCode == http.StatusForbidden {
		return nil
	}
	return handleSpotifyError(resp)
}

func spotifyGet(ctx context.Context, accessToken, endpoint string, out any) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := spotifyHTTPClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNoContent:
		return false, nil
	case http.StatusOK:
		return true, json.NewDecoder(resp.Body).Decode(out)
	}
	return false, handleSpotifyError(resp)
}

func normalizeSpotifyURI(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "spotify:") {
		s, _, _ = strings.Cut(s, "?")
		return s
	}
	u, err := url.Parse(s)
	if err != nil || !strings.EqualFold(u.Host, "open.spotify.com") {
		return s
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) > 0 && strings.HasPrefix(parts[0], "intl-") {
		parts = parts[1:]
	}
	if len(parts) != 2 || parts[1] == "" {
		return s
	}
	return "spotify:" + parts[0] + ":" + parts[1]
}

func SpotifyPlay(ctx context.Context, accessToken, contextURI, deviceID string) error {
	contextURI = normalizeSpotifyURI(contextURI)
	var reqBody any = map[string]string{}
	if contextURI != "" {
		if strings.HasPrefix(contextURI, "spotify:track:") {
			reqBody = map[string][]string{"uris": {contextURI}}
		} else {
			reqBody = map[string]string{"context_uri": contextURI}
		}
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	return spotifyPut(ctx, accessToken, playerURL("/play", nil, deviceID), bodyBytes, false)
}

func SpotifyPause(ctx context.Context, accessToken string) error {
	return spotifyPut(ctx, accessToken, playerURL("/pause", nil, ""), nil, true)
}

func SpotifySetVolume(ctx context.Context, accessToken string, volumePercent int, deviceID string) error {
	q := url.Values{"volume_percent": {strconv.Itoa(volumePercent)}}
	return spotifyPut(ctx, accessToken, playerURL("/volume", q, deviceID), nil, false)
}

func SpotifySetShuffle(ctx context.Context, accessToken string, state bool, deviceID string) error {
	q := url.Values{"state": {strconv.FormatBool(state)}}
	return spotifyPut(ctx, accessToken, playerURL("/shuffle", q, deviceID), nil, false)
}

func SpotifyTransfer(ctx context.Context, accessToken, deviceID string, play bool) error {
	body, err := json.Marshal(map[string]any{"device_ids": []string{deviceID}, "play": play})
	if err != nil {
		return err
	}
	return spotifyPut(ctx, accessToken, playerURL("", nil, ""), body, false)
}

func SpotifyGetPlayback(ctx context.Context, accessToken string) (*SpotifyPlayback, error) {
	var pb SpotifyPlayback
	found, err := spotifyGet(ctx, accessToken, spotifyAPIBase, &pb)
	if err != nil || !found {
		return nil, err
	}
	return &pb, nil
}

func SpotifyDevices(ctx context.Context, accessToken string) ([]SpotifyDevice, error) {
	var resp struct {
		Devices []SpotifyDevice `json:"devices"`
	}
	if _, err := spotifyGet(ctx, accessToken, spotifyAPIBase+"/devices", &resp); err != nil {
		return nil, err
	}
	return resp.Devices, nil
}

func pickSpotifyDevice(devices []SpotifyDevice) (SpotifyDevice, bool) {
	var computer, first *SpotifyDevice
	for i := range devices {
		d := &devices[i]
		if d.IsRestricted || d.ID == "" {
			continue
		}
		if d.IsActive {
			return *d, true
		}
		if computer == nil && strings.EqualFold(d.Type, "Computer") {
			computer = d
		}
		if first == nil {
			first = d
		}
	}
	switch {
	case computer != nil:
		return *computer, true
	case first != nil:
		return *first, true
	}
	return SpotifyDevice{}, false
}

func matchSpotifyDevice(devices []SpotifyDevice, ref spotifyDeviceRef) (SpotifyDevice, bool) {
	if ref.isAuto() {
		return SpotifyDevice{}, false
	}
	var byName *SpotifyDevice
	for i := range devices {
		d := &devices[i]
		if d.IsRestricted || d.ID == "" {
			continue
		}
		if d.ID == ref.ID {
			return *d, true
		}
		if byName == nil && ref.Name != "" && strings.EqualFold(d.Name, ref.Name) {
			byName = d
		}
	}
	if byName != nil {
		return *byName, true
	}
	return SpotifyDevice{}, false
}

func findSpotifyDevice(ctx context.Context, accessToken string, preferred spotifyDeviceRef) (device SpotifyDevice, found bool, err error) {
	devices, err := SpotifyDevices(ctx, accessToken)
	if err != nil {
		return SpotifyDevice{}, false, err
	}
	if d, ok := matchSpotifyDevice(devices, preferred); ok {
		return d, true, nil
	}
	d, ok := pickSpotifyDevice(devices)
	if !ok {
		return SpotifyDevice{}, false, errNoSpotifyDevice
	}
	return d, false, nil
}

func handleSpotifyError(resp *http.Response) error {
	var errResp SpotifyErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		return fmt.Errorf("Spotify returned status %d", resp.StatusCode)
	}

	msg := errResp.Error.Message
	switch resp.StatusCode {
	case http.StatusNotFound:
		if errResp.Error.Reason == "NO_ACTIVE_DEVICE" || strings.Contains(strings.ToLower(msg), "no active device") {
			return errNoActiveDevice
		}
	case http.StatusForbidden:
		return fmt.Errorf("Playback control needs Spotify Premium.")
	case http.StatusUnauthorized:
		return fmt.Errorf("Spotify session expired. Re-authorize in Settings › Spotify.")
	case http.StatusTooManyRequests:
		return fmt.Errorf("Spotify is rate limiting requests. Try again in a moment.")
	}

	return fmt.Errorf("Spotify error: %s (status %d)", msg, resp.StatusCode)
}
