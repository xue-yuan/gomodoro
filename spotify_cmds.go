package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	spotifyCmdTimeout  = 20 * time.Second
	spotifyQuitTimeout = 3 * time.Second
	spotifyAuthTimeout = 5 * time.Minute
	volumeDebounce     = 400 * time.Millisecond
)

type spotifyCreds struct {
	ClientID     string
	ClientSecret string
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
}

func credsFromConfig(cfg *Config) spotifyCreds {
	return spotifyCreds{
		ClientID:     cfg.SpotifyClientID,
		ClientSecret: cfg.SpotifyClientSecret,
		AccessToken:  cfg.SpotifyAccessToken,
		RefreshToken: cfg.SpotifyRefreshToken,
		Expiry:       cfg.SpotifyTokenExpiry,
	}
}

type spotifyAction int

const (
	spotifyActionPlay spotifyAction = iota
	spotifyActionPause
	spotifyActionVolume
)

type spotifyPlayState int

const (
	spotifyIdle spotifyPlayState = iota
	spotifyPlaying
	spotifyPaused
)

type spotifyResultMsg struct {
	action    spotifyAction
	token     *SpotifyTokenResponse
	fetchedAt time.Time
	volume *int
	device string
	missingDevice string
	loadedContext bool
	err           error
}

type spotifyDevicesMsg struct {
	devices   []SpotifyDevice
	token     *SpotifyTokenResponse
	fetchedAt time.Time
	err       error
}

type spotifyAuthResultMsg struct {
	token     *SpotifyTokenResponse
	fetchedAt time.Time
	err       error
}

type volumeApplyMsg struct {
	seq int
}

func validAccessToken(ctx context.Context, creds spotifyCreds) (string, *SpotifyTokenResponse, error) {
	if creds.RefreshToken == "" {
		return "", nil, fmt.Errorf("Spotify account not linked. Go to settings.")
	}
	if creds.AccessToken != "" && !time.Now().Add(5*time.Minute).After(creds.Expiry) {
		return creds.AccessToken, nil, nil
	}
	resp, err := RefreshSpotifyToken(ctx, creds.ClientID, creds.ClientSecret, creds.RefreshToken)
	if err != nil {
		return "", nil, err
	}
	return resp.AccessToken, resp, nil
}

func spotifyCmd(creds spotifyCreds, timeout time.Duration, action spotifyAction, run func(ctx context.Context, accessToken string, msg *spotifyResultMsg) error) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		msg := spotifyResultMsg{action: action}
		accessToken, token, err := validAccessToken(ctx, creds)
		if token != nil {
			msg.token = token
			msg.fetchedAt = time.Now()
		}
		if err != nil {
			msg.err = err
			return msg
		}

		msg.err = run(ctx, accessToken, &msg)
		return msg
	}
}

func listSpotifyDevicesCmd(creds spotifyCreds) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), spotifyCmdTimeout)
		defer cancel()

		var msg spotifyDevicesMsg
		accessToken, token, err := validAccessToken(ctx, creds)
		if token != nil {
			msg.token = token
			msg.fetchedAt = time.Now()
		}
		if err != nil {
			msg.err = err
			return msg
		}
		msg.devices, msg.err = SpotifyDevices(ctx, accessToken)
		return msg
	}
}

func withDeviceFallback(ctx context.Context, accessToken string, preferred spotifyDeviceRef, action func(deviceID string) error) error {
	err := action("")
	if !errors.Is(err, errNoActiveDevice) {
		return err
	}
	device, _, err := findSpotifyDevice(ctx, accessToken, preferred)
	if err != nil {
		return err
	}
	return action(device.ID)
}

