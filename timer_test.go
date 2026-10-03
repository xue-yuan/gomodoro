package main

import (
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

type nopNotifier struct{}

func (nopNotifier) Notify(title, subtitle, message string) error { return nil }
func (nopNotifier) PlaySound(name string)                        {}

func newTimerModel(autoStart bool) model {
	m := initialModel(&Config{AutoStart: autoStart})
	m.notifier = nopNotifier{}
	m.startProfile(Profile{
		Name:         "test",
		Groups:       []PomodoroGroup{{WorkMin: 1, BreakMin: 1}, {WorkMin: 1, BreakMin: 1}},
		LongBreakMin: 1,
	})
	return m
}

func update(t *testing.T, m model, msg tea.Msg) (model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(model), cmd
}

func TestTimerKeepsTickingAfterPhaseCompletes(t *testing.T) {
	m := newTimerModel(true)
	if !m.active {
		t.Fatal("timer should auto-start")
	}

	m.deadline = time.Now().Add(-time.Millisecond)
	m, cmd := update(t, m, tickMsg{id: m.tickID})

	if m.currentSub != subPhaseBreak {
		t.Fatalf("currentSub = %v, want break", m.currentSub)
	}
	if !m.active {
		t.Fatal("next phase should auto-start")
	}
	if cmd == nil {
		t.Fatal("no command returned after phase completion; the timer would freeze")
	}
}

func TestStaleTicksAreIgnored(t *testing.T) {
	m := newTimerModel(true)
	staleID := m.tickID

	m, _ = update(t, m, tea.KeyPressMsg{Code: tea.KeySpace})
	m, _ = update(t, m, tea.KeyPressMsg{Code: tea.KeySpace})
	if m.tickID == staleID {
		t.Fatal("resuming should start a new tick chain")
	}

	_, cmd := update(t, m, tickMsg{id: staleID})
	if cmd != nil {
		t.Fatal("stale tick should not schedule another tick (would double the countdown speed)")
	}
}

func TestPauseFreezesRemainingTime(t *testing.T) {
	m := newTimerModel(true)
	m.deadline = time.Now().Add(42 * time.Second)

	m, _ = update(t, m, tea.KeyPressMsg{Code: tea.KeySpace})
	if m.active {
		t.Fatal("space should pause")
	}
	got := m.remaining()
	if got < 41*time.Second || got > 42*time.Second {
		t.Fatalf("remaining() = %v, want ~42s", got)
	}
	time.Sleep(20 * time.Millisecond)
	if m.remaining() != got {
		t.Fatal("remaining time changed while paused")
	}
}

func TestResetKeepsRunningState(t *testing.T) {
	m := newTimerModel(false)
	m, _ = update(t, m, tea.KeyPressMsg{Code: 'r', Text: "r"})
	if m.active {
		t.Fatal("reset of a paused timer should stay paused")
	}

	m, _ = update(t, m, tea.KeyPressMsg{Code: tea.KeySpace})
	m, _ = update(t, m, tea.KeyPressMsg{Code: 'r', Text: "r"})
	if !m.active {
		t.Fatal("reset of a running timer should keep running")
	}
	if m.remaining() < 59*time.Second {
		t.Fatalf("remaining() = %v after reset, want ~1m", m.remaining())
	}
}

func TestCycleRestartTicks(t *testing.T) {
	m := newTimerModel(false)
	m.cycleFinished = true
	m, cmd := update(t, m, tea.KeyPressMsg{Code: 'y', Text: "y"})
	if !m.active || cmd == nil {
		t.Fatal("restarting the cycle should start the timer and its tick chain")
	}
}

func TestStartGroupTimerHandlesEmptyProfile(t *testing.T) {
	m := initialModel(&Config{})
	m.activeProfile = Profile{Name: "empty", LongBreakMin: 1}
	m.startGroupTimer(0, subPhaseWork) // must not panic
	if m.timeRemaining != 0 {
		t.Fatalf("timeRemaining = %v, want 0", m.timeRemaining)
	}
}

func TestValidSound(t *testing.T) {
	if got := validSound("Ping"); got != "Ping" {
		t.Errorf("validSound(Ping) = %q", got)
	}
	if got := validSound(`Glass" & (do shell script "id") & "`); got != defaultSound {
		t.Errorf("validSound(injection) = %q, want %q", got, defaultSound)
	}
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func TestEscKeepsSessionResumable(t *testing.T) {
	m := newTimerModel(true)
	m, _ = update(t, m, key("esc"))
	if m.state != stateMenu || !m.sessionActive || m.active {
		t.Fatalf("after esc: state=%v sessionActive=%v active=%v", m.state, m.sessionActive, m.active)
	}
	items := m.mainMenuItems()
	if items[m.menuIndex].id != "resume" {
		t.Fatalf("menu should select Resume, got %q", items[m.menuIndex].id)
	}
	m, _ = update(t, m, key("enter"))
	if m.state != stateTimer {
		t.Fatal("enter on Resume should return to the timer")
	}
}

func TestQuitRequiresConfirmation(t *testing.T) {
	m := newTimerModel(true)
	m, _ = update(t, m, key("q"))
	if !m.confirmQuit || m.quitting {
		t.Fatal("q should ask for confirmation first")
	}
	m, _ = update(t, m, key("n"))
	if m.confirmQuit || m.quitting {
		t.Fatal("any key other than y should cancel quitting")
	}
	m, _ = update(t, m, key("q"))
	m, cmd := update(t, m, key("y"))
	if !m.quitting || cmd == nil {
		t.Fatal("y should quit")
	}
}

func TestPhaseReadyPromptWithoutAutoStart(t *testing.T) {
	m := newTimerModel(false)
	if !m.phaseReady {
		t.Fatal("a new session without auto-start should prompt to start")
	}
	m, _ = update(t, m, key("space"))
	if m.phaseReady || !m.active {
		t.Fatal("space should start the phase and clear the prompt")
	}
	m, _ = update(t, m, key("s"))
	if !m.phaseReady || m.active || m.currentSub != subPhaseBreak {
		t.Fatalf("after skip: phaseReady=%v active=%v sub=%v", m.phaseReady, m.active, m.currentSub)
	}
}

func TestSpotifyResultHandling(t *testing.T) {
	m := initialModel(&Config{SpotifyEnabled: true})
	vol := 35
	m.handleSpotifyResult(spotifyResultMsg{action: spotifyActionPlay, volume: &vol})
	if m.spotifyState != spotifyPlaying || !m.spotifyVolumeKnown || m.spotifyVolume != 35 {
		t.Fatalf("after play: state=%v known=%v vol=%d", m.spotifyState, m.spotifyVolumeKnown, m.spotifyVolume)
	}
	m.handleSpotifyResult(spotifyResultMsg{action: spotifyActionPause, err: errTest})
	if m.spotifyError != "" {
		t.Fatal("a failed pause should not show an error")
	}
	m.handleSpotifyResult(spotifyResultMsg{action: spotifyActionPlay, err: errTest})
	if m.spotifyError == "" {
		t.Fatal("a failed play should show an error")
	}
}

var errTest = errors.New("boom")
