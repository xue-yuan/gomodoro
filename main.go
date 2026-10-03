package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

type appState int

const (
	stateMenu appState = iota
	stateSelectProfile
	stateSettingsMenu
	stateConfigName
	stateConfigGroups
	stateSoundSettings
	stateSpotifySettings
	stateSpotifyCredentials
	stateSpotifyAuthWaiting
	stateConfigPathSettings
	stateTimer
)

type subPhase int

const (
	subPhaseWork subPhase = iota
	subPhaseBreak
	subPhaseLongBreak
)

func (s subPhase) String() string {
	switch s {
	case subPhaseWork:
		return "Focus"
	case subPhaseBreak:
		return "Short break"
	case subPhaseLongBreak:
		return "Long break"
	}
	return "Unknown"
}

type tickMsg struct {
	id int
}

type menuItem struct {
	id    string
	label string
	value string
}

type model struct {
	state       appState
	theme       theme
	menuIndex   int
	savedConfig *Config
	configPath  string
	saveError   string
	statusMsg string

	settingsMenuIndex int

	profileIndex  int
	confirmDelete bool

	formEditIndex     int
	formReturnState   appState
	configInputsStep1 []textinput.Model
	configStep1Focus  int
	configInputsStep2 []textinput.Model
	configStep2Focus  int
	configNumGroups   int
	configProfileName string
	configLongBreak   int

	configError     string
	configPathInput textinput.Model

	soundIndex int

	spotifyFocusIndex   int
	spotifyInputs       []textinput.Model
	spotifyEnabledDraft bool
	spotifyDeviceDraft  spotifyDeviceRef
	spotifyCredInputs   []textinput.Model
	spotifyCredFocus    int
	spotifyAuthCancel   context.CancelFunc
	spotifyAuthURL      string
	spotifyAuthDeadline time.Time

	spotifyError  string
	spotifyState  spotifyPlayState
	spotifyDevice string
	spotifyTarget        spotifyDeviceRef
	spotifyMissingDevice string
	spotifyCheckSeq      int
	spotifyStalled       bool
	devicePicker         devicePicker
	spotifyLoaded bool
	spotifyRequested bool
	spotifyBusy        bool
	spotifyInFlight    spotifyAction
	spotifyWantPlay    *bool
	spotifyWantVolume  *int
	spotifyVolume      int
	spotifyVolumeKnown bool
	volumePending      bool
	volumeSeq          int

	sessionActive   bool
	activeProfile   Profile
	currentGroup    int
	currentSub      subPhase
	completedGroups int
	cycleFinished   bool
	phaseReady  bool
	confirmQuit bool

	timeRemaining time.Duration
	deadline      time.Time
	totalDuration time.Duration
	active        bool
	tickID        int
	notifier      Notifier
	quitting      bool

	width  int
	height int
}

const defaultSound = "Glass"

var systemSounds = []string{
	"Basso",
	"Blow",
	"Bottle",
	"Frog",
	"Funk",
	"Glass",
	"Hero",
	"Morse",
	"Ping",
	"Pop",
	"Purr",
	"Sosumi",
	"Submarine",
	"Tink",
}

func validSound(sound string) string {
	for _, s := range systemSounds {
		if s == sound {
			return sound
		}
	}
	return defaultSound
}

func initialModel(cfg *Config) model {
	if cfg == nil {
		cfg = &Config{}
	}
	return model{
		state:         stateMenu,
		theme:         newTheme(true),
		savedConfig:   cfg,
		configPath:    GetConfigPath(),
		notifier:      NewNotifier(),
		formEditIndex: -1,
	}
}

func (m *model) sound() string {
	return validSound(m.savedConfig.Sound)
}

func (m *model) persist() {
	if err := SaveConfig(m.savedConfig); err != nil {
		m.saveError = "Failed to save config: " + err.Error()
	} else {
		m.saveError = ""
	}
}

func (m *model) newInput(placeholder string, charLimit, width int) textinput.Model {
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = placeholder
	in.CharLimit = charLimit
	in.SetWidth(width)

	styles := textinput.DefaultStyles(m.theme.isDark)
	styles.Focused.Prompt = m.theme.fg(m.theme.focus).Bold(true)
	styles.Focused.Text = m.theme.fg(m.theme.text)
	styles.Blurred.Prompt = m.theme.fg(m.theme.subtle)
	styles.Blurred.Text = m.theme.fg(m.theme.muted)
	styles.Focused.Placeholder = m.theme.fg(m.theme.subtle)
	styles.Blurred.Placeholder = m.theme.fg(m.theme.subtle)
	styles.Cursor.Color = m.theme.focus
	in.SetStyles(styles)
	return in
}