func playSpotifyCmd(creds spotifyCreds, uri string, shuffle bool, preferred spotifyDeviceRef) tea.Cmd {
	return spotifyCmd(creds, spotifyCmdTimeout, spotifyActionPlay, func(ctx context.Context, accessToken string, msg *spotifyResultMsg) error {
		if !preferred.isAuto() {
			return playOnPreferredDevice(ctx, accessToken, uri, shuffle, preferred, msg)
		}
		err := withDeviceFallback(ctx, accessToken, preferred, func(deviceID string) error {
			if shuffle && uri != "" {
				if err := SpotifySetShuffle(ctx, accessToken, true, deviceID); errors.Is(err, errNoActiveDevice) {
					return err
				}
			}
			return SpotifyPlay(ctx, accessToken, uri, deviceID)
		})
		if err != nil {
			return err
		}
		msg.loadedContext = uri != ""
		if pb, err := SpotifyGetPlayback(ctx, accessToken); err == nil && pb != nil {
			msg.volume = pb.Device.VolumePercent
			msg.device = pb.Device.Name
		}
		return nil
	})
}

func playOnPreferredDevice(ctx context.Context, accessToken, uri string, shuffle bool, preferred spotifyDeviceRef, msg *spotifyResultMsg) error {
	device, found, err := findSpotifyDevice(ctx, accessToken, preferred)
	if err != nil {
		return err
	}
	switch {
	case uri != "":
		if shuffle {
			_ = SpotifySetShuffle(ctx, accessToken, true, device.ID)
		}
		err = SpotifyPlay(ctx, accessToken, uri, device.ID)
	case device.IsActive:
		err = SpotifyPlay(ctx, accessToken, "", device.ID)
	default:
		err = SpotifyTransfer(ctx, accessToken, device.ID, true)
	}
	if err != nil {
		return err
	}
	if !found {
		msg.missingDevice = preferred.Name
	}
	msg.loadedContext = uri != ""
	msg.device = device.Name
	msg.volume = device.VolumePercent
	return nil
}

func pauseSpotifyCmd(creds spotifyCreds, timeout time.Duration) tea.Cmd {
	return spotifyCmd(creds, timeout, spotifyActionPause, func(ctx context.Context, accessToken string, _ *spotifyResultMsg) error {
		return SpotifyPause(ctx, accessToken)
	})
}

func changeSpotifyVolumeCmd(creds spotifyCreds, volume int, preferred spotifyDeviceRef) tea.Cmd {
	return spotifyCmd(creds, spotifyCmdTimeout, spotifyActionVolume, func(ctx context.Context, accessToken string, _ *spotifyResultMsg) error {
		return withDeviceFallback(ctx, accessToken, preferred, func(deviceID string) error {
			return SpotifySetVolume(ctx, accessToken, volume, deviceID)
		})
	})
}

func runSpotifyAuthCmd(ctx context.Context, clientID, clientSecret, state, authURL string) tea.Cmd {
	return func() tea.Msg {
		code, err := StartOAuthServer(ctx, state, func() {
			openBrowser(authURL)
		})
		if err != nil {
			return spotifyAuthResultMsg{err: err}
		}

		resp, err := ExchangeCodeForToken(ctx, clientID, clientSecret, code)
		if err != nil {
			return spotifyAuthResultMsg{err: err}
		}
		return spotifyAuthResultMsg{token: resp, fetchedAt: time.Now()}
	}
}

func openBrowser(url string) {
	_ = exec.Command("open", url).Start()
}

