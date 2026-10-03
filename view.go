package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func (m model) statusLine() string {
	if m.statusMsg == "" {
		return ""
	}
	return "\n" + m.theme.fg(m.theme.success).Width(m.contentWidth()).Render("✓ "+m.statusMsg)
}

func (m model) viewMenu() string {
	t := m.theme
	var sb strings.Builder
	sb.WriteString(t.fg(t.text).Bold(true).Render("What are we focusing on?") + "\n\n")

	items := m.mainMenuItems()
	idx := wrapIndex(m.menuIndex, len(items))
	for i, it := range items {
		selected := i == idx
		accent := t.focus
		if it.id == "resume" {
			accent = t.shortBreak
		}
		sb.WriteString(m.listItem(selected, accent, it.label, "") + "\n")
		if it.value != "" {
			desc := truncate(it.value, m.contentWidth()-2)
			if selected {
				sb.WriteString("  " + t.fg(t.muted).Render(desc) + "\n")
			} else {
				sb.WriteString("  " + t.fg(t.subtle).Render(desc) + "\n")
			}
		}
		if i < len(items)-1 {
			sb.WriteString("\n")
		}
	}
	sb.WriteString(m.statusLine())

	return m.frame(t.focus, "Home", sb.String(), []helpItem{
		{"↑/↓", "move"}, {"enter", "select"}, {"q", "quit"},
	})
}

func (m model) viewSettingsMenu() string {
	t := m.theme
	var sb strings.Builder
	for i, it := range m.settingsItems() {
		value := truncate(it.value, m.contentWidth()-lipgloss.Width(it.label)-6)
		sb.WriteString(m.listItem(i == m.settingsMenuIndex, t.focus, it.label, value) + "\n")
		if i < len(m.settingsItems())-1 {
			sb.WriteString("\n")
		}
	}
	sb.WriteString(m.statusLine())

	return m.frame(t.focus, "Settings", sb.String(), []helpItem{
		{"↑/↓", "move"}, {"enter", "change"}, {"esc", "back"},
	})
}

func (m model) viewSelectProfile() string {
	t := m.theme
	var sb strings.Builder
	profiles := m.savedConfig.Profiles

	if len(profiles) == 0 {
		sb.WriteString(t.fg(t.text).Render("No profiles yet.") + "\n\n")
		sb.WriteString(t.fg(t.muted).Width(m.contentWidth()).Render("A profile sets how many focus groups you do, how long each focus and break lasts, and the long break at the end.") + "\n\n")
		sb.WriteString(t.fg(t.muted).Render("Press ") + t.fg(t.focus).Bold(true).Render("n") + t.fg(t.muted).Render(" to create your first one."))
	} else {
		quick := m.savedConfig.QuickStartProfile
		for i, p := range profiles {
			label := p.Name
			if p.Name == quick {
				label += " ★"
			}
			summary := truncate(profileSummary(p), m.contentWidth()-lipgloss.Width(label)-6)
			sb.WriteString(m.listItem(i == m.profileIndex, t.focus, label, summary) + "\n")
		}
		if quick != "" {
			sb.WriteString("\n" + t.fg(t.subtle).Render("★ used by Quick start"))
		}
	}

	if m.confirmDelete && m.profileIndex < len(profiles) {
		sb.WriteString("\n\n" + t.fg(t.danger).Bold(true).Render(fmt.Sprintf("Delete %q?", profiles[m.profileIndex].Name)) +
			t.fg(t.muted).Render("  y delete · any other key cancel"))
	}
	sb.WriteString(m.statusLine())

	help := []helpItem{{"n", "new"}, {"esc", "back"}}
	if len(profiles) > 0 {
		help = []helpItem{{"enter", "start"}, {"n", "new"}, {"e", "edit"}, {"s", "quick start"}, {"d", "delete"}, {"esc", "back"}}
	}
	return m.frame(t.focus, "Profiles", sb.String(), help)
}

func (m model) formTitle(step int) string {
	t := m.theme
	title := "New profile"
	if m.formEditIndex >= 0 {
		title = "Edit profile"
	}
	return row(t.fg(t.text).Bold(true).Render(title), t.fg(t.subtle).Render(fmt.Sprintf("Step %d of 2", step)), m.contentWidth())
}

func (m model) fieldLabel(text string, focused bool, width int) string {
	t := m.theme
	style := t.fg(t.muted)
	if focused {
		style = t.fg(t.text).Bold(true)
	}
	return style.Width(width).Render(text)
}

