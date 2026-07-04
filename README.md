# 🍅 Gomodoro

This is a terminal-based Pomodoro timer only for **MacOS**.

## Prerequisites

1. **Golang**: Version 1.18 or higher to build from source.
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

## Spotify Setup

To enable Spotify autoplay:

1. Go to the [Spotify Developer Dashboard](https://developer.spotify.com/) and log in.
2. Click **Create App**:
   - **App Name**: `Gomodoro` (or any name you like)
   - **Redirect URIs**: Must be exactly **`http://127.0.0.1:8080/callback`**.
   - Copy the **Client ID** and **Client Secret**.
3. Open Gomodoro, go to **Settings > Configure Spotify Autoplay > Edit API Credentials**, and paste your **Client ID** and **Client Secret**.
4. Choose **Authorize Spotify Account**. This will open a browser window to link your account. Once done, return to the terminal. It will display **"Linked"** in the config.

## Configuration

All custom profiles and settings are stored at: `~/.config/gomodoro/profiles.json`

You can change this path inside **Settings > Change Config Storage Path**.
