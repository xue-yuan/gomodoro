package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
		return "FOCUS WORK"
	case subPhaseBreak:
		return "SHORT BREAK"
	case subPhaseLongBreak:
		return "LONG BREAK"
	}
	return "UNKNOWN"
}

type tickMsg time.Time

type spotifyAuthResultMsg struct {
	accessToken  string
	refreshToken string
	expiresIn    int
	err          error
}

type spotifyErrorMsg struct {
	err error
}

type model struct {
	state       appState
	menuIndex   int
	savedConfig *Config

	settingsMenuIndex int

	profileIndex      int
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

	spotifyFocusIndex int
	spotifyInputs     []textinput.Model
	spotifyError      string
	spotifyVolume     int

	spotifyCredInputs []textinput.Model
	spotifyCredFocus  int
	spotifyLoaded     bool
	spotifyAuthCancel context.CancelFunc

	activeProfile   Profile
	currentGroup    int
	currentSub      subPhase
	completedGroups int
	cycleFinished   bool

	timeRemaining time.Duration
	totalDuration time.Duration
	active        bool
	notifier      Notifier
	quitting      bool

	width  int
	height int
}

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

func initialModel() model {
	return model{
		state:             stateMenu,
		menuIndex:         0,
		settingsMenuIndex: 0,
		active:            false,
		completedGroups:   0,
		notifier:          NewNotifier(),
	}
}

func createConfigInputsStep1() []textinput.Model {
	inputs := make([]textinput.Model, 3)

	inputs[0] = textinput.New()
	inputs[0].Placeholder = "e.g. Work Session"
	inputs[0].CharLimit = 20
	inputs[0].SetWidth(20)
	inputs[0].Focus()

	inputs[1] = textinput.New()
	inputs[1].Placeholder = "1-8"
	inputs[1].CharLimit = 1
	inputs[1].SetWidth(6)
	inputs[1].SetValue("4")

	inputs[2] = textinput.New()
	inputs[2].Placeholder = "Minutes"
	inputs[2].CharLimit = 3
	inputs[2].SetWidth(6)
	inputs[2].SetValue("20")

	return inputs
}

func (m *model) initSpotifyInputs() {
	m.spotifyInputs = make([]textinput.Model, 1)
	m.spotifyInputs[0] = textinput.New()
	m.spotifyInputs[0].Placeholder = "spotify:playlist:ID"
	m.spotifyInputs[0].CharLimit = 100
	m.spotifyInputs[0].SetWidth(40)
	if m.savedConfig != nil {
		m.spotifyInputs[0].SetValue(m.savedConfig.SpotifyURI)
	}
	m.spotifyFocusIndex = 0
	m.configError = ""
}

func (m *model) initSpotifyCredInputs() {
	m.spotifyCredInputs = make([]textinput.Model, 2)

	m.spotifyCredInputs[0] = textinput.New()
	m.spotifyCredInputs[0].Placeholder = "Your Client ID"
	m.spotifyCredInputs[0].CharLimit = 50
	m.spotifyCredInputs[0].SetWidth(40)
	if m.savedConfig != nil {
		m.spotifyCredInputs[0].SetValue(m.savedConfig.SpotifyClientID)
	}
	m.spotifyCredInputs[0].Focus()

	m.spotifyCredInputs[1] = textinput.New()
	m.spotifyCredInputs[1].Placeholder = "Your Client Secret"
	m.spotifyCredInputs[1].CharLimit = 50
	m.spotifyCredInputs[1].SetWidth(40)
	if m.savedConfig != nil {
		m.spotifyCredInputs[1].SetValue(m.savedConfig.SpotifyClientSecret)
	}

	m.spotifyCredFocus = 0
	m.configError = ""
}

func playSound(soundName string) {
	path := fmt.Sprintf("/System/Library/Sounds/%s.aiff", soundName)
	cmd := exec.Command("afplay", path)
	_ = cmd.Run()
}

func openBrowser(url string) {
	_ = exec.Command("open", url).Start()
}

func getSpotifyAuthURL(clientID string) string {
	redirectURI := "http://127.0.0.1:8080/callback"
	scopes := "user-modify-playback-state user-read-playback-state"
	return fmt.Sprintf("https://accounts.spotify.com/authorize?client_id=%s&response_type=code&redirect_uri=%s&scope=%s",
		clientID,
		url.QueryEscape(redirectURI),
		url.QueryEscape(scopes),
	)
}

func runSpotifyAuthCmdWithCtx(ctx context.Context, clientID, clientSecret string) tea.Cmd {
	return func() tea.Msg {
		code, err := StartOAuthServer(ctx)
		if err != nil {
			return spotifyAuthResultMsg{err: err}
		}

		resp, err := ExchangeCodeForToken(clientID, clientSecret, code)
		if err != nil {
			return spotifyAuthResultMsg{err: err}
		}

		return spotifyAuthResultMsg{
			accessToken:  resp.AccessToken,
			refreshToken: resp.RefreshToken,
			expiresIn:    resp.ExpiresIn,
		}
	}
}

func playSpotifyCmd(clientID, clientSecret, refreshToken string, expiry time.Time, uri string, shuffle bool) tea.Cmd {
	return func() tea.Msg {
		accessToken := ""
		if refreshToken == "" {
			return spotifyErrorMsg{err: fmt.Errorf("Spotify account not linked. Go to settings.")}
		}

		if time.Now().Add(5 * time.Minute).After(expiry) {
			resp, err := RefreshSpotifyToken(clientID, clientSecret, refreshToken)
			if err != nil {
				return spotifyErrorMsg{err: fmt.Errorf("token refresh failed: %v", err)}
			}
			accessToken = resp.AccessToken
			cfg, loadErr := LoadConfig()
			if loadErr == nil {
				cfg.SpotifyAccessToken = resp.AccessToken
				cfg.SpotifyRefreshToken = resp.RefreshToken
				cfg.SpotifyTokenExpiry = time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
				_ = SaveConfig(cfg)
			}
		} else {
			cfg, loadErr := LoadConfig()
			if loadErr == nil {
				accessToken = cfg.SpotifyAccessToken
			}
		}

		if accessToken == "" {
			return spotifyErrorMsg{err: fmt.Errorf("access token not found")}
		}

		if shuffle && uri != "" {
			_ = SpotifySetShuffle(accessToken, true)
		}

		err := SpotifyPlay(accessToken, uri)
		if err != nil {
			return spotifyErrorMsg{err: err}
		}
		return spotifyErrorMsg{err: nil}
	}
}