func (m model) Init() tea.Cmd {
	return tea.RequestBackgroundColor
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.BackgroundColorMsg:
		m.theme = newTheme(msg.IsDark())
		return m, nil

	case tea.KeyPressMsg:
		m.statusMsg = ""

	case spotifyAuthResultMsg:
		waiting := m.state == stateSpotifyAuthWaiting
		if waiting {
			m.state = stateSpotifySettings
			m.spotifyAuthCancel = nil
		}
		switch {
		case errors.Is(msg.err, context.Canceled):
			m.configError = ""
		case errors.Is(msg.err, context.DeadlineExceeded):
			m.configError = "Authorization timed out. Please try again."
		case msg.err != nil:
			if waiting {
				m.configError = "Authorization failed: " + msg.err.Error()
			}
		default:
			m.storeSpotifyToken(msg.token, msg.fetchedAt)
			m.configError = ""
			m.statusMsg = "Spotify account linked."
		}
		return m, nil

	case spotifyResultMsg:
		return m, m.handleSpotifyResult(msg)

	case spotifyDevicesMsg:
		m.receiveSpotifyDevices(msg)
		return m, nil

	case playbackCheckMsg:
		return m, m.handlePlaybackCheck(msg)

	case volumeApplyMsg:
		return m, m.applyVolume(msg)
	}

	switch m.state {
	case stateMenu:
		return m.updateMenu(msg)
	case stateSelectProfile:
		return m.updateSelectProfile(msg)
	case stateSettingsMenu:
		return m.updateSettingsMenu(msg)
	case stateConfigName:
		return m.updateConfigName(msg)
	case stateConfigGroups:
		return m.updateConfigGroups(msg)
	case stateSoundSettings:
		return m.updateSoundSettings(msg)
	case stateSpotifySettings:
		return m.updateSpotifySettings(msg)
	case stateSpotifyCredentials:
		return m.updateSpotifyCredentials(msg)
	case stateSpotifyAuthWaiting:
		return m.updateSpotifyAuthWaiting(msg)
	case stateConfigPathSettings:
		return m.updateConfigPathSettings(msg)
	case stateTimer:
		return m.updateTimer(msg)
	}

	return m, nil
}

func defaultProfile() Profile {
	groups := make([]PomodoroGroup, 4)
	for i := range groups {
		groups[i] = PomodoroGroup{WorkMin: 25, BreakMin: 5}
	}
	return Profile{
		Name:         "Classic",
		Groups:       groups,
		LongBreakMin: 20,
	}
}

func (m *model) quickStartProfile() Profile {
	if name := m.savedConfig.QuickStartProfile; name != "" {
		for _, p := range m.savedConfig.Profiles {
			if p.Name == name {
				return p
			}
		}
	}
	return defaultProfile()
}

func formatMinutes(min int) string {
	if min >= 60 && min%60 == 0 {
		return fmt.Sprintf("%dh", min/60)
	}
	if min > 60 {
		return fmt.Sprintf("%dh%02dm", min/60, min%60)
	}
	return fmt.Sprintf("%dm", min)
}

func profileSummary(p Profile) string {
	uniform := true
	focusTotal := 0
	for _, g := range p.Groups {
		focusTotal += g.WorkMin
		if g != p.Groups[0] {
			uniform = false
		}
	}
	if len(p.Groups) == 0 {
		return "no groups"
	}
	var groups string
	if uniform {
		groups = fmt.Sprintf("%d × %s/%s", len(p.Groups), formatMinutes(p.Groups[0].WorkMin), formatMinutes(p.Groups[0].BreakMin))
	} else {
		groups = fmt.Sprintf("%d groups · %s focus", len(p.Groups), formatMinutes(focusTotal))
	}
	return fmt.Sprintf("%s · long %s", groups, formatMinutes(p.LongBreakMin))
}

func formatClock(d time.Duration) string {
	totalSeconds := int((d + time.Second - 1) / time.Second)
	return fmt.Sprintf("%02d:%02d", totalSeconds/60, totalSeconds%60)
}

func (m model) mainMenuItems() []menuItem {
	var items []menuItem
	if m.sessionActive {
		desc := fmt.Sprintf("%s · %s · %s left", m.activeProfile.Name, m.currentSub, formatClock(m.remaining()))
		if m.cycleFinished {
			desc = m.activeProfile.Name + " · cycle complete"
		}
		items = append(items, menuItem{id: "resume", label: "Resume session", value: desc})
	}
	qs := m.quickStartProfile()
	profilesDesc := "No saved profiles yet"
	if n := len(m.savedConfig.Profiles); n > 0 {
		profilesDesc = fmt.Sprintf("%d saved", n)
	}
	return append(items,
		menuItem{id: "quick", label: "Quick start", value: qs.Name + " · " + profileSummary(qs)},
		menuItem{id: "profiles", label: "Profiles", value: profilesDesc},
		menuItem{id: "settings", label: "Settings", value: "Sound, auto-start, Spotify, config file"},
		menuItem{id: "quit", label: "Quit", value: ""},
	)
}

