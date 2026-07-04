package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type SpotifyErrorResponse struct {
	Error struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
	} `json:"error"`
}

func SpotifyPlay(accessToken, contextURI string) error {
	playURL := "https://api.spotify.com/v1/me/player/play"

	var bodyBytes []byte
	var err error

	if contextURI != "" {
		if strings.HasPrefix(contextURI, "spotify:track:") {
			reqBody := map[string][]string{
				"uris": {contextURI},
			}
			bodyBytes, err = json.Marshal(reqBody)
		} else {
			reqBody := map[string]string{
				"context_uri": contextURI,
			}
			bodyBytes, err = json.Marshal(reqBody)
		}
		if err != nil {
			return err
		}
	} else {
		bodyBytes = []byte("{}")
	}

	var req *http.Request
	if len(bodyBytes) > 0 {
		req, err = http.NewRequest("PUT", playURL, bytes.NewBuffer(bodyBytes))
	} else {
		req, err = http.NewRequest("PUT", playURL, nil)
	}
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	if len(bodyBytes) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK {
		return nil
	}

	return handleSpotifyError(resp)
}

func SpotifyPause(accessToken string) error {
	pauseURL := "https://api.spotify.com/v1/me/player/pause"

	req, err := http.NewRequest("PUT", pauseURL, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK {
		return nil
	}

	if resp.StatusCode == http.StatusForbidden {
		return nil
	}

	return handleSpotifyError(resp)
}

func SpotifySetVolume(accessToken string, volumePercent int) error {
	volumeURL := fmt.Sprintf("https://api.spotify.com/v1/me/player/volume?volume_percent=%d", volumePercent)

	req, err := http.NewRequest("PUT", volumeURL, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK {
		return nil
	}

	return handleSpotifyError(resp)
}

func SpotifySetShuffle(accessToken string, state bool) error {
	shuffleURL := fmt.Sprintf("https://api.spotify.com/v1/me/player/shuffle?state=%t", state)

	req, err := http.NewRequest("PUT", shuffleURL, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK {
		return nil
	}

	return handleSpotifyError(resp)
}

func handleSpotifyError(resp *http.Response) error {
	var errResp SpotifyErrorResponse
	err := json.NewDecoder(resp.Body).Decode(&errResp)
	if err != nil {
		return fmt.Errorf("spotify API returned status %d", resp.StatusCode)
	}

	msg := errResp.Error.Message
	switch resp.StatusCode {
	case http.StatusNotFound:
		if strings.Contains(strings.ToLower(msg), "no active device") {
			return fmt.Errorf("No active Spotify player found. Please open Spotify on your device and start playing first.")
		}
	case http.StatusForbidden:
		return fmt.Errorf("Playback control restricted. Spotify Premium is required.")
	case http.StatusUnauthorized:
		return fmt.Errorf("Spotify session expired. Please re-authorize in settings.")
	}

	return fmt.Errorf("Spotify error: %s (status %d)", msg, resp.StatusCode)
}