func pauseSpotifyCmd(clientID, clientSecret, refreshToken string, expiry time.Time) tea.Cmd {
	return func() tea.Msg {
		accessToken := ""
		if refreshToken == "" {
			return nil
		}

		if time.Now().Add(5 * time.Minute).After(expiry) {
			resp, err := RefreshSpotifyToken(clientID, clientSecret, refreshToken)
			if err == nil {
				accessToken = resp.AccessToken
				cfg, loadErr := LoadConfig()
				if loadErr == nil {
					cfg.SpotifyAccessToken = resp.AccessToken
					cfg.SpotifyRefreshToken = resp.RefreshToken
					cfg.SpotifyTokenExpiry = time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
					_ = SaveConfig(cfg)
				}
			}
		} else {
			cfg, loadErr := LoadConfig()
			if loadErr == nil {
				accessToken = cfg.SpotifyAccessToken
			}
		}

		if accessToken != "" {
			_ = SpotifyPause(accessToken)
		}
		return nil
	}
}

func changeSpotifyVolumeCmd(clientID, clientSecret, refreshToken string, expiry time.Time, volume int) tea.Cmd {
	return func() tea.Msg {
		accessToken := ""
		if refreshToken == "" {
			return spotifyErrorMsg{err: fmt.Errorf("Spotify account not linked.")}
		}

		if time.Now().Add(5 * time.Minute).After(expiry) {
			resp, err := RefreshSpotifyToken(clientID, clientSecret, refreshToken)
			if err != nil {
				return spotifyErrorMsg{err: fmt.Errorf("token refresh failed: %v", err)}
			}
			accessToken = resp.AccessToken
			cfg, loadErr := LoadConfig()
			if loadErr == nil {
				cfg.SpotifyAccessToken = resp.AccessToken
				cfg.SpotifyRefreshToken = resp.RefreshToken
				cfg.SpotifyTokenExpiry = time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
				_ = SaveConfig(cfg)
			}
		} else {
			cfg, loadErr := LoadConfig()
			if loadErr == nil {
				accessToken = cfg.SpotifyAccessToken
			}
		}

		if accessToken == "" {
			return spotifyErrorMsg{err: fmt.Errorf("access token not found")}
		}

		err := SpotifySetVolume(accessToken, volume)
		if err != nil {
			return spotifyErrorMsg{err: err}
		}
		return spotifyErrorMsg{err: nil}
	}
}

func (m *model) triggerSpotifyPlay() tea.Cmd {
	if m.savedConfig != nil && m.savedConfig.SpotifyEnabled && m.savedConfig.SpotifyURI != "" {
		if !m.spotifyLoaded {
			m.spotifyLoaded = true
			return playSpotifyCmd(
				m.savedConfig.SpotifyClientID,
				m.savedConfig.SpotifyClientSecret,
				m.savedConfig.SpotifyRefreshToken,
				m.savedConfig.SpotifyTokenExpiry,
				m.savedConfig.SpotifyURI,
				true,
			)
		}
		return playSpotifyCmd(
			m.savedConfig.SpotifyClientID,
			m.savedConfig.SpotifyClientSecret,
			m.savedConfig.SpotifyRefreshToken,
			m.savedConfig.SpotifyTokenExpiry,
			"", // empty context URI resumes playback
			false,
		)
	}
	return nil
}

func (m *model) triggerSpotifyPause() tea.Cmd {
	if m.savedConfig != nil && m.savedConfig.SpotifyEnabled {
		return pauseSpotifyCmd(
			m.savedConfig.SpotifyClientID,
			m.savedConfig.SpotifyClientSecret,
			m.savedConfig.SpotifyRefreshToken,
			m.savedConfig.SpotifyTokenExpiry,
		)
	}
	return nil
}

func (m *model) triggerSpotifyVolume() tea.Cmd {
	if m.savedConfig != nil && m.savedConfig.SpotifyEnabled {
		return changeSpotifyVolumeCmd(
			m.savedConfig.SpotifyClientID,
			m.savedConfig.SpotifyClientSecret,
			m.savedConfig.SpotifyRefreshToken,
			m.savedConfig.SpotifyTokenExpiry,
			m.spotifyVolume,
		)
	}
	return nil
}