func copyToClipboard(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

func (m *model) spotifyActive() bool {
	return m.savedConfig.SpotifyEnabled
}

func (m *model) triggerSpotifyPlay() tea.Cmd {
	if !m.spotifyActive() || m.savedConfig.SpotifyURI == "" {
		return nil
	}
	m.spotifyRequested = true
	play := true
	m.spotifyWantPlay = &play
	return m.dispatchSpotify()
}

func (m *model) spotifyStarted() bool {
	return m.spotifyActive() && m.spotifyRequested
}

func (m *model) triggerSpotifyPause() tea.Cmd {
	if !m.spotifyStarted() {
		return nil
	}
	play := false
	m.spotifyWantPlay = &play
	return m.dispatchSpotify()
}

func (m *model) dispatchSpotify() tea.Cmd {
	if m.spotifyBusy || !m.spotifyActive() {
		return nil
	}
	creds := credsFromConfig(m.savedConfig)
	switch {
	case m.spotifyWantPlay != nil:
		play := *m.spotifyWantPlay
		m.spotifyWantPlay = nil
		m.spotifyBusy = true
		if !play {
			m.spotifyInFlight = spotifyActionPause
			return pauseSpotifyCmd(creds, spotifyCmdTimeout)
		}
		m.spotifyInFlight = spotifyActionPlay
		if !m.spotifyLoaded {
			return playSpotifyCmd(creds, m.savedConfig.SpotifyURI, true, m.spotifyTarget)
		}
		return playSpotifyCmd(creds, "", false, m.spotifyTarget)
	case m.spotifyWantVolume != nil:
		volume := *m.spotifyWantVolume
		m.spotifyWantVolume = nil
		m.spotifyBusy = true
		m.spotifyInFlight = spotifyActionVolume
		return changeSpotifyVolumeCmd(creds, volume, m.spotifyTarget)
	}
	return nil
}

func (m *model) pauseAndQuitCmd() tea.Cmd {
	m.quitting = true
	if !m.spotifyStarted() {
		return tea.Quit
	}
	return tea.Sequence(pauseSpotifyCmd(credsFromConfig(m.savedConfig), spotifyQuitTimeout), tea.Quit)
}

func (m *model) adjustVolume(delta int) tea.Cmd {
	if !m.spotifyActive() {
		return nil
	}
	m.spotifyVolume = max(0, min(100, m.spotifyVolume+delta))
	m.spotifyVolumeKnown = true
	m.volumePending = true
	m.volumeSeq++
	seq := m.volumeSeq
	return tea.Tick(volumeDebounce, func(time.Time) tea.Msg {
		return volumeApplyMsg{seq: seq}
	})
}

func (m *model) applyVolume(msg volumeApplyMsg) tea.Cmd {
	if msg.seq != m.volumeSeq || !m.spotifyActive() {
		return nil
	}
	m.volumePending = false
	m.savedConfig.SetVolume(m.spotifyVolume)
	m.persist()
	volume := m.spotifyVolume
	m.spotifyWantVolume = &volume
	return m.dispatchSpotify()
}

func (m *model) handleSpotifyResult(msg spotifyResultMsg) tea.Cmd {
	m.spotifyBusy = false
	if msg.token != nil {
		m.storeSpotifyToken(msg.token, msg.fetchedAt)
	}

	switch {
	case msg.err != nil:
		if msg.action != spotifyActionPause {
			m.spotifyError = msg.err.Error()
		}
	case msg.action == spotifyActionPlay:
		m.spotifyError = ""
		m.spotifyState = spotifyPlaying
		if msg.loadedContext {
			m.spotifyLoaded = true
		}
		if msg.device != "" {
			m.spotifyDevice = msg.device
		}
		m.spotifyMissingDevice = msg.missingDevice
	case msg.action == spotifyActionPause:
		m.spotifyError = ""
		m.spotifyState = spotifyPaused
	default:
		m.spotifyError = ""
	}

	if msg.err == nil && msg.volume != nil && !m.volumePending {
		m.spotifyVolume = *msg.volume
		m.spotifyVolumeKnown = true
	}
	return m.dispatchSpotify()
}

func (m *model) storeSpotifyToken(token *SpotifyTokenResponse, fetchedAt time.Time) {
	m.savedConfig.SpotifyAccessToken = token.AccessToken
	if token.RefreshToken != "" {
		m.savedConfig.SpotifyRefreshToken = token.RefreshToken
	}
	m.savedConfig.SpotifyTokenExpiry = fetchedAt.Add(time.Duration(token.ExpiresIn) * time.Second)
	m.persist()
}