func wrapIndex(i, n int) int {
	if n == 0 {
		return 0
	}
	return ((i % n) + n) % n
}

func (m *model) startProfile(p Profile) tea.Cmd {
	m.activeProfile = p
	m.sessionActive = true
	m.completedGroups = 0
	m.cycleFinished = false
	m.confirmQuit = false
	m.spotifyVolume = m.savedConfig.Volume()
	m.spotifyVolumeKnown = false
	m.spotifyLoaded = false
	m.spotifyRequested = false
	m.spotifyWantPlay = nil
	m.spotifyWantVolume = nil
	m.spotifyState = spotifyIdle
	m.spotifyError = ""
	m.spotifyTarget = m.savedConfig.SpotifyDevice()
	m.spotifyMissingDevice = ""
	m.devicePicker = devicePicker{}
	m.startGroupTimer(0, subPhaseWork)
	m.state = stateTimer
	if m.savedConfig.AutoStart {
		return tea.Batch(m.resume(), m.triggerSpotifyPlay())
	}
	m.phaseReady = true
	return nil
}

func (m model) updateMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	items := m.mainMenuItems()
	m.menuIndex = wrapIndex(m.menuIndex, len(items))

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "q", "ctrl+c":
		return m, m.pauseAndQuitCmd()
	case "up", "k":
		m.menuIndex = wrapIndex(m.menuIndex-1, len(items))
	case "down", "j":
		m.menuIndex = wrapIndex(m.menuIndex+1, len(items))
	case "enter":
		switch items[m.menuIndex].id {
		case "resume":
			m.state = stateTimer
			return m, nil
		case "quick":
			return m, m.startProfile(m.quickStartProfile())
		case "profiles":
			m.state = stateSelectProfile
			m.profileIndex = 0
			m.confirmDelete = false
		case "settings":
			m.state = stateSettingsMenu
			m.settingsMenuIndex = 0
		case "quit":
			return m, m.pauseAndQuitCmd()
		}
	}
	return m, nil
}

func (m *model) backToMenu(id string) {
	m.state = stateMenu
	for i, it := range m.mainMenuItems() {
		if it.id == id {
			m.menuIndex = i
		}
	}
}

func (m model) updateSelectProfile(msg tea.Msg) (tea.Model, tea.Cmd) {
	profiles := m.savedConfig.Profiles
	numProfiles := len(profiles)

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}

	if m.confirmDelete {
		m.confirmDelete = false
		if key.String() == "y" && m.profileIndex < numProfiles {
			name := profiles[m.profileIndex].Name
			m.savedConfig.Profiles = append(profiles[:m.profileIndex], profiles[m.profileIndex+1:]...)
			if m.savedConfig.QuickStartProfile == name {
				m.savedConfig.QuickStartProfile = ""
			}
			m.persist()
			m.profileIndex = min(m.profileIndex, max(0, len(m.savedConfig.Profiles)-1))
			m.statusMsg = fmt.Sprintf("Deleted %q.", name)
		}
		return m, nil
	}

	switch key.String() {
	case "esc":
		m.backToMenu("profiles")
	case "up", "k":
		m.profileIndex = wrapIndex(m.profileIndex-1, numProfiles)
	case "down", "j":
		m.profileIndex = wrapIndex(m.profileIndex+1, numProfiles)
	case "n":
		return m, m.openProfileForm(-1, stateSelectProfile)
	case "e":
		if m.profileIndex < numProfiles {
			return m, m.openProfileForm(m.profileIndex, stateSelectProfile)
		}
	case "s":
		if m.profileIndex < numProfiles {
			name := profiles[m.profileIndex].Name
			if m.savedConfig.QuickStartProfile == name {
				m.savedConfig.QuickStartProfile = ""
				m.statusMsg = "Quick start reset to Classic."
			} else {
				m.savedConfig.QuickStartProfile = name
				m.statusMsg = fmt.Sprintf("Quick start now uses %q.", name)
			}
			m.persist()
		}
	case "d":
		if m.profileIndex < numProfiles {
			m.confirmDelete = true
		}
	case "enter":
		if m.profileIndex < numProfiles {
			return m, m.startProfile(profiles[m.profileIndex])
		}
		return m, m.openProfileForm(-1, stateSelectProfile)
	}
	return m, nil
}