func (m model) Init() tea.Cmd {
	if m.state == stateTimer {
		return tickCmd()
	}
	return textinput.Blink
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case spotifyAuthResultMsg:
		if msg.err != nil {
			if msg.err == context.Canceled {
				m.configError = ""
				m.state = stateSpotifySettings
				return m, nil
			}
			m.configError = "Auth Failed: " + msg.err.Error()
			m.state = stateSpotifySettings
			return m, nil
		}
		if m.savedConfig == nil {
			m.savedConfig = &Config{}
		}
		m.savedConfig.SpotifyAccessToken = msg.accessToken
		m.savedConfig.SpotifyRefreshToken = msg.refreshToken
		m.savedConfig.SpotifyTokenExpiry = time.Now().Add(time.Duration(msg.expiresIn) * time.Second)
		_ = SaveConfig(m.savedConfig)

		m.configError = ""
		m.state = stateSpotifySettings
		return m, nil

	case spotifyErrorMsg:
		if msg.err != nil {
			m.spotifyError = msg.err.Error()
		} else {
			m.spotifyError = ""
		}
		return m, nil
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

func (m model) updateMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "up", "k":
			m.menuIndex--
			if m.menuIndex < 0 {
				m.menuIndex = 3
			}
		case "down", "j":
			m.menuIndex++
			if m.menuIndex > 3 {
				m.menuIndex = 0
			}
		case "enter":
			switch m.menuIndex {
			case 0:
				groups := make([]PomodoroGroup, 4)
				for i := 0; i < 4; i++ {
					groups[i] = PomodoroGroup{WorkMin: 25, BreakMin: 5}
				}
				m.activeProfile = Profile{
					Name:         "Default Cycle",
					Groups:       groups,
					LongBreakMin: 20,
				}
				m.completedGroups = 0
				m.spotifyVolume = 50
				if m.savedConfig != nil && m.savedConfig.SpotifyVolume != 0 {
					m.spotifyVolume = m.savedConfig.SpotifyVolume
				}
				m.spotifyLoaded = false
				m.startGroupTimer(0, subPhaseWork)
				m.state = stateTimer
				if m.savedConfig != nil && m.savedConfig.AutoStart {
					m.active = true
					return m, tea.Batch(tickCmd(), m.triggerSpotifyPlay())
				}
				m.active = false
				return m, tickCmd()

			case 1:
				m.state = stateSelectProfile
				m.profileIndex = 0
				return m, nil

			case 2:
				m.state = stateSettingsMenu
				m.settingsMenuIndex = 0
				return m, nil

			case 3:
				m.quitting = true
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

func (m model) updateSelectProfile(msg tea.Msg) (tea.Model, tea.Cmd) {
	numProfiles := 0
	if m.savedConfig != nil {
		numProfiles = len(m.savedConfig.Profiles)
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.state = stateMenu
			return m, nil
		case "up", "k":
			if numProfiles > 0 {
				m.profileIndex--
				if m.profileIndex < 0 {
					m.profileIndex = numProfiles - 1
				}
			}
		case "down", "j":
			if numProfiles > 0 {
				m.profileIndex++
				if m.profileIndex >= numProfiles {
					m.profileIndex = 0
				}
			}
		case "d", "backspace":
			if numProfiles > 0 && m.profileIndex < numProfiles {
				m.savedConfig.Profiles = append(m.savedConfig.Profiles[:m.profileIndex], m.savedConfig.Profiles[m.profileIndex+1:]...)
				_ = SaveConfig(m.savedConfig)
				m.profileIndex = 0
			}
		case "enter":
			if numProfiles > 0 && m.profileIndex < numProfiles {
				m.activeProfile = m.savedConfig.Profiles[m.profileIndex]
				m.completedGroups = 0
				m.spotifyVolume = 50
				if m.savedConfig != nil && m.savedConfig.SpotifyVolume != 0 {
					m.spotifyVolume = m.savedConfig.SpotifyVolume
				}
				m.spotifyLoaded = false
				m.startGroupTimer(0, subPhaseWork)
				m.state = stateTimer
				if m.savedConfig != nil && m.savedConfig.AutoStart {
					m.active = true
					return m, tea.Batch(tickCmd(), m.triggerSpotifyPlay())
				}
				m.active = false
				return m, tickCmd()
			} else {
				m.state = stateConfigName
				m.configInputsStep1 = createConfigInputsStep1()
				m.configStep1Focus = 0
				m.configError = ""
				return m, textinput.Blink
			}
		}
	}
	return m, nil
}

func (m model) updateSettingsMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.state = stateMenu
			m.menuIndex = 2
			return m, nil
		case "up", "k":
			m.settingsMenuIndex--
			if m.settingsMenuIndex < 0 {
				m.settingsMenuIndex = 5
			}
		case "down", "j":
			m.settingsMenuIndex++
			if m.settingsMenuIndex > 5 {
				m.settingsMenuIndex = 0
			}
		case "enter":
			switch m.settingsMenuIndex {
			case 0:
				m.state = stateConfigName
				m.configInputsStep1 = createConfigInputsStep1()
				m.configStep1Focus = 0
				m.configError = ""
				return m, textinput.Blink

			case 1:
				m.state = stateSoundSettings
				currentSound := "Glass"
				if m.savedConfig != nil && m.savedConfig.Sound != "" {
					currentSound = m.savedConfig.Sound
				}
				m.soundIndex = 0
				for i, s := range systemSounds {
					if s == currentSound {
						m.soundIndex = i
						break
					}
				}
				return m, nil

			case 2:
				m.state = stateSpotifySettings
				m.initSpotifyInputs()
				return m, textinput.Blink

			case 3:
				if m.savedConfig == nil {
					m.savedConfig = &Config{}
				}
				m.savedConfig.AutoStart = !m.savedConfig.AutoStart
				_ = SaveConfig(m.savedConfig)
				return m, nil

			case 4:
				m.state = stateConfigPathSettings
				m.configPathInput = textinput.New()
				m.configPathInput.Placeholder = "e.g. /path/to/profiles.json"
				m.configPathInput.SetWidth(60)
				m.configPathInput.SetValue(GetConfigPath())
				m.configPathInput.Focus()
				m.configError = ""
				return m, textinput.Blink

			case 5:
				m.state = stateMenu
				m.menuIndex = 2
				return m, nil
			}
		}
	}
	return m, nil
}

