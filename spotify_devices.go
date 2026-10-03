package main

import (
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type devicePicker struct {
	open    bool
	loading bool
	current spotifyDeviceRef
	devices []SpotifyDevice
	index   int
	err     string
}

type deviceOption struct {
	ref     spotifyDeviceRef
	kind    string
	active  bool
	offline bool
	current bool
}

func (p devicePicker) options() []deviceOption {
	opts := []deviceOption{{current: p.current.isAuto()}}
	match, found := matchSpotifyDevice(p.devices, p.current)
	for _, d := range p.devices {
		if d.IsRestricted || d.ID == "" {
			continue
		}
		opts = append(opts, deviceOption{
			ref:     spotifyDeviceRef{ID: d.ID, Name: d.Name},
			kind:    d.Type,
			active:  d.IsActive,
			current: found && d.ID == match.ID,
		})
	}
	if !p.current.isAuto() && !found && !p.loading {
		opts = append(opts, deviceOption{ref: p.current, offline: true, current: true})
	}
	return opts
}

func (m *model) openDevicePicker(current spotifyDeviceRef) tea.Cmd {
	m.devicePicker = devicePicker{open: true, loading: true, current: current}
	if m.savedConfig.SpotifyRefreshToken == "" {
		m.devicePicker.loading = false
		m.devicePicker.err = "Link your Spotify account first (Settings › Spotify › Authorize account)."
		return nil
	}
	return listSpotifyDevicesCmd(credsFromConfig(m.savedConfig))
}

func (m *model) receiveSpotifyDevices(msg spotifyDevicesMsg) {
	if msg.token != nil {
		m.storeSpotifyToken(msg.token, msg.fetchedAt)
	}
	p := &m.devicePicker
	if !p.open || !p.loading {
		return
	}
	p.loading = false
	if msg.err != nil {
		p.err = msg.err.Error()
		return
	}
	p.devices = msg.devices
	p.err = ""
	for i, o := range p.options() {
		if o.current {
			p.index = i
		}
	}
}

func (m *model) updateDevicePicker(key tea.KeyPressMsg) (*spotifyDeviceRef, tea.Cmd) {
	p := &m.devicePicker
	opts := p.options()
	p.index = min(p.index, len(opts)-1)
	switch key.String() {
	case "esc":
		p.open = false
	case "up", "k":
		p.index = wrapIndex(p.index-1, len(opts))
	case "down", "j":
		p.index = wrapIndex(p.index+1, len(opts))
	case "r":
		return nil, m.openDevicePicker(p.current)
	case "enter", "space":
		chosen := opts[p.index].ref
		p.open = false
		return &chosen, nil
	}
	return nil, nil
}

func (m *model) chooseSessionDevice(d spotifyDeviceRef) tea.Cmd {
	m.spotifyTarget = d
	m.spotifyMissingDevice = ""
	if d.isAuto() || !m.spotifyWillPlay() {
		return nil
	}
	return m.triggerSpotifyPlay()
}

func (m *model) spotifyWillPlay() bool {
	if m.spotifyWantPlay != nil {
		return *m.spotifyWantPlay
	}
	if m.spotifyBusy && m.spotifyInFlight != spotifyActionVolume {
		return m.spotifyInFlight == spotifyActionPlay
	}
	return m.spotifyState == spotifyPlaying
}

func (m model) viewDevicePicker(accent color.Color, section, intro string) string {
	t := m.theme
	p := m.devicePicker
	w := m.contentWidth()
	var sb strings.Builder
	sb.WriteString(t.fg(t.muted).Width(w).Render(intro) + "\n\n")

	opts := p.options()
	idx := min(p.index, len(opts)-1)
	for i, o := range opts {
		var value string
		switch {
		case o.ref.isAuto():
			value = "playing device, else this computer"
		case o.offline:
			value = "not available"
		case o.active:
			value = o.kind + " · active"
		default:
			value = o.kind
		}
		label := truncate(o.ref.label(), max(8, w-lipgloss.Width(value)-8))
		if o.current {
			label += " ★"
		}
		sb.WriteString(m.listItem(i == idx, accent, label, value) + "\n")
	}

	switch {
	case p.loading:
		sb.WriteString("\n" + t.fg(t.subtle).Render("Looking for devices…") + "\n")
	case p.err == "" && len(opts) == 1:
		sb.WriteString("\n" + t.fg(t.muted).Width(w).Render("No devices found. Open Spotify on the device you want, then press r.") + "\n")
	}
	sb.WriteString(m.errorLine(p.err))
	sb.WriteString("\n" + t.fg(t.subtle).Render("★ current choice"))

	return m.frame(accent, section, sb.String(), []helpItem{
		{"↑/↓", "move"}, {"enter", "choose"}, {"r", "refresh"}, {"esc", "cancel"},
	})
}