func (m model) viewConfigName() string {
	t := m.theme
	var sb strings.Builder
	sb.WriteString(m.formTitle(1) + "\n\n")

	labels := []string{"Name", "Focus groups", "Long break"}
	suffixes := []string{"", "1–8", "minutes"}
	for i, label := range labels {
		line := m.fieldLabel(label, i == m.configStep1Focus, 15) + m.configInputsStep1[i].View()
		if suffixes[i] != "" {
			line += " " + t.fg(t.subtle).Render(suffixes[i])
		}
		sb.WriteString(line + "\n\n")
	}
	sb.WriteString(m.errorLine(m.configError))

	return m.frame(t.focus, "Profiles", strings.TrimRight(sb.String(), "\n"), []helpItem{
		{"tab", "next field"}, {"enter", "continue"}, {"esc", "cancel"},
	})
}

func (m model) viewConfigGroups() string {
	t := m.theme
	var sb strings.Builder
	sb.WriteString(m.formTitle(2) + "\n")
	sb.WriteString(t.fg(t.muted).Render(fmt.Sprintf("%s · long break %s", m.configProfileName, formatMinutes(m.configLongBreak))) + "\n\n")

	head := t.fg(t.subtle).Bold(true)
	sb.WriteString(head.Width(10).Render("GROUP") + head.Width(14).Render("FOCUS") + head.Render("BREAK") + "\n")
	for i := 0; i < m.configNumGroups; i++ {
		wIdx, bIdx := 2*i, 2*i+1
		rowFocused := m.configStep2Focus == wIdx || m.configStep2Focus == bIdx
		label := m.fieldLabel(fmt.Sprintf("#%d", i+1), rowFocused, 10)
		sb.WriteString(label +
			lipgloss.NewStyle().Width(14).Render(m.configInputsStep2[wIdx].View()) +
			m.configInputsStep2[bIdx].View() + "\n")
	}
	sb.WriteString("\n" + t.fg(t.subtle).Render("All durations are in minutes."))
	sb.WriteString(m.errorLine(m.configError))

	return m.frame(t.focus, "Profiles", sb.String(), []helpItem{
		{"tab", "next"}, {"enter", "save profile"}, {"esc", "back"},
	})
}

func (m model) viewSoundSettings() string {
	t := m.theme
	var sb strings.Builder
	sb.WriteString(t.fg(t.muted).Render("Played when a phase ends.") + "\n\n")

	current := m.sound()
	colWidth := m.contentWidth() / 2
	cell := func(i int) string {
		name := systemSounds[i]
		mark := "  "
		if name == current {
			mark = " " + t.fg(t.focus).Render("★")
		}
		if i == m.soundIndex {
			return lipgloss.NewStyle().Width(colWidth).Render(t.fg(t.focus).Render("▌ ") + t.fg(t.text).Bold(true).Render(name) + mark)
		}
		return lipgloss.NewStyle().Width(colWidth).Render("  " + t.fg(t.muted).Render(name) + mark)
	}
	for r := 0; r < soundRows; r++ {
		line := cell(r)
		if r+soundRows < len(systemSounds) {
			line += cell(r + soundRows)
		}
		sb.WriteString(line + "\n")
	}
	sb.WriteString("\n" + t.fg(t.subtle).Render("★ current sound"))

	return m.frame(t.focus, "Settings › Sound", sb.String(), []helpItem{
		{"↑/↓/←/→", "move"}, {"space", "preview"}, {"enter", "save"}, {"esc", "cancel"},
	})
}