func (m model) updateConfigName(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m.configStep1Focus == 1 || m.configStep1Focus == 2 {
			key := msg.String()
			if len(key) == 1 {
				if key[0] < '0' || key[0] > '9' {
					return m, nil
				}
			}
		}

		switch msg.String() {
		case "esc":
			m.state = stateSettingsMenu
			return m, nil
		case "tab", "down":
			m.configInputsStep1[m.configStep1Focus].Blur()
			m.configStep1Focus = (m.configStep1Focus + 1) % 3
			m.configInputsStep1[m.configStep1Focus].Focus()
			return m, nil
		case "shift+tab", "up":
			m.configInputsStep1[m.configStep1Focus].Blur()
			m.configStep1Focus--
			if m.configStep1Focus < 0 {
				m.configStep1Focus = 2
			}
			m.configInputsStep1[m.configStep1Focus].Focus()
			return m, nil
		case "enter":
			name := strings.TrimSpace(m.configInputsStep1[0].Value())
			groupsStr := m.configInputsStep1[1].Value()
			longBreakStr := m.configInputsStep1[2].Value()

			numGroups, errG := strconv.Atoi(groupsStr)
			longBreakMin, errL := strconv.Atoi(longBreakStr)

			if name == "" {
				m.configError = "Profile Name cannot be empty."
				return m, nil
			}
			if errG != nil || numGroups < 1 || numGroups > 8 {
				m.configError = "Groups count must be an integer between 1 and 8."
				return m, nil
			}
			if errL != nil || longBreakMin <= 0 {
				m.configError = "Long break must be a positive integer."
				return m, nil
			}

			m.configProfileName = name
			m.configNumGroups = numGroups
			m.configLongBreak = longBreakMin

			m.configInputsStep2 = make([]textinput.Model, 2*numGroups)
			for i := 0; i < numGroups; i++ {
				wInput := textinput.New()
				wInput.Placeholder = "Work Min"
				wInput.CharLimit = 3
				wInput.SetWidth(6)
				wInput.SetValue("25")
				m.configInputsStep2[2*i] = wInput

				bInput := textinput.New()
				bInput.Placeholder = "Break Min"
				bInput.CharLimit = 3
				bInput.SetWidth(6)
				bInput.SetValue("5")
				m.configInputsStep2[2*i+1] = bInput
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

func (m model) updateConfigGroups(msg tea.Msg) (tea.Model, tea.Cmd) {
	numFields := 2 * m.configNumGroups

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		key := msg.String()
		if len(key) == 1 {
			if key[0] < '0' || key[0] > '9' {
				return m, nil
			}
		}

		switch msg.String() {
		case "esc":
			m.state = stateConfigName
			return m, nil
		case "tab", "down":
			m.configInputsStep2[m.configStep2Focus].Blur()
			m.configStep2Focus = (m.configStep2Focus + 1) % numFields
			m.configInputsStep2[m.configStep2Focus].Focus()
			return m, nil
		case "shift+tab", "up":
			m.configInputsStep2[m.configStep2Focus].Blur()
			m.configStep2Focus--
			if m.configStep2Focus < 0 {
				m.configStep2Focus = numFields - 1
			}
			m.configInputsStep2[m.configStep2Focus].Focus()
			return m, nil
		case "enter":
			var groups []PomodoroGroup
			for i := 0; i < m.configNumGroups; i++ {
				wVal := m.configInputsStep2[2*i].Value()
				bVal := m.configInputsStep2[2*i+1].Value()

				wMin, errW := strconv.Atoi(wVal)
				bMin, errB := strconv.Atoi(bVal)

				if errW != nil || errB != nil || wMin <= 0 || bMin <= 0 {
					m.configError = fmt.Sprintf("Invalid duration in Group %d. Duration must be > 0.", i+1)
					return m, nil
				}

				groups = append(groups, PomodoroGroup{
					WorkMin:  wMin,
					BreakMin: bMin,
				})
			}

			newProfile := Profile{
				Name:         m.configProfileName,
				Groups:       groups,
				LongBreakMin: m.configLongBreak,
			}

			if m.savedConfig == nil {
				m.savedConfig = &Config{}
			}
			m.savedConfig.Profiles = append(m.savedConfig.Profiles, newProfile)
			_ = SaveConfig(m.savedConfig)

			m.state = stateSelectProfile
			m.profileIndex = len(m.savedConfig.Profiles) - 1
			m.configError = ""
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.configInputsStep2[m.configStep2Focus], cmd = m.configInputsStep2[m.configStep2Focus].Update(msg)
	return m, cmd
}

func (m model) updateSoundSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	numSounds := len(systemSounds)
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.state = stateSettingsMenu
			return m, nil
		case "up", "k":
			m.soundIndex--
			if m.soundIndex < 0 {
				m.soundIndex = numSounds - 1
			}
		case "down", "j":
			m.soundIndex++
			if m.soundIndex >= numSounds {
				m.soundIndex = 0
			}
		case "space":
			soundName := systemSounds[m.soundIndex]
			go playSound(soundName)
		case "enter":
			soundName := systemSounds[m.soundIndex]
			if m.savedConfig == nil {
				m.savedConfig = &Config{}
			}
			m.savedConfig.Sound = soundName
			_ = SaveConfig(m.savedConfig)
			m.state = stateSettingsMenu
			return m, nil
		}
	}
	return m, nil
}

func (m model) updateSpotifySettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.state = stateSettingsMenu
			return m, nil
		case "up", "shift+tab":
			m.spotifyFocusIndex--
			if m.spotifyFocusIndex < 0 {
				m.spotifyFocusIndex = 4
			}
			if m.spotifyFocusIndex == 1 {
				m.spotifyInputs[0].Focus()
			} else {
				m.spotifyInputs[0].Blur()
			}
			return m, nil
		case "down", "tab":
			m.spotifyFocusIndex = (m.spotifyFocusIndex + 1) % 5
			if m.spotifyFocusIndex == 1 {
				m.spotifyInputs[0].Focus()
			} else {
				m.spotifyInputs[0].Blur()
			}
			return m, nil
		case "space", "enter":
			switch m.spotifyFocusIndex {
			case 0:
				if m.savedConfig == nil {
					m.savedConfig = &Config{}
				}
				m.savedConfig.SpotifyEnabled = !m.savedConfig.SpotifyEnabled
				return m, nil
			case 1:
				if msg.String() == "enter" {
					m.spotifyInputs[0].Blur()
					m.spotifyFocusIndex = 2
					return m, nil
				}
			case 2:
				m.state = stateSpotifyCredentials
				m.initSpotifyCredInputs()
				return m, textinput.Blink
			case 3:
				if m.savedConfig == nil || m.savedConfig.SpotifyClientID == "" || m.savedConfig.SpotifyClientSecret == "" {
					m.configError = "Please edit and save API credentials first."
					return m, nil
				}
				m.configError = ""
				m.state = stateSpotifyAuthWaiting
				authURL := getSpotifyAuthURL(m.savedConfig.SpotifyClientID)
				openBrowser(authURL)
				ctx, cancel := context.WithCancel(context.Background())
				m.spotifyAuthCancel = cancel
				return m, runSpotifyAuthCmdWithCtx(ctx, m.savedConfig.SpotifyClientID, m.savedConfig.SpotifyClientSecret)
			case 4:
				uri := strings.TrimSpace(m.spotifyInputs[0].Value())
				if m.savedConfig == nil {
					m.savedConfig = &Config{}
				}
				m.savedConfig.SpotifyURI = uri
				_ = SaveConfig(m.savedConfig)
				m.state = stateSettingsMenu
				return m, nil
			}
		}
	}

	if m.spotifyFocusIndex == 1 {
		var cmd tea.Cmd
		m.spotifyInputs[0], cmd = m.spotifyInputs[0].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) updateSpotifyCredentials(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.state = stateSpotifySettings
			m.configError = ""
			return m, nil
		case "tab", "down":
			m.spotifyCredInputs[m.spotifyCredFocus].Blur()
			m.spotifyCredFocus = (m.spotifyCredFocus + 1) % 2
			m.spotifyCredInputs[m.spotifyCredFocus].Focus()
			return m, nil
		case "shift+tab", "up":
			m.spotifyCredInputs[m.spotifyCredFocus].Blur()
			m.spotifyCredFocus--
			if m.spotifyCredFocus < 0 {
				m.spotifyCredFocus = 1
			}
			m.spotifyCredInputs[m.spotifyCredFocus].Focus()
			return m, nil
		case "enter":
			id := strings.TrimSpace(m.spotifyCredInputs[0].Value())
			secret := strings.TrimSpace(m.spotifyCredInputs[1].Value())

			if id == "" || secret == "" {
				m.configError = "Client ID and Client Secret cannot be empty."
				return m, nil
			}

			if m.savedConfig == nil {
				m.savedConfig = &Config{}
			}
			m.savedConfig.SpotifyClientID = id
			m.savedConfig.SpotifyClientSecret = secret
			_ = SaveConfig(m.savedConfig)

			m.configError = ""
			m.state = stateSpotifySettings
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.spotifyCredInputs[m.spotifyCredFocus], cmd = m.spotifyCredInputs[m.spotifyCredFocus].Update(msg)
	return m, cmd
}

func (m model) updateSpotifyAuthWaiting(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "q", "ctrl+c":
			if m.spotifyAuthCancel != nil {
				m.spotifyAuthCancel()
			}
			m.state = stateSpotifySettings
			return m, nil
		}
	}
	return m, nil
}

