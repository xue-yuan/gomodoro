package main

import (
	"context"
	"strings"
	"testing"
)

func fakeLocalSpotify(t *testing.T, name string, samples ...playbackSample) {
	t.Helper()
	origDelay, origRecheck, origInterval := playbackCheckDelay, playbackRecheckDelay, playbackCheckInterval
	origPlayer, origName := localSpotifyPlayer, localComputerName
	playbackCheckDelay, playbackRecheckDelay, playbackCheckInterval = 0, 0, 0
	localComputerName = func() string { return name }
	i := 0
	localSpotifyPlayer = func(context.Context) (playbackSample, bool) {
		if len(samples) == 0 {
			return playbackSample{}, false
		}
		s := samples[min(i, len(samples)-1)]
		i++
		return s, true
	}
	t.Cleanup(func() {
		playbackCheckDelay, playbackRecheckDelay, playbackCheckInterval = origDelay, origRecheck, origInterval
		localSpotifyPlayer, localComputerName = origPlayer, origName
	})
}

func playingAt(ms int) playbackSample { return playbackSample{positionMS: ms, playing: true} }

func TestPlaybackCheckOnThisMac(t *testing.T) {
	cases := []struct {
		name    string
		samples []playbackSample
		stalled bool
	}{
		{"position does not move", []playbackSample{playingAt(1532), playingAt(1532), playingAt(1532)}, true},
		{"position advances", []playbackSample{playingAt(1000), playingAt(1000), playingAt(2500)}, false},
		{"track changed", []playbackSample{playingAt(90000), playingAt(90000), playingAt(200)}, false},
		{"paused in the Spotify app", []playbackSample{{positionMS: 1532}, {positionMS: 1532}, {positionMS: 1532}}, false},
		{"paused between samples", []playbackSample{playingAt(1532), playingAt(1532), {positionMS: 1600}}, false},
	}
	for _, c := range cases {
		fakeLocalSpotify(t, "Ares", c.samples...)
		if msg := checkPlaybackCmd(validCreds, 1, "ares", 0)().(playbackCheckMsg); msg.stalled != c.stalled {
			t.Errorf("%s: stalled = %v, want %v", c.name, msg.stalled, c.stalled)
		}
	}
}

func TestPlaybackCheckUsesAPIForOtherDevices(t *testing.T) {
	fakeLocalSpotify(t, "Ares", playingAt(1000), playingAt(2500), playingAt(4000))
	f := useFakeSpotify(t, `{"id":"speaker","name":"Kitchen","type":"Speaker"}`)
	f.active = "speaker"
	if msg := checkPlaybackCmd(validCreds, 1, "Kitchen", 0)().(playbackCheckMsg); !msg.stalled {
		t.Fatalf("expected the API sample to be used (requests: %v)", f.requests)
	}
}

func TestStalledWarningRechecksAndClearsWhenPlaybackRecovers(t *testing.T) {
	m := spotifyModel(false)
	m.triggerSpotifyPlay()
	m.handleSpotifyResult(spotifyResultMsg{action: spotifyActionPlay, loadedContext: true, device: "Ares"})
	seq := m.spotifyCheckSeq

	if cmd := m.handlePlaybackCheck(playbackCheckMsg{seq: seq - 1, stalled: true}); cmd != nil || m.spotifyStalled {
		t.Fatal("a check from an earlier command must be ignored")
	}

	cmd := m.handlePlaybackCheck(playbackCheckMsg{seq: seq, stalled: true})
	if !m.spotifyStalled || cmd == nil {
		t.Fatal("a stall should show the warning and schedule another check")
	}
	if line := stripANSI(m.spotifyStatusLine()); !strings.Contains(line, "Playback on Ares stalled") {
		t.Fatalf("status line = %q", line)
	}

	if cmd := m.handlePlaybackCheck(playbackCheckMsg{seq: seq}); cmd != nil || m.spotifyStalled {
		t.Fatal("the warning should clear and checking should stop once playback advances")
	}
	if line := stripANSI(m.spotifyStatusLine()); !strings.Contains(line, "playing on Ares") {
		t.Fatalf("status line = %q", line)
	}
}

func TestStalledWarningClearsOnNextCommand(t *testing.T) {
	m := spotifyModel(false)
	m.triggerSpotifyPlay()
	m.handleSpotifyResult(spotifyResultMsg{action: spotifyActionPlay, loadedContext: true, device: "Ares"})
	seq := m.spotifyCheckSeq
	m.handlePlaybackCheck(playbackCheckMsg{seq: seq, stalled: true})

	m.triggerSpotifyPause()
	if m.spotifyStalled {
		t.Fatal("sending a new command should clear the warning")
	}
	m.handleSpotifyResult(spotifyResultMsg{action: spotifyActionPause})
	if cmd := m.handlePlaybackCheck(playbackCheckMsg{seq: seq, stalled: true}); cmd != nil || m.spotifyStalled {
		t.Fatal("a pending recheck is stale once another command was sent")
	}
}
