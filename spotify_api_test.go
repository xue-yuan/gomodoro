package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var farFuture = time.Now().Add(24 * time.Hour)

func stripANSI(s string) string { return ansi.Strip(s) }

type fakeSpotify struct {
	mu       sync.Mutex
	devices  string
	active   string
	requests []string
}

func (f *fakeSpotify) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)

	noActive := func() {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":{"status":404,"message":"Player command failed: No active device found","reason":"NO_ACTIVE_DEVICE"}}`)
	}
	switch {
	case r.URL.Path == "/devices":
		fmt.Fprintf(w, `{"devices":[%s]}`, f.devices)
	case r.Method == http.MethodGet && r.URL.Path == "/":
		if f.active == "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		fmt.Fprintf(w, `{"is_playing":true,"device":{"id":%q,"name":"MacBook","volume_percent":40}}`, f.active)
	case r.Method == http.MethodPut && r.URL.Path == "/":
		var body struct {
			DeviceIDs []string `json:"device_ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.active = body.DeviceIDs[0]
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPut:
		id := r.URL.Query().Get("device_id")
		if id == "" && f.active == "" {
			noActive()
			return
		}
		if id != "" {
			f.active = id
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func useFakeSpotify(t *testing.T, devices string) *fakeSpotify {
	t.Helper()
	f := &fakeSpotify{devices: devices}
	srv := httptest.NewServer(f)
	orig := spotifyAPIBase
	spotifyAPIBase = srv.URL
	t.Cleanup(func() {
		spotifyAPIBase = orig
		srv.Close()
	})
	return f
}

var validCreds = spotifyCreds{AccessToken: "token", RefreshToken: "refresh", Expiry: farFuture}

func TestPlayFallsBackToAvailableDevice(t *testing.T) {
	f := useFakeSpotify(t, `{"id":"phone","name":"iPhone","type":"Smartphone","is_active":false,"is_restricted":false},
		{"id":"mac","name":"MacBook","type":"Computer","is_active":false,"is_restricted":false}`)

	msg := playSpotifyCmd(validCreds, "spotify:playlist:abc", true, spotifyDeviceRef{})().(spotifyResultMsg)
	if msg.err != nil {
		t.Fatalf("play failed: %v (requests: %v)", msg.err, f.requests)
	}
	if f.active != "mac" {
		t.Fatalf("should play on the computer, active = %q", f.active)
	}
	if msg.device != "MacBook" || msg.volume == nil || *msg.volume != 40 {
		t.Fatalf("device/volume not reported: %+v", msg)
	}
}

func TestPlayWithNoDevicesExplainsWhatToDo(t *testing.T) {
	useFakeSpotify(t, ``)
	msg := playSpotifyCmd(validCreds, "spotify:playlist:abc", false, spotifyDeviceRef{})().(spotifyResultMsg)
	if !errors.Is(msg.err, errNoSpotifyDevice) {
		t.Fatalf("err = %v, want errNoSpotifyDevice", msg.err)
	}
}

func TestVolumeFallsBackToAvailableDevice(t *testing.T) {
	f := useFakeSpotify(t, `{"id":"mac","name":"MacBook","type":"Computer"}`)
	msg := changeSpotifyVolumeCmd(validCreds, 30, spotifyDeviceRef{})().(spotifyResultMsg)
	if msg.err != nil || f.active != "mac" {
		t.Fatalf("volume err = %v, active = %q", msg.err, f.active)
	}
}

func TestPickSpotifyDevice(t *testing.T) {
	devices := []SpotifyDevice{
		{ID: "r", Type: "Computer", IsRestricted: true},
		{ID: "speaker", Type: "Speaker"},
		{ID: "mac", Type: "Computer"},
	}
	if d, _ := pickSpotifyDevice(devices); d.ID != "mac" {
		t.Errorf("picked %q, want mac (unrestricted computer)", d.ID)
	}
	devices = append(devices, SpotifyDevice{ID: "tv", Type: "TV", IsActive: true})
	if d, _ := pickSpotifyDevice(devices); d.ID != "tv" {
		t.Errorf("picked %q, want the active device", d.ID)
	}
	if _, ok := pickSpotifyDevice([]SpotifyDevice{{ID: "r", IsRestricted: true}}); ok {
		t.Error("restricted devices must not be picked")
	}
}

func TestSpotifyErrorWrapsInsteadOfTruncating(t *testing.T) {
	m := initialModel(&Config{SpotifyEnabled: true, SpotifyRefreshToken: "x", SpotifyURI: "spotify:playlist:abc"})
	m.width, m.height = 80, 30
	m.spotifyError = errNoSpotifyDevice.Error()
	line := m.spotifyStatusLine()
	if strings.Contains(line, "…") {
		t.Fatalf("error was truncated: %q", line)
	}
	for _, l := range strings.Split(line, "\n") {
		if w := lipgloss.Width(l); w > m.contentWidth() {
			t.Fatalf("line is %d wide, card content is %d", w, m.contentWidth())
		}
	}
	if !strings.Contains(strings.Join(strings.Fields(stripANSI(line)), " "), "press space again.") {
		t.Fatalf("full message not shown: %q", stripANSI(line))
	}
}

func spotifyModel(autoStart bool) model {
	m := initialModel(&Config{
		AutoStart:           autoStart,
		SpotifyEnabled:      true,
		SpotifyURI:          "spotify:playlist:abc",
		SpotifyRefreshToken: "refresh",
		SpotifyAccessToken:  "token",
		SpotifyTokenExpiry:  farFuture,
	})
	m.notifier = nopNotifier{}
	return m
}

func TestSpotifyCommandsAreSerialized(t *testing.T) {
	m := spotifyModel(false)

	if cmd := m.triggerSpotifyPlay(); cmd == nil || !m.spotifyBusy || m.spotifyInFlight != spotifyActionPlay {
		t.Fatal("play should be sent immediately")
	}
	if cmd := m.triggerSpotifyPause(); cmd != nil {
		t.Fatal("pause must not be sent while another command is in flight")
	}
	if cmd := m.handleSpotifyResult(spotifyResultMsg{action: spotifyActionPlay, loadedContext: true}); cmd == nil || m.spotifyInFlight != spotifyActionPause {
		t.Fatal("the queued pause should be sent once play finishes")
	}
	if cmd := m.handleSpotifyResult(spotifyResultMsg{action: spotifyActionPause}); cmd != nil {
		t.Fatal("nothing else should be pending")
	}
	if m.spotifyState != spotifyPaused {
		t.Fatalf("final state = %v, want paused (matches what was sent last)", m.spotifyState)
	}
}

func TestLatestPlaybackWishWins(t *testing.T) {
	m := spotifyModel(false)
	m.triggerSpotifyPlay()
	m.triggerSpotifyPause()
	m.triggerSpotifyPlay()
	m.handleSpotifyResult(spotifyResultMsg{action: spotifyActionPlay, loadedContext: true})
	if m.spotifyInFlight != spotifyActionPlay {
		t.Fatalf("in flight = %v, want play (the latest wish)", m.spotifyInFlight)
	}
}

func TestAutoStartFocusAfterBreakOnlyPlays(t *testing.T) {
	m := spotifyModel(true)
	m.startProfile(defaultProfile())
	m.handleSpotifyResult(spotifyResultMsg{action: spotifyActionPlay, loadedContext: true})

	m, _ = update(t, m, key("s"))
	if m.currentSub != subPhaseBreak || m.spotifyInFlight != spotifyActionPause || m.spotifyWantPlay != nil {
		t.Fatalf("focus→break: sub=%v inFlight=%v", m.currentSub, m.spotifyInFlight)
	}
	m.handleSpotifyResult(spotifyResultMsg{action: spotifyActionPause})

	m, _ = update(t, m, key("s"))
	if m.currentSub != subPhaseWork || !m.active {
		t.Fatalf("break→focus: sub=%v active=%v", m.currentSub, m.active)
	}
	if m.spotifyInFlight != spotifyActionPlay || m.spotifyWantPlay != nil {
		t.Fatalf("break→focus should send only play, inFlight=%v pending=%v", m.spotifyInFlight, m.spotifyWantPlay)
	}
	m.handleSpotifyResult(spotifyResultMsg{action: spotifyActionPlay})
	if m.spotifyState != spotifyPlaying {
		t.Fatal("should be playing")
	}

	m, _ = update(t, m, key("space"))
	if m.active || m.spotifyInFlight != spotifyActionPause {
		t.Fatal("a single space should pause both the timer and Spotify")
	}
}

func TestFailedFirstPlayLoadsPlaylistAgain(t *testing.T) {
	m := spotifyModel(false)
	m.triggerSpotifyPlay()
	m.handleSpotifyResult(spotifyResultMsg{action: spotifyActionPlay, err: errNoSpotifyDevice})
	if m.spotifyLoaded {
		t.Fatal("a failed play must not mark the playlist as loaded")
	}
	m.triggerSpotifyPlay()
	m.handleSpotifyResult(spotifyResultMsg{action: spotifyActionPlay, loadedContext: true})
	if !m.spotifyLoaded {
		t.Fatal("a successful play with the playlist should mark it loaded")
	}
}

func TestSpaceDuringBreakLeavesSpotifyAlone(t *testing.T) {
	useTempConfigDir(t)
	m := spotifyModel(true)
	m.startProfile(defaultProfile())
	m.handleSpotifyResult(spotifyResultMsg{action: spotifyActionPlay, loadedContext: true})
	m, _ = update(t, m, key("s"))
	m.handleSpotifyResult(spotifyResultMsg{action: spotifyActionPause})
	if m.currentSub != subPhaseBreak || !m.active {
		t.Fatal("expected a running break")
	}

	for i := 0; i < 2; i++ {
		m, _ = update(t, m, key("space"))
		if m.spotifyBusy || m.spotifyWantPlay != nil {
			t.Fatalf("space #%d during a break should not send Spotify commands", i+1)
		}
	}
	if line := stripANSI(m.spotifyStatusLine()); !strings.Contains(line, "paused for the break") {
		t.Fatalf("Spotify line should explain the break pause, got %q", line)
	}
}

const phoneAndSpeaker = `{"id":"phone","name":"iPhone","type":"Smartphone","is_active":true},
	{"id":"speaker","name":"Kitchen","type":"Speaker","is_active":false,"volume_percent":25},
	{"id":"mac","name":"MacBook","type":"Computer","is_active":false}`

func TestPlayUsesPreferredDeviceOverActiveOne(t *testing.T) {
	f := useFakeSpotify(t, phoneAndSpeaker)
	msg := playSpotifyCmd(validCreds, "spotify:playlist:abc", true, spotifyDeviceRef{ID: "speaker", Name: "Kitchen"})().(spotifyResultMsg)
	if msg.err != nil {
		t.Fatalf("play failed: %v", msg.err)
	}
	if f.active != "speaker" || msg.device != "Kitchen" || msg.missingDevice != "" {
		t.Fatalf("active = %q, device = %q, missing = %q", f.active, msg.device, msg.missingDevice)
	}
	if msg.volume == nil || *msg.volume != 25 {
		t.Fatalf("volume should come from the chosen device, got %v", msg.volume)
	}
}

func TestResumeOnPreferredDeviceTransfersPlayback(t *testing.T) {
	f := useFakeSpotify(t, phoneAndSpeaker)
	msg := playSpotifyCmd(validCreds, "", false, spotifyDeviceRef{ID: "speaker", Name: "Kitchen"})().(spotifyResultMsg)
	if msg.err != nil || f.active != "speaker" {
		t.Fatalf("err = %v, active = %q", msg.err, f.active)
	}
	if last := f.requests[len(f.requests)-1]; last != "PUT /?" {
		t.Fatalf("resuming on an inactive device should transfer playback, last request = %q", last)
	}
}

func TestPreferredDeviceFoundByNameWhenIDChanged(t *testing.T) {
	f := useFakeSpotify(t, phoneAndSpeaker)
	msg := playSpotifyCmd(validCreds, "spotify:playlist:abc", false, spotifyDeviceRef{ID: "old-id", Name: "kitchen"})().(spotifyResultMsg)
	if msg.err != nil || f.active != "speaker" || msg.missingDevice != "" {
		t.Fatalf("err = %v, active = %q, missing = %q", msg.err, f.active, msg.missingDevice)
	}
}

func TestOfflinePreferredDeviceFallsBack(t *testing.T) {
	f := useFakeSpotify(t, phoneAndSpeaker)
	msg := playSpotifyCmd(validCreds, "spotify:playlist:abc", false, spotifyDeviceRef{ID: "tv", Name: "Living room"})().(spotifyResultMsg)
	if msg.err != nil || f.active != "phone" {
		t.Fatalf("should fall back to the active device: err = %v, active = %q", msg.err, f.active)
	}
	if msg.missingDevice != "Living room" {
		t.Fatalf("missing = %q, want the unavailable device's name", msg.missingDevice)
	}
}

func TestDevicePickerOptions(t *testing.T) {
	devices := []SpotifyDevice{
		{ID: "mac", Name: "MacBook", Type: "Computer", IsActive: true},
		{ID: "r", Name: "Locked", IsRestricted: true},
	}
	p := devicePicker{current: spotifyDeviceRef{ID: "tv", Name: "Living room"}, devices: devices}
	opts := p.options()
	if len(opts) != 3 || !opts[0].ref.isAuto() || opts[1].ref.ID != "mac" {
		t.Fatalf("want Automatic, MacBook, offline current; got %+v", opts)
	}
	if last := opts[2]; !last.offline || !last.current || last.ref.Name != "Living room" {
		t.Fatalf("offline current choice should stay listed and marked: %+v", last)
	}

	p.current = spotifyDeviceRef{}
	if opts := p.options(); len(opts) != 2 || !opts[0].current {
		t.Fatalf("Automatic should be marked current: %+v", opts)
	}
}

func TestDevicePickerSelectsCurrentWhenLoaded(t *testing.T) {
	m := spotifyModel(false)
	m.openDevicePicker(spotifyDeviceRef{ID: "mac", Name: "MacBook"})
	m.receiveSpotifyDevices(spotifyDevicesMsg{devices: []SpotifyDevice{{ID: "phone", Name: "iPhone"}, {ID: "mac", Name: "MacBook"}}})
	if m.devicePicker.loading || m.devicePicker.index != 2 {
		t.Fatalf("loading = %v, index = %d, want the current device (2) selected", m.devicePicker.loading, m.devicePicker.index)
	}
	chosen, _ := m.updateDevicePicker(key("up"))
	if chosen != nil {
		t.Fatal("moving should not choose")
	}
	chosen, _ = m.updateDevicePicker(key("enter"))
	if chosen == nil || chosen.ID != "phone" || m.devicePicker.open {
		t.Fatalf("enter should choose iPhone and close the picker, got %+v", chosen)
	}
}

func TestSessionDeviceSwitchMovesPlayingMusic(t *testing.T) {
	m := spotifyModel(false)
	m.startProfile(defaultProfile())
	kitchen := spotifyDeviceRef{ID: "speaker", Name: "Kitchen"}

	if cmd := m.chooseSessionDevice(kitchen); cmd != nil || m.spotifyTarget != kitchen {
		t.Fatal("before anything plays, choosing a device should only remember it")
	}

	m.triggerSpotifyPlay()
	m.handleSpotifyResult(spotifyResultMsg{action: spotifyActionPlay, loadedContext: true})
	mac := spotifyDeviceRef{ID: "mac", Name: "MacBook"}
	if cmd := m.chooseSessionDevice(mac); cmd == nil || m.spotifyInFlight != spotifyActionPlay {
		t.Fatal("switching while playing should send play to move the music")
	}
	if m.savedConfig.SpotifyDeviceID != "" {
		t.Fatal("a session choice must not change the saved default")
	}
}

func TestTimerDeviceKeyOpensPickerAndKeepsTicking(t *testing.T) {
	m := spotifyModel(true)
	m.startProfile(defaultProfile())
	m.handleSpotifyResult(spotifyResultMsg{action: spotifyActionPlay, loadedContext: true})

	m, _ = update(t, m, key("d"))
	if !m.devicePicker.open {
		t.Fatal("d should open the device picker")
	}
	if _, cmd := update(t, m, tickMsg{id: m.tickID}); cmd == nil {
		t.Fatal("the timer must keep ticking while the picker is open")
	}
	m, _ = update(t, m, key("space"))
	if !m.active || m.devicePicker.open {
		t.Fatal("space should choose in the picker, not pause the timer")
	}
}

func TestSpotifySettingsSavesDefaultDevice(t *testing.T) {
	useTempConfigDir(t)
	m := spotifyModel(false)
	m.state = stateSpotifySettings
	m.initSpotifyInputs()
	m.spotifyDeviceDraft = spotifyDeviceRef{ID: "speaker", Name: "Kitchen"}
	if !m.spotifyDirty() {
		t.Fatal("changing the device should mark the page dirty")
	}
	m.setSpotifyFocus(5)
	m, _ = update(t, m, key("enter"))
	if got := m.savedConfig.SpotifyDevice(); got.ID != "speaker" || got.Name != "Kitchen" {
		t.Fatalf("saved device = %+v", got)
	}
}

func TestNormalizeSpotifyURI(t *testing.T) {
	cases := map[string]string{
		"https://open.spotify.com/playlist/0vvXs?si=9117":   "spotify:playlist:0vvXs",
		" https://open.spotify.com/intl-ja/track/51iz?si=x": "spotify:track:51iz",
		"https://open.spotify.com/album/abc":                "spotify:album:abc",
		"spotify:playlist:0vvXs?si=9117":                    "spotify:playlist:0vvXs",
		"spotify:track:51iz":                                "spotify:track:51iz",
		"https://example.com/playlist/abc":                  "https://example.com/playlist/abc",
		"":                                                  "",
	}
	for in, want := range cases {
		if got := normalizeSpotifyURI(in); got != want {
			t.Errorf("normalizeSpotifyURI(%q) = %q, want %q", in, got, want)
		}
	}
}