func (m model) updateConfigPathSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.state = stateSettingsMenu
			return m, nil
		case "enter":
			newPath := m.configPathInput.Value()
			err := SetConfigPath(newPath)
			if err != nil {
				m.configError = err.Error()
				return m, nil
			}
			savedCfg, _ := LoadConfig()
			m.savedConfig = savedCfg
			m.state = stateSettingsMenu
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.configPathInput, cmd = m.configPathInput.Update(msg)
	return m, cmd
}

func (m model) updateTimer(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.cycleFinished {
		switch msg := msg.(type) {
		case tea.KeyPressMsg:
			switch msg.String() {
			case "y", "Y", "enter":
				m.cycleFinished = false
				m.completedGroups = 0
				m.spotifyLoaded = false
				m.startGroupTimer(0, subPhaseWork)
				m.active = true
				return m, m.triggerSpotifyPlay()
			case "n", "N", "esc":
				m.cycleFinished = false
				m.state = stateMenu
				return m, nil
			case "q", "ctrl+c":
				m.quitting = true
				return m, tea.Quit
			}
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Batch(m.triggerSpotifyPause(), tea.Quit)
		case "esc":
			m.active = false
			m.state = stateMenu
			return m, m.triggerSpotifyPause()
		case "space":
			m.active = !m.active
			var cmd tea.Cmd
			if m.active && m.currentSub == subPhaseWork {
				cmd = m.triggerSpotifyPlay()
			} else {
				cmd = m.triggerSpotifyPause()
			}
			return m, cmd
		case "+", "=":
			if m.savedConfig != nil && m.savedConfig.SpotifyEnabled {
				m.spotifyVolume += 10
				if m.spotifyVolume > 100 {
					m.spotifyVolume = 100
				}
				m.savedConfig.SpotifyVolume = m.spotifyVolume
				_ = SaveConfig(m.savedConfig)
				return m, m.triggerSpotifyVolume()
			}
		case "-", "_":
			if m.savedConfig != nil && m.savedConfig.SpotifyEnabled {
				m.spotifyVolume -= 10
				if m.spotifyVolume < 0 {
					m.spotifyVolume = 0
				}
				m.savedConfig.SpotifyVolume = m.spotifyVolume
				_ = SaveConfig(m.savedConfig)
				return m, m.triggerSpotifyVolume()
			}
		case "r":
			m.startGroupTimer(m.currentGroup, m.currentSub)
			var cmd tea.Cmd
			if m.currentSub == subPhaseWork {
				cmd = m.triggerSpotifyPlay()
			} else {
				cmd = m.triggerSpotifyPause()
			}
			return m, cmd
		case "s":
			m.active = false
			cmd := m.completePhase()
			return m, cmd
		}

	case tickMsg:
		if m.active {
			m.timeRemaining -= time.Second
			if m.timeRemaining <= 0 {
				m.active = false
				cmd := m.completePhase()
				return m, cmd
			}
		}
		return m, tickCmd()
	}
	return m, nil
}

func (m *model) startGroupTimer(groupIdx int, sub subPhase) {
	m.currentGroup = groupIdx
	m.currentSub = sub
	m.active = false

	var duration time.Duration
	if sub == subPhaseLongBreak {
		duration = time.Duration(m.activeProfile.LongBreakMin) * time.Minute
	} else {
		grp := m.activeProfile.Groups[groupIdx]
		if sub == subPhaseWork {
			duration = time.Duration(grp.WorkMin) * time.Minute
		} else {
			duration = time.Duration(grp.BreakMin) * time.Minute
		}
	}
	m.timeRemaining = duration
	m.totalDuration = duration
}

func (m *model) completePhase() tea.Cmd {
	oldSub := m.currentSub

	spotifyCmd := m.triggerSpotifyPause()

	sound := "Glass"
	if m.savedConfig != nil && m.savedConfig.Sound != "" {
		sound = m.savedConfig.Sound
	}

	autoStart := false
	if m.savedConfig != nil {
		autoStart = m.savedConfig.AutoStart
	}

	switch oldSub {
	case subPhaseWork:
		m.completedGroups++

		if m.currentGroup == len(m.activeProfile.Groups)-1 {
			m.startGroupTimer(0, subPhaseLongBreak)
			m.active = autoStart

			title := m.activeProfile.Name
			subtitle := "Cycle Focus Completed!"
			message := fmt.Sprintf("Awesome! You completed all %d focus sessions. Starting long break...", len(m.activeProfile.Groups))

			go func(notifier Notifier, t, sub, msg, snd string) {
				_ = notifier.Notify(t, sub, msg, snd)
			}(m.notifier, title, subtitle, message, sound)
		} else {
			m.startGroupTimer(m.currentGroup, subPhaseBreak)
			m.active = autoStart

			title := m.activeProfile.Name
			subtitle := "Focus Session Complete!"
			message := "Great job! Focus completed. Ready for a break?"

			go func(notifier Notifier, t, sub, msg, snd string) {
				_ = notifier.Notify(t, sub, msg, snd)
			}(m.notifier, title, subtitle, message, sound)
		}
	case subPhaseBreak:
		m.startGroupTimer(m.currentGroup+1, subPhaseWork)
		m.active = autoStart

		title := m.activeProfile.Name
		subtitle := "Break Over!"
		message := fmt.Sprintf("Ready to focus on Group %d?", m.currentGroup+1)

		go func(notifier Notifier, t, sub, msg, snd string) {
			_ = notifier.Notify(t, sub, msg, snd)
		}(m.notifier, title, subtitle, message, sound)

	case subPhaseLongBreak:
		m.cycleFinished = true
		m.active = false

		title := m.activeProfile.Name
		subtitle := "Cycle Completed!"
		message := "Congratulations! You completed the cycle!"

		go func(notifier Notifier, t, sub, msg, snd string) {
			_ = notifier.Notify(t, sub, msg, snd)
		}(m.notifier, title, subtitle, message, sound)
	}

	var nextCmd tea.Cmd = spotifyCmd
	if m.active && m.currentSub == subPhaseWork {
		nextCmd = tea.Batch(spotifyCmd, m.triggerSpotifyPlay())
	}

	return nextCmd
}