func (m model) settingsItems() []menuItem {
	onOff := func(b bool) string {
		if b {
			return "On"
		}
		return "Off"
	}
	spotify := "Off"
	if m.savedConfig.SpotifyEnabled {
		spotify = "On"
	}
	if m.savedConfig.SpotifyRefreshToken != "" {
		spotify += " · linked"
	} else {
		spotify += " · not linked"
	}
	path := m.configPath
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home) {
		path = "~" + strings.TrimPrefix(path, home)
	}
	return []menuItem{
		{id: "sound", label: "Notification sound", value: m.sound()},
		{id: "autostart", label: "Auto-start next phase", value: onOff(m.savedConfig.AutoStart)},
		{id: "spotify", label: "Spotify autoplay", value: spotify},
		{id: "configpath", label: "Config file", value: path},
	}
}

func (m model) updateSettingsMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	items := m.settingsItems()
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "esc", "q":
		m.backToMenu("settings")
	case "up", "k":
		m.settingsMenuIndex = wrapIndex(m.settingsMenuIndex-1, len(items))
	case "down", "j":
		m.settingsMenuIndex = wrapIndex(m.settingsMenuIndex+1, len(items))
	case "enter", "space":
		switch items[m.settingsMenuIndex].id {
		case "sound":
			m.state = stateSoundSettings
			current := m.sound()
			m.soundIndex = 0
			for i, s := range systemSounds {
				if s == current {
					m.soundIndex = i
					break
				}
			}
		case "autostart":
			m.savedConfig.AutoStart = !m.savedConfig.AutoStart
			m.persist()
		case "spotify":
			m.state = stateSpotifySettings
			m.initSpotifyInputs()
			return m, textinput.Blink
		case "configpath":
			m.state = stateConfigPathSettings
			m.configPathInput = m.newInput("~/Dropbox/gomodoro/profiles.json", 512, m.contentWidth()-2)
			m.configPathInput.SetValue(m.configPath)
			m.configPathInput.Focus()
			m.configError = ""
			return m, textinput.Blink
		}
	}
	return m, nil
}

func (m *model) openProfileForm(editIndex int, returnTo appState) tea.Cmd {
	m.formEditIndex = editIndex
	m.formReturnState = returnTo
	m.state = stateConfigName
	m.configStep1Focus = 0
	m.configError = ""

	name, groups, longBreak := "", "4", "20"
	if editIndex >= 0 && editIndex < len(m.savedConfig.Profiles) {
		p := m.savedConfig.Profiles[editIndex]
		name, groups, longBreak = p.Name, strconv.Itoa(len(p.Groups)), strconv.Itoa(p.LongBreakMin)
	}

	m.configInputsStep1 = []textinput.Model{
		m.newInput("e.g. Deep Work", 20, 22),
		m.newInput("1-8", 1, 4),
		m.newInput("min", 3, 6),
	}
	m.configInputsStep1[0].SetValue(name)
	m.configInputsStep1[1].SetValue(groups)
	m.configInputsStep1[2].SetValue(longBreak)
	m.configInputsStep1[0].Focus()
	return textinput.Blink
}

func (m model) updateConfigName(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		if m.configStep1Focus == 1 || m.configStep1Focus == 2 {
			k := key.String()
			if len(k) == 1 && (k[0] < '0' || k[0] > '9') {
				return m, nil
			}
		}

		switch key.String() {
		case "esc":
			m.state = m.formReturnState
			return m, nil
		case "tab", "down":
			m.configInputsStep1[m.configStep1Focus].Blur()
			m.configStep1Focus = wrapIndex(m.configStep1Focus+1, 3)
			m.configInputsStep1[m.configStep1Focus].Focus()
			return m, nil
		case "shift+tab", "up":
			m.configInputsStep1[m.configStep1Focus].Blur()
			m.configStep1Focus = wrapIndex(m.configStep1Focus-1, 3)
			m.configInputsStep1[m.configStep1Focus].Focus()
			return m, nil
		case "enter":
			name := strings.TrimSpace(m.configInputsStep1[0].Value())
			numGroups, errG := strconv.Atoi(m.configInputsStep1[1].Value())
			longBreakMin, errL := strconv.Atoi(m.configInputsStep1[2].Value())

			switch {
			case name == "":
				m.configError = "Profile name cannot be empty."
				return m, nil
			case m.profileNameTaken(name):
				m.configError = fmt.Sprintf("A profile named %q already exists.", name)
				return m, nil
			case errG != nil || numGroups < 1 || numGroups > 8:
				m.configError = "Groups must be a number from 1 to 8."
				return m, nil
			case errL != nil || longBreakMin <= 0:
				m.configError = "Long break must be a positive number of minutes."
				return m, nil
			}

			m.configProfileName = name
			m.configNumGroups = numGroups
			m.configLongBreak = longBreakMin

			var existing []PomodoroGroup
			if m.formEditIndex >= 0 && m.formEditIndex < len(m.savedConfig.Profiles) {
				existing = m.savedConfig.Profiles[m.formEditIndex].Groups
			}
			m.configInputsStep2 = make([]textinput.Model, 2*numGroups)
			for i := 0; i < numGroups; i++ {
				g := PomodoroGroup{WorkMin: 25, BreakMin: 5}
				if i < len(existing) {
					g = existing[i]
				}
				m.configInputsStep2[2*i] = m.newInput("min", 3, 5)
				m.configInputsStep2[2*i].SetValue(strconv.Itoa(g.WorkMin))
				m.configInputsStep2[2*i+1] = m.newInput("min", 3, 5)
				m.configInputsStep2[2*i+1].SetValue(strconv.Itoa(g.BreakMin))
			}
			m.configInputsStep2[0].Focus()
			m.configStep2Focus = 0
			m.configError = ""
			m.state = stateConfigGroups
			return m, textinput.Blink
		}
	}

	var cmd tea.Cmd
	m.configInputsStep1[m.configStep1Focus], cmd = m.configInputsStep1[m.configStep1Focus].Update(msg)
	return m, cmd
}