func (m model) viewSpotifySettings() string {
	t := m.theme
	accent := t.spotify
	w := m.contentWidth()
	if m.devicePicker.open {
		return m.viewDevicePicker(accent, "Settings › Spotify", "Where music plays by default. Automatic uses the device that is already playing, or else this computer.")
	}
	var sb strings.Builder

	pill := func(on bool) string {
		if on {
			return t.fg(t.spotify).Bold(true).Render("● On")
		}
		return t.fg(t.subtle).Render("○ Off")
	}

	sb.WriteString(row(m.spotifyRowLabel(0, "Autoplay during focus"), pill(m.spotifyEnabledDraft), w) + "\n\n")

	sb.WriteString(m.spotifyRowLabel(1, "Playlist / track URI") + "\n")
	sb.WriteString("  " + m.spotifyInputs[0].View() + "\n\n")

	device := m.spotifyDeviceDraft.label()
	sb.WriteString(row(m.spotifyRowLabel(2, "Playback device"), t.fg(t.muted).Render(truncate(device, w-24)), w) + "\n\n")

	credValue := t.fg(t.danger).Render("missing")
	if id := m.savedConfig.SpotifyClientID; id != "" {
		credValue = t.fg(t.muted).Render("Client ID …" + id[max(0, len(id)-4):])
	}
	sb.WriteString(row(m.spotifyRowLabel(3, "API credentials"), credValue, w) + "\n\n")

	account := t.fg(t.warn).Render("○ Not linked")
	if m.savedConfig.SpotifyRefreshToken != "" {
		account = t.fg(t.spotify).Render("● Linked")
	}
	sb.WriteString(row(m.spotifyRowLabel(4, "Authorize account"), account, w) + "\n\n")

	save := "Save"
	if m.spotifyFocusIndex == 5 {
		sb.WriteString(t.fg(accent).Render("▌ ") + lipgloss.NewStyle().Bold(true).Foreground(t.text).Render("[ "+save+" ]"))
	} else {
		sb.WriteString("  " + t.fg(t.muted).Render("[ "+save+" ]"))
	}
	if m.spotifyDirty() {
		sb.WriteString("   " + t.fg(t.warn).Render("● Unsaved changes"))
	}
	sb.WriteString("\n")

	sb.WriteString(m.errorLine(m.configError))
	sb.WriteString(m.statusLine())
	sb.WriteString("\n" + t.fg(t.subtle).Width(w).Render("Requires Spotify Premium. Open Spotify on any device before starting a session."))

	escDesc := "back"
	if m.spotifyDirty() {
		escDesc = "discard"
	}
	return m.frame(accent, "Settings › Spotify", sb.String(), []helpItem{
		{"↑/↓", "move"}, {"enter", "select"}, {"space", "toggle"}, {"esc", escDesc},
	})
}

func (m model) spotifyRowLabel(i int, label string) string {
	t := m.theme
	if m.spotifyFocusIndex == i {
		return t.fg(t.spotify).Render("▌ ") + t.fg(t.text).Bold(true).Render(label)
	}
	return "  " + t.fg(t.muted).Render(label)
}

func (m model) viewSpotifyCredentials() string {
	t := m.theme
	var sb strings.Builder
	sb.WriteString(t.fg(t.muted).Width(m.contentWidth()).Render("From your app at developer.spotify.com. Its redirect URI must be "+spotifyRedirectURI) + "\n\n")

	labels := []string{"Client ID", "Client Secret"}
	for i, label := range labels {
		sb.WriteString(m.fieldLabel(label, i == m.spotifyCredFocus, 0) + "\n")
		sb.WriteString(m.spotifyCredInputs[i].View() + "\n\n")
	}
	sb.WriteString(m.errorLine(m.configError))

	return m.frame(t.spotify, "Settings › Spotify", strings.TrimRight(sb.String(), "\n"), []helpItem{
		{"tab", "switch field"}, {"enter", "save"}, {"esc", "cancel"},
	})
}

func (m model) viewSpotifyAuthWaiting() string {
	t := m.theme
	w := m.contentWidth()
	var sb strings.Builder
	sb.WriteString(t.fg(t.text).Bold(true).Render("Waiting for Spotify…") + "\n\n")
	sb.WriteString(t.fg(t.muted).Width(w).Render("Approve access in the browser window that just opened. This page updates automatically when you're done.") + "\n\n")

	link := lipgloss.NewStyle().Foreground(t.spotify).Underline(true).Hyperlink(m.spotifyAuthURL).
		Render(truncate(m.spotifyAuthURL, w))
	sb.WriteString(link + "\n\n")
	sb.WriteString(t.fg(t.subtle).Render(fmt.Sprintf("Listening on %s · expires at %s", spotifyRedirectURI, m.spotifyAuthDeadline.Format("15:04"))))
	sb.WriteString(m.statusLine())

	return m.frame(t.spotify, "Settings › Spotify", sb.String(), []helpItem{
		{"o", "open again"}, {"c", "copy link"}, {"esc", "cancel"},
	})
}

func (m model) viewConfigPathSettings() string {
	t := m.theme
	w := m.contentWidth()
	var sb strings.Builder
	sb.WriteString(t.fg(t.text).Bold(true).Render("Where should gomodoro keep its config?") + "\n\n")
	sb.WriteString(m.configPathInput.View() + "\n\n")

	note := t.fg(t.muted)
	sb.WriteString(note.Width(w).Render("• New file: your current settings are copied there.") + "\n")
	sb.WriteString(note.Width(w).Render("• Existing gomodoro config: it is used as-is.") + "\n")
	sb.WriteString(note.Width(w).Render("• Any other existing file is never overwritten.") + "\n")
	sb.WriteString(m.errorLine(m.configError))

	return m.frame(t.focus, "Settings › Config file", strings.TrimRight(sb.String(), "\n"), []helpItem{
		{"enter", "save"}, {"esc", "cancel"},
	})
}