func (m model) viewMenu() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#7C3AED")).
		Padding(0, 3).
		Align(lipgloss.Center)

	title := titleStyle.Render("🍅 GOMODORO")

	menuItems := []string{
		"Start Default Cycle (4 Groups of 25m/5m + 20m Long)",
		"Select Saved Profile",
		"Settings",
		"Exit App",
	}

	var sb strings.Builder
	sb.WriteString("  " + title + "\n\n")
	sb.WriteString("  Select an option:\n\n")

	for i, item := range menuItems {
		cursor := "  "
		itemStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF"))

		if i == m.menuIndex {
			cursor = "❯ "
			itemStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7C3AED"))
		}
		sb.WriteString(fmt.Sprintf("  %s%s\n", cursor, itemStyle.Render(item)))
	}

	sb.WriteString("\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Italic(true).Render("[↑/↓] Navigate  •  [enter] Select  •  [q] Quit"))

	cardStyle := lipgloss.NewStyle().
		Padding(1, 0).
		Margin(1, 0)

	return cardStyle.Render(sb.String())
}

func (m model) viewSettingsMenu() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#4B5563")).
		Padding(0, 3).
		Align(lipgloss.Center)

	title := titleStyle.Render("⚙️ SETTINGS")

	autoStartStr := "Disabled"
	if m.savedConfig != nil && m.savedConfig.AutoStart {
		autoStartStr = "Enabled"
	}

	configPath := GetConfigPath()
	home, err := os.UserHomeDir()
	if err == nil {
		configPath = strings.Replace(configPath, home, "~", 1)
	}

	menuItems := []string{
		"Create Custom Profile",
		"Configure Notification Sound",
		"Configure Spotify Autoplay",
		fmt.Sprintf("Auto-Start Next Phase: [%s]", autoStartStr),
		"Change Config Storage Path",
		"Back to Main Menu",
	}

	var sb strings.Builder
	sb.WriteString("  " + title + "\n\n")
	sb.WriteString(fmt.Sprintf("  Config File: %s\n\n", lipgloss.NewStyle().Foreground(lipgloss.Color("#60A5FA")).Render(configPath)))
	sb.WriteString("  Select a setting to configure:\n\n")

	for i, item := range menuItems {
		cursor := "  "
		itemStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF"))

		if i == m.settingsMenuIndex {
			cursor = "❯ "
			if i == 3 {
				itemStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#10B981"))
			} else {
				itemStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#4B5563"))
			}
		}
		sb.WriteString(fmt.Sprintf("  %s%s\n", cursor, itemStyle.Render(item)))
	}

	sb.WriteString("\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Italic(true).Render("[↑/↓] Navigate  •  [enter] Select/Toggle  •  [esc] Main Menu"))

	cardStyle := lipgloss.NewStyle().
		Padding(1, 0).
		Margin(1, 0)

	return cardStyle.Render(sb.String())
}

func (m model) viewSelectProfile() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#3B82F6")).
		Padding(0, 3).
		Align(lipgloss.Center)

	title := titleStyle.Render("📁 SELECT PROFILE")

	var sb strings.Builder
	sb.WriteString("  " + title + "\n\n")

	numProfiles := 0
	if m.savedConfig != nil {
		numProfiles = len(m.savedConfig.Profiles)
	}

	if numProfiles == 0 {
		sb.WriteString("  No saved custom profiles. Select Create Profile in Settings.\n")
	} else {
		for i, profile := range m.savedConfig.Profiles {
			cursor := "  "
			profileStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF"))

			if i == m.profileIndex {
				cursor = "❯ "
				profileStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#3B82F6"))
			}

			sb.WriteString(fmt.Sprintf("  %s%s (%d Groups, Long: %dm)\n",
				cursor,
				profileStyle.Render(profile.Name),
				len(profile.Groups),
				profile.LongBreakMin,
			))
		}
	}

	sound := "Glass"
	if m.savedConfig != nil && m.savedConfig.Sound != "" {
		sound = m.savedConfig.Sound
	}

	sb.WriteString(fmt.Sprintf("\n  Active Sound Effect: %s\n", lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#10B981")).Render(sound)))
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Italic(true).Render("[↑/↓] Navigate  •  [enter] Start  •  [d/backspace] Delete  •  [esc] Main Menu"))

	cardStyle := lipgloss.NewStyle().
		Padding(1, 0).
		Margin(1, 0)

	return cardStyle.Render(sb.String())
}

func (m model) viewConfigName() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#EC4899")).
		Padding(0, 3).
		Align(lipgloss.Center)

	title := titleStyle.Render("🛠 CREATE PROFILE - STEP 1")

	var sb strings.Builder
	sb.WriteString("  " + title + "\n\n")

	labels := []string{
		"Profile Name:    ",
		"Groups (1-8):    ",
		"Long Break (min):",
	}

	for i := 0; i < 3; i++ {
		labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB"))
		if i == m.configStep1Focus {
			labelStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#EC4899"))
		}
		sb.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render(labels[i]), m.configInputsStep1[i].View()))
	}

	if m.configError != "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Bold(true)
		sb.WriteString(fmt.Sprintf("\n  %s\n", errStyle.Render(m.configError)))
	} else {
		sb.WriteString("\n")
	}

	sb.WriteString("\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Italic(true).Render("[tab] Next  •  [shift+tab] Prev  •  [enter] Next Step  •  [esc] Cancel"))

	cardStyle := lipgloss.NewStyle().
		Padding(1, 0).
		Margin(1, 0)

	return cardStyle.Render(sb.String())
}

func (m model) viewConfigGroups() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#EC4899")).
		Padding(0, 3).
		Align(lipgloss.Center)

	title := titleStyle.Render(fmt.Sprintf("🛠 SETUP GROUPS: %s - STEP 2", m.configProfileName))

	var sb strings.Builder
	sb.WriteString("  " + title + "\n\n")
	sb.WriteString("  Configure durations (minutes) for each group:\n\n")

	for i := 0; i < m.configNumGroups; i++ {
		wIdx := 2 * i
		bIdx := 2*i + 1

		groupLabel := fmt.Sprintf("  Group %d: ", i+1)

		wStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB"))
		bStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB"))

		if m.configStep2Focus == wIdx {
			wStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#EC4899"))
		} else if m.configStep2Focus == bIdx {
			bStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#EC4899"))
		}

		sb.WriteString(fmt.Sprintf("%s%s %s min | %s %s min\n",
			groupLabel,
			wStyle.Render("Work"),
			m.configInputsStep2[wIdx].View(),
			bStyle.Render("Break"),
			m.configInputsStep2[bIdx].View(),
		))
	}

	if m.configError != "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Bold(true)
		sb.WriteString(fmt.Sprintf("\n  %s\n", errStyle.Render(m.configError)))
	} else {
		sb.WriteString("\n")
	}

	sb.WriteString("\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Italic(true).Render("[tab] Next  •  [shift+tab] Prev  •  [enter] Save Profile  •  [esc] Back"))

	cardStyle := lipgloss.NewStyle().
		Padding(1, 0).
		Margin(1, 0)

	return cardStyle.Render(sb.String())
}

