package main

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

var (
	playbackCheckDelay    = 3 * time.Second
	playbackRecheckDelay  = 2 * time.Second
	playbackCheckInterval = 1500 * time.Millisecond
)

const playbackMinAdvanceMS = 500

type playbackCheckMsg struct {
	seq       int
	stalled   bool
	token     *SpotifyTokenResponse
	fetchedAt time.Time
}

type playbackSample struct {
	positionMS int
	playing    bool
}

const localPlayerScript = `if application "Spotify" is running then
	tell application "Spotify" to return (player state as text) & " " & (((player position) * 1000) as integer)
end if`

var localSpotifyPlayer = func(ctx context.Context) (playbackSample, bool) {
	out, err := exec.CommandContext(ctx, "osascript", "-e", localPlayerScript).Output()
	if err != nil {
		return playbackSample{}, false
	}
	state, pos, found := strings.Cut(strings.TrimSpace(string(out)), " ")
	if !found {
		return playbackSample{}, false
	}
	ms, err := strconv.Atoi(pos)
	if err != nil {
		return playbackSample{}, false
	}
	return playbackSample{positionMS: ms, playing: state == "playing"}, true
}

var localComputerName = func() string {
	out, err := exec.Command("scutil", "--get", "ComputerName").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func apiSpotifyPlayer(ctx context.Context, accessToken string) (playbackSample, bool) {
	pb, err := SpotifyGetPlayback(ctx, accessToken)
	if err != nil || pb == nil {
		return playbackSample{}, false
	}
	return playbackSample{positionMS: pb.ProgressMS, playing: pb.IsPlaying}, true
}

func checkPlaybackCmd(creds spotifyCreds, seq int, deviceName string, delay time.Duration) tea.Cmd {
	return func() tea.Msg {
		msg := playbackCheckMsg{seq: seq}
		time.Sleep(delay)

		ctx, cancel := context.WithTimeout(context.Background(), spotifyCmdTimeout)
		defer cancel()

		var sample func() (playbackSample, bool)
		if deviceName != "" && strings.EqualFold(deviceName, localComputerName()) {
			if _, ok := localSpotifyPlayer(ctx); ok {
				sample = func() (playbackSample, bool) { return localSpotifyPlayer(ctx) }
			}
		}
		if sample == nil {
			accessToken, token, err := validAccessToken(ctx, creds)
			if token != nil {
				msg.token = token
				msg.fetchedAt = time.Now()
			}
			if err != nil {
				return msg
			}
			sample = func() (playbackSample, bool) { return apiSpotifyPlayer(ctx, accessToken) }
		}

		first, ok := sample()
		if !ok || !first.playing {
			return msg
		}
		time.Sleep(playbackCheckInterval)
		second, ok := sample()
		if !ok || !second.playing {
			return msg
		}
		advance := second.positionMS - first.positionMS
		msg.stalled = advance >= 0 && advance < playbackMinAdvanceMS
		return msg
	}
}

func (m *model) handlePlaybackCheck(msg playbackCheckMsg) tea.Cmd {
	if msg.token != nil {
		m.storeSpotifyToken(msg.token, msg.fetchedAt)
	}
	if msg.seq != m.spotifyCheckSeq || m.spotifyBusy || m.spotifyState != spotifyPlaying {
		return nil
	}
	m.spotifyStalled = msg.stalled
	if !m.spotifyStalled {
		return nil
	}
	return checkPlaybackCmd(credsFromConfig(m.savedConfig), msg.seq, m.spotifyDevice, playbackRecheckDelay)
}
