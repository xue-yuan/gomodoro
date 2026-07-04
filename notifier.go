package main

import (
	"fmt"
	"os/exec"
)

type Notifier interface {
	Notify(title, subtitle, message, sound string) error
}

type AppleScriptNotifier struct{}

func NewNotifier() AppleScriptNotifier {
	return AppleScriptNotifier{}
}

func (n AppleScriptNotifier) Notify(title, subtitle, message, sound string) error {
	if sound == "" {
		sound = "Glass"
	}

	escapedMsg := ""
	for _, char := range message {
		if char == '"' {
			escapedMsg += `\"`
		} else {
			escapedMsg += string(char)
		}
	}
	escapedTitle := ""
	for _, char := range title {
		if char == '"' {
			escapedTitle += `\"`
		} else {
			escapedTitle += string(char)
		}
	}
	escapedSubtitle := ""
	for _, char := range subtitle {
		if char == '"' {
			escapedSubtitle += `\"`
		} else {
			escapedSubtitle += string(char)
		}
	}

	script := fmt.Sprintf(`display notification "%s" with title "%s" subtitle "%s" sound name "%s"`, escapedMsg, escapedTitle, escapedSubtitle, sound)
	cmd := exec.Command("osascript", "-e", script)
	return cmd.Run()
}
