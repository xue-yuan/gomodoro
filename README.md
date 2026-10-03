# 🍅 Gomodoro

This is a terminal-based Pomodoro timer only for **MacOS**.

## Prerequisites

1. **Golang**: Version 1.26.4 or higher to build from source (see `go.mod`).
2. **Spotify Premium**: (Optional, required for Spotify playback controls).

## Installation & Build

Clone the repository and build the binary:

```bash
cd gomodoro
go build -o gomodoro
```

To run the application:

```bash
./gomodoro
```

## Usage

| Screen | Keys |
|---|---|
| Home | `↑/↓` move · `enter` select · `q` quit |
| Timer | `space` start/pause · `s` skip · `r` restart phase · `+/-` Spotify volume · `d` Spotify device for this session · `esc` back to menu (session can be resumed) · `q` quit (asks to confirm) |
| Profiles | `enter` start · `n` new · `e` edit · `s` use for Quick start · `d` delete (asks to confirm) |

- **Quick start** runs the classic 4 × 25m/5m cycle, or the profile you marked with `s` in **Profiles**.
- The terminal tab title shows the remaining time, so you can keep an eye on it from other tabs.
- The theme adapts to light and dark terminal backgrounds.

## Spotify Setup

To enable Spotify autoplay:

1. Go to the [Spotify Developer Dashboard](https://developer.spotify.com/) and log in.
2. Click **Create App**:
   - **App Name**: `Gomodoro` (or any name you like)
   - **Redirect URIs**: Must be exactly **`http://127.0.0.1:8080/callback`**.
   - Copy the **Client ID** and **Client Secret**.
3. Open Gomodoro, go to **Settings › Spotify autoplay › API credentials**, and paste your **Client ID** and **Client Secret**.
4. Choose **Authorize account**. This opens a browser window to link your account (press `o` to open it again or `c` to copy the link). Once done, the status shows **Linked**.
5. Enter a playlist or track URI, turn **Autoplay** on, and choose **Save**.
6. Optionally pick a **Playback device** as the default. **Automatic** uses the device that is already playing, or else this computer. If the chosen device is offline, Gomodoro falls back to Automatic. Press `d` on the timer to switch devices for the current session; music that is playing moves to the new device.

## Configuration

All custom profiles and settings are stored at: `~/.config/gomodoro/profiles.json`

The file contains your Spotify Client Secret and tokens, so it is written with `0600` permissions (readable only by you).

You can change this path inside **Settings > Change Config Storage Path**. The path must be absolute or start with `~/`:

- If the file does not exist, your current settings are copied there.
- If it is already a gomodoro config (e.g. synced from another machine), it is used as-is.
- Any other existing file is never overwritten.

If the config file cannot be read or is invalid (for example, a profile with no groups), Gomodoro exits with an error instead of starting with empty settings, so your config is never overwritten by mistake.
