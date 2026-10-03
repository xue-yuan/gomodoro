package main

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func profileModel(t *testing.T) model {
	t.Helper()
	useTempConfigDir(t)
	m := initialModel(&Config{Profiles: []Profile{
		{Name: "Deep", Groups: []PomodoroGroup{{50, 10}, {50, 10}}, LongBreakMin: 30},
		{Name: "Short", Groups: []PomodoroGroup{{15, 3}}, LongBreakMin: 10},
	}})
	m.notifier = nopNotifier{}
	m.state = stateSelectProfile
	return m
}

func TestQuickStartUsesChosenProfile(t *testing.T) {
	m := profileModel(t)
	if got := m.quickStartProfile().Name; got != "Classic" {
		t.Fatalf("default quick start = %q, want Classic", got)
	}
	m, _ = update(t, m, key("s"))
	if got := m.quickStartProfile().Name; got != "Deep" {
		t.Fatalf("quick start = %q, want Deep", got)
	}
	m, _ = update(t, m, key("d"))
	m, _ = update(t, m, key("y"))
	if m.savedConfig.QuickStartProfile != "" {
		t.Fatal("deleting the quick start profile should reset quick start")
	}
}

func TestEditProfileRenamesQuickStart(t *testing.T) {
	m := profileModel(t)
	m.savedConfig.QuickStartProfile = "Deep"

	m, _ = update(t, m, key("e"))
	if m.state != stateConfigName || m.configInputsStep1[0].Value() != "Deep" || m.configInputsStep1[1].Value() != "2" {
		t.Fatal("edit should open the form prefilled")
	}
	m.configInputsStep1[0].SetValue("Deeper")
	m, _ = update(t, m, key("enter"))
	if m.configInputsStep2[0].Value() != "50" {
		t.Fatalf("group inputs should be prefilled, got %q", m.configInputsStep2[0].Value())
	}
	m, _ = update(t, m, key("enter"))

	if len(m.savedConfig.Profiles) != 2 || m.savedConfig.Profiles[0].Name != "Deeper" {
		t.Fatalf("profiles = %+v", m.savedConfig.Profiles)
	}
	if m.savedConfig.QuickStartProfile != "Deeper" {
		t.Fatalf("quick start = %q, want Deeper", m.savedConfig.QuickStartProfile)
	}
}

func TestDuplicateProfileNameRejected(t *testing.T) {
	m := profileModel(t)
	m, _ = update(t, m, key("n"))
	m.configInputsStep1[0].SetValue("short")
	m, _ = update(t, m, key("enter"))
	if m.state != stateConfigName || m.configError == "" {
		t.Fatal("duplicate name should be rejected")
	}
}

func TestProfileFormEscReturnsToOrigin(t *testing.T) {
	m := profileModel(t)
	m, _ = update(t, m, key("n"))
	m, _ = update(t, m, key("esc"))
	if m.state != stateSelectProfile {
		t.Fatalf("esc from a form opened in Profiles should return there, got %v", m.state)
	}
}

func TestProfileSummary(t *testing.T) {
	if got := profileSummary(defaultProfile()); got != "4 × 25m/5m · long 20m" {
		t.Errorf("summary = %q", got)
	}
	mixed := Profile{Groups: []PomodoroGroup{{50, 10}, {25, 5}}, LongBreakMin: 90}
	if got := profileSummary(mixed); got != "2 groups · 1h15m focus · long 1h30m" {
		t.Errorf("summary = %q", got)
	}
}

func TestSpotifyDirty(t *testing.T) {
	m := initialModel(&Config{SpotifyURI: "spotify:playlist:x"})
	m.initSpotifyInputs()
	if m.spotifyDirty() {
		t.Fatal("fresh settings page should not be dirty")
	}
	m.spotifyEnabledDraft = true
	if !m.spotifyDirty() {
		t.Fatal("toggling autoplay should mark the page dirty")
	}
}

func TestBigClockShape(t *testing.T) {
	for _, compact := range []bool{false, true} {
		lines := strings.Split(bigClock("25:00", compact), "\n")
		want := 5
		if compact {
			want = 3
		}
		if len(lines) != want {
			t.Fatalf("compact=%v: %d rows, want %d", compact, len(lines), want)
		}
		for _, l := range lines {
			if lipgloss.Width(l) != lipgloss.Width(lines[0]) {
				t.Fatalf("compact=%v: rows have different widths", compact)
			}
		}
	}
}

func TestHelpBarWraps(t *testing.T) {
	th := newTheme(true)
	items := []helpItem{{"enter", "start"}, {"n", "new"}, {"e", "edit"}, {"s", "quick start"}, {"d", "delete"}, {"esc", "back"}}
	for _, line := range strings.Split(th.helpBar(items, 30), "\n") {
		if w := lipgloss.Width(line); w > 30 {
			t.Fatalf("help line is %d wide, want <= 30: %q", w, line)
		}
	}
}

func TestViewsRenderAtSmallSizes(t *testing.T) {
	m := profileModel(t)
	for _, size := range [][2]int{{0, 0}, {50, 20}, {80, 24}, {120, 40}} {
		m.width, m.height = size[0], size[1]
		for _, st := range []appState{stateMenu, stateSelectProfile, stateSettingsMenu, stateSoundSettings} {
			m.state = st
			_ = m.View()
		}
		m.startProfile(defaultProfile())
		v := m.View()
		if m.width > 0 && lipgloss.Width(v.Content) > m.width {
			t.Fatalf("timer view wider than terminal at %v", size)
		}
	}
}