func (m *model) profileNameTaken(name string) bool {
	for i, p := range m.savedConfig.Profiles {
		if i != m.formEditIndex && strings.EqualFold(p.Name, name) {
			return true
		}
	}
	return false
}

func (m model) updateConfigGroups(msg tea.Msg) (tea.Model, tea.Cmd) {
	numFields := 2 * m.configNumGroups

	if key, ok := msg.(tea.KeyPressMsg); ok {
		k := key.String()
		if len(k) == 1 && (k[0] < '0' || k[0] > '9') {
			return m, nil
		}

		switch k {
		case "esc":
			m.state = stateConfigName
			return m, nil
		case "tab", "down", "right":
			m.configInputsStep2[m.configStep2Focus].Blur()
			m.configStep2Focus = wrapIndex(m.configStep2Focus+1, numFields)
			m.configInputsStep2[m.configStep2Focus].Focus()
			return m, nil
		case "shift+tab", "up", "left":
			m.configInputsStep2[m.configStep2Focus].Blur()
			m.configStep2Focus = wrapIndex(m.configStep2Focus-1, numFields)
			m.configInputsStep2[m.configStep2Focus].Focus()
			return m, nil
		case "enter":
			var groups []PomodoroGroup
			for i := 0; i < m.configNumGroups; i++ {
				wMin, errW := strconv.Atoi(m.configInputsStep2[2*i].Value())
				bMin, errB := strconv.Atoi(m.configInputsStep2[2*i+1].Value())
				if errW != nil || errB != nil || wMin <= 0 || bMin <= 0 {
					m.configError = fmt.Sprintf("Group %d: durations must be greater than 0.", i+1)
					return m, nil
				}
				groups = append(groups, PomodoroGroup{WorkMin: wMin, BreakMin: bMin})
			}

			newProfile := Profile{
				Name:         m.configProfileName,
				Groups:       groups,
				LongBreakMin: m.configLongBreak,
			}

			if m.formEditIndex >= 0 && m.formEditIndex < len(m.savedConfig.Profiles) {
				oldName := m.savedConfig.Profiles[m.formEditIndex].Name
				m.savedConfig.Profiles[m.formEditIndex] = newProfile
				if m.savedConfig.QuickStartProfile == oldName {
					m.savedConfig.QuickStartProfile = newProfile.Name
				}
				m.profileIndex = m.formEditIndex
				m.statusMsg = fmt.Sprintf("Saved %q.", newProfile.Name)
			} else {
				m.savedConfig.Profiles = append(m.savedConfig.Profiles, newProfile)
				m.profileIndex = len(m.savedConfig.Profiles) - 1
				m.statusMsg = fmt.Sprintf("Created %q.", newProfile.Name)
			}
			m.persist()

			m.state = stateSelectProfile
			m.confirmDelete = false
			m.configError = ""
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.configInputsStep2[m.configStep2Focus], cmd = m.configInputsStep2[m.configStep2Focus].Update(msg)
	return m, cmd
}

const soundRows = 7

func (m model) updateSoundSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	n := len(systemSounds)
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "esc":
		m.state = stateSettingsMenu
	case "up", "k":
		m.soundIndex = wrapIndex(m.soundIndex-1, n)
	case "down", "j":
		m.soundIndex = wrapIndex(m.soundIndex+1, n)
	case "left", "h":
		m.soundIndex = wrapIndex(m.soundIndex-soundRows, n)
	case "right", "l":
		m.soundIndex = wrapIndex(m.soundIndex+soundRows, n)
	case "space":
		go m.notifier.PlaySound(systemSounds[m.soundIndex])
	case "enter":
		m.savedConfig.Sound = systemSounds[m.soundIndex]
		m.persist()
		m.state = stateSettingsMenu
	}
	return m, nil
}

