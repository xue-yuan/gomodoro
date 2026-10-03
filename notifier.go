package main

import (
	"fmt"
	"os/exec"
)

type Notifier interface {
	Notify(title, subtitle, message string) error
	PlaySound(name string)
}

type AppleScriptNotifier struct{}

func NewNotifier() AppleScriptNotifier {
	return AppleScriptNotifier{}
}

const notifyScript = `on run argv
	display notification (item 1 of argv) with title (item 2 of argv) subtitle (item 3 of argv)
end run`

func (n AppleScriptNotifier) Notify(title, subtitle, message string) error {
	return exec.Command("osascript", "-e", notifyScript, message, title, subtitle).Run()
}

func (n AppleScriptNotifier) PlaySound(name string) {
	path := fmt.Sprintf("/System/Library/Sounds/%s.aiff", validSound(name))
	_ = exec.Command("afplay", path).Run()
}