func (m model) viewSoundSettings() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#10B981")).
		Padding(0, 3).
		Align(lipgloss.Center)

	title := titleStyle.Render("🎵 CONFIGURE GLOBAL SOUND")

	var sb strings.Builder
	sb.WriteString("  " + title + "\n\n")
	sb.WriteString("  Select a sound for completion notifications:\n\n")

	currentGlobalSound := "Glass"
	if m.savedConfig != nil && m.savedConfig.Sound != "" {
		currentGlobalSound = m.savedConfig.Sound
	}

	for i, sound := range systemSounds {
		cursor := "  "
		soundStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF"))
		activeIndicator := ""

		if sound == currentGlobalSound {
			activeIndicator = lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Render(" ★")
		}

		if i == m.soundIndex {
			cursor = "❯ "
			soundStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#10B981"))
		}

		sb.WriteString(fmt.Sprintf("  %s%s%s\n", cursor, soundStyle.Render(sound), activeIndicator))
	}

	sb.WriteString("\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Italic(true).Render("[↑/↓] Navigate  •  [space] Play Preview  •  [enter] Save & Exit  •  [esc] Cancel"))

	cardStyle := lipgloss.NewStyle().
		Padding(1, 0).
		Margin(1, 0)

	return cardStyle.Render(sb.String())
}

func (m model) viewSpotifySettings() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#1DB954")).
		Padding(0, 3).
		Align(lipgloss.Center)

	title := titleStyle.Render("🔊 SPOTIFY AUTOPLAY SETTINGS")

	var sb strings.Builder
	sb.WriteString("  " + title + "\n\n")
	sb.WriteString("  Configure Spotify integration (Premium & Web API):\n\n")

	enabledStr := "Disabled"
	enabledStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Bold(true)
	if m.savedConfig != nil && m.savedConfig.SpotifyEnabled {
		enabledStr = "Enabled"
		enabledStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#1DB954")).Bold(true)
	}
	cursor0 := "  "
	labelStyle0 := lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB"))
	if m.spotifyFocusIndex == 0 {
		cursor0 = "❯ "
		labelStyle0 = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1DB954"))
	}
	sb.WriteString(fmt.Sprintf("  %s%s [%s]\n\n", cursor0, labelStyle0.Render("Spotify Autoplay:        "), enabledStyle.Render(enabledStr)))

	cursor1 := "  "
	labelStyle1 := lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB"))
	if m.spotifyFocusIndex == 1 {
		cursor1 = "❯ "
		labelStyle1 = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1DB954"))
	}
	sb.WriteString(fmt.Sprintf("  %s%s\n", cursor1, labelStyle1.Render("Playlist/Track URI:")))
	sb.WriteString(fmt.Sprintf("    %s\n\n", m.spotifyInputs[0].View()))

	cursor2 := "  "
	labelStyle2 := lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB"))
	if m.spotifyFocusIndex == 2 {
		cursor2 = "❯ "
		labelStyle2 = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1DB954"))
	}
	sb.WriteString(fmt.Sprintf("  %s%s\n\n", cursor2, labelStyle2.Render("Edit API Credentials (Client ID/Secret)")))

	cursor3 := "  "
	labelStyle3 := lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB"))
	isAuth := "Not Linked"
	authStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Bold(true)
	if m.savedConfig != nil && m.savedConfig.SpotifyRefreshToken != "" {
		isAuth = "Linked"
		authStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#1DB954")).Bold(true)
	}
	if m.spotifyFocusIndex == 3 {
		cursor3 = "❯ "
		labelStyle3 = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1DB954"))
	}
	sb.WriteString(fmt.Sprintf("  %s%s [Status: %s]\n\n", cursor3, labelStyle3.Render("Authorize Spotify Account"), authStyle.Render(isAuth)))

	cursor4 := "  "
	labelStyle4 := lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB"))
	if m.spotifyFocusIndex == 4 {
		cursor4 = "❯ "
		labelStyle4 = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1DB954"))
	}
	sb.WriteString(fmt.Sprintf("  %s%s\n", cursor4, labelStyle4.Render("Save Settings & Exit")))

	if m.configError != "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Bold(true)
		sb.WriteString(fmt.Sprintf("\n  %s\n", errStyle.Render(m.configError)))
	}

	sb.WriteString("\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Italic(true).Render("[tab/↑/↓] Switch Options  •  [space/enter] Select/Toggle  •  [esc] Cancel"))

	cardStyle := lipgloss.NewStyle().
		Padding(1, 0).
		Margin(1, 0)

	return cardStyle.Render(sb.String())
}

func (m model) viewSpotifyCredentials() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#1DB954")).
		Padding(0, 3).
		Align(lipgloss.Center)

	title := titleStyle.Render("🔑 EDIT SPOTIFY API CREDENTIALS")

	var sb strings.Builder
	sb.WriteString("  " + title + "\n\n")
	sb.WriteString("  Enter your Client ID and Secret (from developer.spotify.com):\n\n")

	labels := []string{
		"Client ID:    ",
		"Client Secret:",
	}

	for i := 0; i < 2; i++ {
		labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB"))
		if i == m.spotifyCredFocus {
			labelStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1DB954"))
		}
		sb.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render(labels[i]), m.spotifyCredInputs[i].View()))
	}

	if m.configError != "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Bold(true)
		sb.WriteString(fmt.Sprintf("\n  %s\n", errStyle.Render(m.configError)))
	} else {
		sb.WriteString("\n")
	}

	sb.WriteString("\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Italic(true).Render("[tab] Switch Fields  •  [enter] Save Credentials  •  [esc] Cancel"))

	cardStyle := lipgloss.NewStyle().
		Padding(1, 0).
		Margin(1, 0)

	return cardStyle.Render(sb.String())
}