func (m *model) initSpotifyInputs() {
	m.spotifyInputs = []textinput.Model{m.newInput("spotify:playlist:…", 100, m.contentWidth()-4)}
	m.spotifyInputs[0].SetValue(m.savedConfig.SpotifyURI)
	m.spotifyEnabledDraft = m.savedConfig.SpotifyEnabled
	m.spotifyDeviceDraft = m.savedConfig.SpotifyDevice()
	m.devicePicker = devicePicker{}
	m.spotifyFocusIndex = 0
	m.configError = ""
}

func (m model) spotifyDirty() bool {
	if len(m.spotifyInputs) == 0 {
		return false
	}
	return m.spotifyEnabledDraft != m.savedConfig.SpotifyEnabled ||
		m.spotifyDeviceDraft != m.savedConfig.SpotifyDevice() ||
		normalizeSpotifyURI(m.spotifyInputs[0].Value()) != m.savedConfig.SpotifyURI
}

const spotifyFields = 6

func (m *model) setSpotifyFocus(i int) {
	m.spotifyFocusIndex = wrapIndex(i, spotifyFields)
	if m.spotifyFocusIndex == 1 {
		m.spotifyInputs[0].Focus()
	} else {
		m.spotifyInputs[0].Blur()
	}
}

func (m model) updateSpotifySettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok && m.devicePicker.open {
		chosen, cmd := m.updateDevicePicker(key)
		if chosen != nil {
			m.spotifyDeviceDraft = *chosen
		}
		return m, cmd
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		k := key.String()
		onInput := m.spotifyFocusIndex == 1
		switch {
		case k == "esc":
			m.state = stateSettingsMenu
			return m, nil
		case k == "up" || k == "shift+tab" || (k == "k" && !onInput):
			m.setSpotifyFocus(m.spotifyFocusIndex - 1)
			return m, nil
		case k == "down" || k == "tab" || (k == "j" && !onInput):
			m.setSpotifyFocus(m.spotifyFocusIndex + 1)
			return m, nil
		case k == "enter" || (k == "space" && !onInput):
			switch m.spotifyFocusIndex {
			case 0:
				m.spotifyEnabledDraft = !m.spotifyEnabledDraft
			case 1:
				m.setSpotifyFocus(2)
			case 2:
				return m, m.openDevicePicker(m.spotifyDeviceDraft)
			case 3:
				m.state = stateSpotifyCredentials
				m.initSpotifyCredInputs()
				return m, textinput.Blink
			case 4:
				return m, m.startSpotifyAuth()
			case 5:
				m.savedConfig.SpotifyURI = normalizeSpotifyURI(m.spotifyInputs[0].Value())
				m.savedConfig.SpotifyEnabled = m.spotifyEnabledDraft
				if m.spotifyDeviceDraft != m.savedConfig.SpotifyDevice() {
					m.savedConfig.SetSpotifyDevice(m.spotifyDeviceDraft)
					m.spotifyTarget = m.spotifyDeviceDraft
				}
				m.persist()
				m.state = stateSettingsMenu
			}
			return m, nil
		}
	}

	if m.spotifyFocusIndex == 1 {
		var cmd tea.Cmd
		m.spotifyInputs[0], cmd = m.spotifyInputs[0].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) startSpotifyAuth() tea.Cmd {
	if m.savedConfig.SpotifyClientID == "" || m.savedConfig.SpotifyClientSecret == "" {
		m.configError = "Add your Client ID and Secret first (API credentials)."
		return nil
	}
	state, err := newOAuthState()
	if err != nil {
		m.configError = "Could not start authorization: " + err.Error()
		return nil
	}
	m.configError = ""
	m.state = stateSpotifyAuthWaiting
	m.spotifyAuthURL = getSpotifyAuthURL(m.savedConfig.SpotifyClientID, state)
	m.spotifyAuthDeadline = time.Now().Add(spotifyAuthTimeout)
	ctx, cancel := context.WithTimeout(context.Background(), spotifyAuthTimeout)
	m.spotifyAuthCancel = cancel
	return runSpotifyAuthCmd(ctx, m.savedConfig.SpotifyClientID, m.savedConfig.SpotifyClientSecret, state, m.spotifyAuthURL)
}