func (m model) sessionDots(accent lipgloss.Style) string {
	t := m.theme
	var dots []string
	for i := range m.activeProfile.Groups {
		switch {
		case i < m.completedGroups:
			dots = append(dots, t.fg(t.focus).Render("●"))
		case i == m.currentGroup && m.currentSub == subPhaseWork:
			dots = append(dots, accent.Render("◉"))
		default:
			dots = append(dots, t.fg(t.subtle).Render("○"))
		}
	}
	return strings.Join(dots, " ")
}

func (m model) spotifyStatusLine() string {
	t := m.theme
	cfg := m.savedConfig
	if !cfg.SpotifyEnabled {
		return ""
	}
	label := t.fg(t.spotify).Render("♫ Spotify") + t.fg(t.subtle).Render(" · ")
	text := func(style lipgloss.Style, s string) string {
		width := max(10, m.contentWidth()-lipgloss.Width(label))
		return lipgloss.JoinHorizontal(lipgloss.Top, label, style.Width(width).Render(s))
	}
	switch {
	case cfg.SpotifyRefreshToken == "":
		return text(t.fg(t.warn), "not linked — Settings › Spotify")
	case cfg.SpotifyURI == "":
		return text(t.fg(t.warn), "no playlist set — Settings › Spotify")
	case m.spotifyError != "":
		return text(t.fg(t.danger), m.spotifyError)
	}
	state := "ready"
	switch {
	case m.spotifyBusy && m.spotifyInFlight == spotifyActionPlay:
		state = "starting…"
	case m.spotifyBusy && m.spotifyInFlight == spotifyActionPause:
		state = "pausing…"
	case m.spotifyState == spotifyPlaying:
		state = "playing"
		if m.spotifyDevice != "" {
			state += " on " + m.spotifyDevice
		}
	case m.currentSub != subPhaseWork:
		state = "paused for the break · resumes at focus"
	case m.spotifyState == spotifyPaused:
		state = "paused"
	}
	switch {
	case m.spotifyState == spotifyPlaying && m.spotifyMissingDevice != "":
		state += " · " + m.spotifyMissingDevice + " not found"
	case m.spotifyState != spotifyPlaying && !m.spotifyTarget.isAuto():
		state += " · on " + m.spotifyTarget.Name
	}
	if m.spotifyVolumeKnown {
		state += fmt.Sprintf(" · vol %d%%", m.spotifyVolume)
	}
	return text(t.fg(t.muted), state)
}

func (m model) timerPercent() float64 {
	if m.totalDuration <= 0 {
		return 1
	}
	p := 1 - float64(m.remaining())/float64(m.totalDuration)
	return max(0, min(1, p))
}

func (m model) viewTimer() string {
	if m.devicePicker.open {
		return m.viewDevicePicker(m.theme.spotify, "Spotify device", "Where music plays for this session. Set the default in Settings › Spotify.")
	}
	if m.cycleFinished {
		return m.viewCycleFinished()
	}

	t := m.theme
	w := m.contentWidth()
	phaseColor := t.phaseColor(m.currentSub)
	accent := t.fg(phaseColor)
	center := lipgloss.NewStyle().Width(w).Align(lipgloss.Center)

	badge := lipgloss.NewStyle().Foreground(phaseColor).Bold(true).Render("● " + strings.ToUpper(m.currentSub.String()))
	groupInfo := ""
	if m.currentSub != subPhaseLongBreak {
		groupInfo = fmt.Sprintf("Group %d of %d", m.currentGroup+1, len(m.activeProfile.Groups))
	}
	header := row(badge, t.fg(t.muted).Render(groupInfo), w)

	clockText := formatClock(m.remaining())
	clockStyle := accent.Bold(true)
	if !m.active {
		clockStyle = t.fg(t.muted)
	}
	compact := m.height > 0 && m.height < 30
	clock := bigClock(clockText, compact)
	if lipgloss.Width(clock) > w || (m.height > 0 && m.height < 22) {
		clock = clockText
	}
	clock = center.Render(clockStyle.Render(clock))

	percent := m.timerPercent()
	barWidth := w - 6
	filled := int(float64(barWidth) * percent)
	bar := accent.Render(strings.Repeat("━", filled)) + t.fg(t.border).Render(strings.Repeat("━", barWidth-filled))
	bar += t.fg(t.muted).Render(fmt.Sprintf(" %3d%%", int(percent*100)))

	var state string
	switch {
	case m.confirmQuit:
		state = t.fg(t.danger).Bold(true).Render("End this session and quit?") + t.fg(t.muted).Render("  y yes · n no")
	case m.phaseReady:
		msg := "Time to focus — press space to start"
		if m.currentSub != subPhaseWork {
			msg = m.currentSub.String() + " time — press space to start"
		}
		state = accent.Bold(true).Render("▸ " + msg)
	case m.active:
		state = accent.Render("● Running")
	default:
		state = t.fg(t.muted).Render("○ Paused")
	}

	stats := m.sessionDots(accent) + t.fg(t.subtle).Render(fmt.Sprintf("   %d/%d focus done", m.completedGroups, len(m.activeProfile.Groups)))

	var sb strings.Builder
	sb.WriteString(header + "\n\n")
	sb.WriteString(clock + "\n\n")
	sb.WriteString(bar + "\n\n")
	sb.WriteString(center.Render(state) + "\n\n")
	sb.WriteString(stats)
	if sp := m.spotifyStatusLine(); sp != "" {
		sb.WriteString("\n" + sp)
	}

	playLabel := "start"
	if m.active {
		playLabel = "pause"
	}
	help := []helpItem{{"space", playLabel}, {"s", "skip"}, {"r", "restart phase"}}
	if m.savedConfig.SpotifyEnabled {
		help = append(help, helpItem{"+/-", "volume"}, helpItem{"d", "device"})
	}
	help = append(help, helpItem{"esc", "menu"}, helpItem{"q", "quit"})

	return m.frame(phaseColor, m.activeProfile.Name, sb.String(), help)
}