func (m model) viewSpotifyAuthWaiting() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#1DB954")).
		Padding(0, 3).
		Align(lipgloss.Center)

	title := titleStyle.Render("⌛ WAITING FOR SPOTIFY AUTHENTICATION")

	var sb strings.Builder
	sb.WriteString("  " + title + "\n\n")
	sb.WriteString("  We have opened Spotify authorization page in your browser.\n")
	sb.WriteString("  Please log in and authorize the application.\n\n")
	sb.WriteString("  Listening locally on: " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#3B82F6")).Render("http://127.0.0.1:8080/callback") + "\n\n")
	sb.WriteString("  Waiting for response...\n\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Italic(true).Render("[esc / q] Cancel Authentication"))

	cardStyle := lipgloss.NewStyle().
		Padding(1, 0).
		Margin(1, 0)

	return cardStyle.Render(sb.String())
}

func (m model) viewTimer() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#7C3AED")).
		Padding(0, 3).
		Align(lipgloss.Center)

	title := titleStyle.Render(fmt.Sprintf("🍅 %s", strings.ToUpper(m.activeProfile.Name)))

	var phaseColor string
	switch m.currentSub {
	case subPhaseWork:
		phaseColor = "#F472B6"
	case subPhaseBreak:
		phaseColor = "#34D399"
	case subPhaseLongBreak:
		phaseColor = "#60A5FA"
	}

	phaseStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(phaseColor)).
		Underline(true)

	var statusStr string
	if m.currentSub == subPhaseLongBreak {
		statusStr = phaseStyle.Render(m.currentSub.String())
	} else {
		statusStr = phaseStyle.Render(fmt.Sprintf("%s (Group %d/%d)", m.currentSub.String(), m.currentGroup+1, len(m.activeProfile.Groups)))
	}

	var activeStr string
	if m.active {
		activeStr = lipgloss.NewStyle().Foreground(lipgloss.Color("#34D399")).Render("● RUNNING")
	} else {
		activeStr = lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF")).Render("○ PAUSED")
	}

	minutes := int(m.timeRemaining.Minutes())
	seconds := int(m.timeRemaining.Seconds()) % 60
	timerText := fmt.Sprintf("%02d:%02d", minutes, seconds)

	timerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F9FAFB"))

	percent := 1.0 - (float64(m.timeRemaining) / float64(m.totalDuration))
	if percent < 0 {
		percent = 0
	} else if percent > 1 {
		percent = 1
	}

	filledStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(phaseColor))
	emptyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#374151"))
	pBar := progressBar(30, percent, filledStyle, emptyStyle)

	completedTomatoesStr := ""
	for i := 0; i < m.completedGroups; i++ {
		completedTomatoesStr += "🍅 "
	}
	if completedTomatoesStr == "" {
		completedTomatoesStr = "None"
	}
	statsStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF"))
	stats := fmt.Sprintf("Tomatoes: %s (Count: %d)", completedTomatoesStr, m.completedGroups)

	var sbSpotify strings.Builder
	if m.savedConfig != nil && m.savedConfig.SpotifyEnabled {
		if m.spotifyError != "" {
			errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Bold(true)
			sbSpotify.WriteString(fmt.Sprintf("\n  ⚠️ Spotify: %s", errStyle.Render(m.spotifyError)))
		} else {
			sbSpotify.WriteString(fmt.Sprintf("\n  🔊 Spotify: Connected (Vol: %d%%)", m.spotifyVolume))
		}
	}

	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Italic(true)
	help := "[space] play/pause  •  [s] skip  •  [+]/[-] vol  •  [r] reset  •  [esc] menu"

	cardContent := fmt.Sprintf(
		"  %s\n\n"+
			"  Phase:    %s  (%s)\n\n"+
			"  Timer:    %s\n\n"+
			"  Progress: %s  %d%%\n\n"+
			"  %s%s\n\n"+
			"  %s",
		title,
		statusStr,
		activeStr,
		timerStyle.Render(timerText),
		pBar,
		int(percent*100),
		statsStyle.Render(stats),
		sbSpotify.String(),
		helpStyle.Render(help),
	)

	cardStyle := lipgloss.NewStyle().
		Padding(1, 0).
		Margin(1, 0)

	return cardStyle.Render(cardContent)
}

func (m model) viewConfigPathSettings() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#60A5FA")).
		Padding(0, 3).
		Align(lipgloss.Center)

	title := titleStyle.Render("🛠 CHANGE CONFIG STORAGE PATH")

	var sb strings.Builder
	sb.WriteString("  " + title + "\n\n")
	sb.WriteString("  Enter the new absolute path for the profiles.json configuration file:\n\n")
	sb.WriteString(fmt.Sprintf("  Path: %s\n\n", m.configPathInput.View()))

	sb.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF")).Italic(true).Render("Note: Saving will automatically copy your existing profiles, sounds,") + "\n")
	sb.WriteString("        " + lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF")).Italic(true).Render("and Spotify configurations to the new location.") + "\n")

	if m.configError != "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Bold(true)
		sb.WriteString(fmt.Sprintf("\n  ⚠️ %s\n", errStyle.Render(m.configError)))
	} else {
		sb.WriteString("\n")
	}

	sb.WriteString("\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Italic(true).Render("[enter] Save & Move  •  [esc] Cancel"))

	cardStyle := lipgloss.NewStyle().
		Padding(1, 0).
		Margin(1, 0)

	return cardStyle.Render(sb.String())
}

func (m model) View() tea.View {
	if m.quitting {
		return tea.NewView("\n  Thanks for using Gomodoro! Keep focusing! 🍅\n\n")
	}

	if m.cycleFinished {
		titleStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#10B981")).
			Padding(0, 3).
			Align(lipgloss.Center)

		title := titleStyle.Render(fmt.Sprintf("🎉 %s - CYCLE COMPLETE", strings.ToUpper(m.activeProfile.Name)))

		tomatoesStr := ""
		for i := 0; i < m.completedGroups; i++ {
			tomatoesStr += "🍅 "
		}
		if tomatoesStr == "" {
			tomatoesStr = "None"
		}

		var sb strings.Builder
		sb.WriteString("  " + title + "\n\n")
		sb.WriteString("  Congratulations! You completed the full cycle.\n")
		sb.WriteString(fmt.Sprintf("  Total Tomatoes Earned: %s (%d)\n\n", tomatoesStr, m.completedGroups))
		sb.WriteString("  Would you like to start a new cycle?\n")
		sb.WriteString("  [y] Yes, start fresh  •  [n] No, return to Main Menu\n")

		cardStyle := lipgloss.NewStyle().
			Padding(1, 0).
			Margin(1, 0)

		return tea.NewView(cardStyle.Render(sb.String()))
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

	return tea.NewView(content)
}

func main() {
	flag.Parse()

	savedCfg, _ := LoadConfig()

	m := initialModel()
	m.savedConfig = savedCfg

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Encountered error: %v\n", err)
		os.Exit(1)
	}
}

func progressBar(width int, percent float64, filledStyle, emptyStyle lipgloss.Style) string {
	if width < 5 {
		width = 20
	}
	filledCells := int(float64(width) * percent)
	if filledCells > width {
		filledCells = width
	}
	if filledCells < 0 {
		filledCells = 0
	}
	emptyCells := width - filledCells

	filledStr := filledStyle.Render(strings.Repeat("█", filledCells))
	emptyStr := emptyStyle.Render(strings.Repeat("░", emptyCells))
	return filledStr + emptyStr
}