func (m *model) initSpotifyCredInputs() {
	m.spotifyCredInputs = []textinput.Model{
		m.newInput("Client ID", 64, m.contentWidth()-4),
		m.newInput("Client Secret", 64, m.contentWidth()-4),
	}
	m.spotifyCredInputs[0].SetValue(m.savedConfig.SpotifyClientID)
	m.spotifyCredInputs[0].Focus()
	m.spotifyCredInputs[1].EchoMode = textinput.EchoPassword
	m.spotifyCredInputs[1].SetValue(m.savedConfig.SpotifyClientSecret)
	m.spotifyCredFocus = 0
	m.configError = ""
}

func (m model) updateSpotifyCredentials(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			m.state = stateSpotifySettings
			m.configError = ""
			return m, nil
		case "tab", "down", "shift+tab", "up":
			m.spotifyCredInputs[m.spotifyCredFocus].Blur()
			m.spotifyCredFocus = 1 - m.spotifyCredFocus
			m.spotifyCredInputs[m.spotifyCredFocus].Focus()
			return m, nil
		case "enter":
			id := strings.TrimSpace(m.spotifyCredInputs[0].Value())
			secret := strings.TrimSpace(m.spotifyCredInputs[1].Value())
			if id == "" || secret == "" {
				m.configError = "Client ID and Client Secret cannot be empty."
				return m, nil
			}
			m.savedConfig.SpotifyClientID = id
			m.savedConfig.SpotifyClientSecret = secret
			m.persist()
			m.configError = ""
			m.statusMsg = "Credentials saved."
			m.state = stateSpotifySettings
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.spotifyCredInputs[m.spotifyCredFocus], cmd = m.spotifyCredInputs[m.spotifyCredFocus].Update(msg)
	return m, cmd
}

func (m model) updateSpotifyAuthWaiting(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "esc", "q", "ctrl+c":
		if m.spotifyAuthCancel != nil {
			m.spotifyAuthCancel()
			m.spotifyAuthCancel = nil
		}
		m.state = stateSpotifySettings
	case "o":
		openBrowser(m.spotifyAuthURL)
		m.statusMsg = "Opened the authorization page in your browser."
	case "c":
		if err := copyToClipboard(m.spotifyAuthURL); err != nil {
			m.statusMsg = "Could not copy the link: " + err.Error()
		} else {
			m.statusMsg = "Link copied to clipboard."
		}
	}
	return m, nil
}

func (m model) updateConfigPathSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			m.state = stateSettingsMenu
			return m, nil
		case "enter":
			if err := SetConfigPath(m.configPathInput.Value(), m.savedConfig); err != nil {
				m.configError = err.Error()
				return m, nil
			}
			m.configPath = GetConfigPath()
			savedCfg, err := LoadConfig()
			if err != nil {
				m.configError = err.Error()
				return m, nil
			}
			m.savedConfig = savedCfg
			m.configError = ""
			m.statusMsg = "Config file location updated."
			m.state = stateSettingsMenu
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.configPathInput, cmd = m.configPathInput.Update(msg)
	return m, cmd
}

func (m model) updateTimer(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, isKey := msg.(tea.KeyPressMsg)

	if isKey && key.String() == "ctrl+c" {
		m.pause()
		return m, m.pauseAndQuitCmd()
	}

	if isKey && m.devicePicker.open {
		chosen, cmd := m.updateDevicePicker(key)
		if chosen != nil {
			return m, m.chooseSessionDevice(*chosen)
		}
		return m, cmd
	}

	if m.cycleFinished {
		if !isKey {
			return m, nil
		}
		switch key.String() {
		case "y", "enter":
			m.cycleFinished = false
			m.completedGroups = 0
			m.spotifyLoaded = false
			m.phaseReady = false
			m.startGroupTimer(0, subPhaseWork)
			return m, tea.Batch(m.resume(), m.triggerSpotifyPlay())
		case "n", "esc":
			m.cycleFinished = false
			m.sessionActive = false
			m.backToMenu("quick")
		case "q":
			return m, m.pauseAndQuitCmd()
		}
		return m, nil
	}

	if m.confirmQuit {
		if isKey {
			m.confirmQuit = false
			if key.String() == "y" {
				m.pause()
				return m, m.pauseAndQuitCmd()
			}
			return m, nil
		}
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q":
			m.confirmQuit = true
			return m, nil
		case "esc":
			m.pause()
			m.backToMenu("resume")
			return m, m.pauseSpotifyForFocus()
		case "space":
			if m.active {
				m.pause()
				return m, m.pauseSpotifyForFocus()
			}
			tick := m.resume()
			if m.currentSub == subPhaseWork {
				return m, tea.Batch(tick, m.triggerSpotifyPlay())
			}
			return m, tick
		case "+", "=":
			return m, m.adjustVolume(10)
		case "-", "_":
			return m, m.adjustVolume(-10)
		case "d":
			if m.spotifyActive() {
				return m, m.openDevicePicker(m.spotifyTarget)
			}
		case "r":
			wasActive := m.active
			m.startGroupTimer(m.currentGroup, m.currentSub)
			if wasActive {
				return m, m.resume()
			}
			return m, nil
		case "s":
			m.pause()
			return m, m.completePhase()
		}

	case tickMsg:
		if msg.id != m.tickID || !m.active {
			return m, nil
		}
		if m.remaining() <= 0 {
			m.pause()
			return m, m.completePhase()
		}
		return m, m.scheduleTick()
	}
	return m, nil
}