func (m model) viewCycleFinished() string {
	t := m.theme
	w := m.contentWidth()
	center := lipgloss.NewStyle().Width(w).Align(lipgloss.Center)

	focusMin := 0
	for _, g := range m.activeProfile.Groups {
		focusMin += g.WorkMin
	}

	var sb strings.Builder
	sb.WriteString(center.Render(t.fg(t.success).Bold(true).Render("✓ Cycle complete")) + "\n\n")
	sb.WriteString(center.Render(strings.Repeat("🍅", m.completedGroups)) + "\n\n")
	sb.WriteString(center.Render(t.fg(t.muted).Render(fmt.Sprintf("%d focus sessions · %s of focus", m.completedGroups, formatMinutes(focusMin)))) + "\n\n")
	sb.WriteString(center.Render(t.fg(t.text).Render("Start another cycle?")))

	return m.frame(t.success, m.activeProfile.Name, sb.String(), []helpItem{
		{"y", "start again"}, {"n", "back to menu"}, {"q", "quit"},
	})
}

func (m model) windowTitle() string {
	if m.state != stateTimer || !m.sessionActive {
		return "gomodoro"
	}
	if m.cycleFinished {
		return "✓ Cycle complete · gomodoro"
	}
	icon := "🍅"
	if !m.active {
		icon = "⏸"
	}
	return fmt.Sprintf("%s %s %s · gomodoro", icon, formatClock(m.remaining()), m.currentSub)
}

func (m model) View() tea.View {
	if m.quitting {
		return tea.NewView("\n  " + m.theme.fg(m.theme.muted).Render("Thanks for using gomodoro. Keep focusing! 🍅") + "\n\n")
	}

	var content string
	switch m.state {
	case stateMenu:
		content = m.viewMenu()
	case stateSelectProfile:
		content = m.viewSelectProfile()
	case stateSettingsMenu:
		content = m.viewSettingsMenu()
	case stateConfigName:
		content = m.viewConfigName()
	case stateConfigGroups:
		content = m.viewConfigGroups()
	case stateSoundSettings:
		content = m.viewSoundSettings()
	case stateSpotifySettings:
		content = m.viewSpotifySettings()
	case stateSpotifyCredentials:
		content = m.viewSpotifyCredentials()
	case stateSpotifyAuthWaiting:
		content = m.viewSpotifyAuthWaiting()
	case stateConfigPathSettings:
		content = m.viewConfigPathSettings()
	case stateTimer:
		content = m.viewTimer()
	}

	v := tea.NewView(content)
	v.AltScreen = true
	v.WindowTitle = m.windowTitle()
	if m.state == stateTimer && m.sessionActive && !m.cycleFinished {
		state := tea.ProgressBarDefault
		if !m.active {
			state = tea.ProgressBarWarning
		}
		v.ProgressBar = tea.NewProgressBar(state, int(m.timerPercent()*100))
	}
	return v
}