func (m *model) pauseSpotifyForFocus() tea.Cmd {
	if m.currentSub != subPhaseWork {
		return nil
	}
	return m.triggerSpotifyPause()
}

func (m *model) remaining() time.Duration {
	if m.active {
		return max(0, time.Until(m.deadline))
	}
	return m.timeRemaining
}

func (m *model) resume() tea.Cmd {
	m.active = true
	m.phaseReady = false
	m.deadline = time.Now().Add(m.timeRemaining)
	m.tickID++
	return m.scheduleTick()
}

func (m *model) pause() {
	if m.active {
		m.timeRemaining = m.remaining()
		m.active = false
	}
	m.tickID++
}

func (m *model) scheduleTick() tea.Cmd {
	id := m.tickID
	d := m.remaining() % time.Second
	if d <= 0 {
		d = time.Second
	}
	return tea.Tick(d+10*time.Millisecond, func(time.Time) tea.Msg {
		return tickMsg{id: id}
	})
}

func (m *model) startGroupTimer(groupIdx int, sub subPhase) {
	m.pause()
	if groupIdx < 0 || groupIdx >= len(m.activeProfile.Groups) {
		groupIdx = 0
	}
	m.currentGroup = groupIdx
	m.currentSub = sub

	var minutes int
	switch {
	case sub == subPhaseLongBreak:
		minutes = m.activeProfile.LongBreakMin
	case len(m.activeProfile.Groups) == 0:
		minutes = 0
	case sub == subPhaseWork:
		minutes = m.activeProfile.Groups[groupIdx].WorkMin
	default:
		minutes = m.activeProfile.Groups[groupIdx].BreakMin
	}
	duration := time.Duration(minutes) * time.Minute
	m.timeRemaining = duration
	m.totalDuration = duration
}

func (m *model) notify(subtitle, message string) {
	go m.notifier.PlaySound(m.sound())
	go func(notifier Notifier, title string) {
		_ = notifier.Notify(title, subtitle, message)
	}(m.notifier, m.activeProfile.Name)
}

func (m *model) completePhase() tea.Cmd {
	autoStart := m.savedConfig.AutoStart

	switch m.currentSub {
	case subPhaseWork:
		m.completedGroups++

		if m.currentGroup >= len(m.activeProfile.Groups)-1 {
			m.startGroupTimer(0, subPhaseLongBreak)
			m.notify("Cycle focus completed!", fmt.Sprintf("You completed all %d focus sessions. Time for a long break.", len(m.activeProfile.Groups)))
		} else {
			m.startGroupTimer(m.currentGroup, subPhaseBreak)
			m.notify("Focus session complete!", "Great job! Time for a short break.")
		}
	case subPhaseBreak:
		m.startGroupTimer(m.currentGroup+1, subPhaseWork)
		m.notify("Break over!", fmt.Sprintf("Ready to focus on group %d?", m.currentGroup+1))

	case subPhaseLongBreak:
		m.cycleFinished = true
		autoStart = false
		m.notify("Cycle completed!", "Congratulations! You completed the cycle!")
	}

	var tick tea.Cmd
	if autoStart {
		tick = m.resume()
	} else if !m.cycleFinished {
		m.phaseReady = true
	}

	var spotify tea.Cmd
	if autoStart && m.currentSub == subPhaseWork {
		spotify = m.triggerSpotifyPlay()
	} else {
		spotify = m.triggerSpotifyPause()
	}
	return tea.Batch(tick, spotify)
}

var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println("gomodoro", version)
		return
	}

	tightenPermissions()

	savedCfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		fmt.Fprintln(os.Stderr, "Fix or move the file and try again. Gomodoro will not start, to avoid overwriting it.")
		os.Exit(1)
	}

	p := tea.NewProgram(initialModel(savedCfg))
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Encountered error: %v\n", err)
		os.Exit(1)
	}
}
