package main

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type theme struct {
	isDark bool

	text   color.Color
	muted  color.Color
	subtle color.Color
	border color.Color

	focus      color.Color
	shortBreak color.Color
	longBreak  color.Color
	warn       color.Color
	danger     color.Color
	success    color.Color
	spotify    color.Color
}

func newTheme(isDark bool) theme {
	ld := lipgloss.LightDark(isDark)
	c := lipgloss.Color
	return theme{
		isDark: isDark,

		text:   ld(c("#1F2328"), c("#E6EDF3")),
		muted:  ld(c("#59636E"), c("#9BA6B2")),
		subtle: ld(c("#8C959F"), c("#5B6573")),
		border: ld(c("#D0D7DE"), c("#3A4250")),

		focus:      ld(c("#C93C26"), c("#FF6F59")),
		shortBreak: ld(c("#1A7F37"), c("#4ADE80")),
		longBreak:  ld(c("#0969DA"), c("#60A5FA")),
		warn:       ld(c("#9A6700"), c("#FBBF24")),
		danger:     ld(c("#CF222E"), c("#F87171")),
		success:    ld(c("#1A7F37"), c("#4ADE80")),
		spotify:    ld(c("#168D40"), c("#1ED760")),
	}
}

func (t theme) fg(c color.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(c)
}

func (t theme) phaseColor(sub subPhase) color.Color {
	switch sub {
	case subPhaseBreak:
		return t.shortBreak
	case subPhaseLongBreak:
		return t.longBreak
	}
	return t.focus
}

type helpItem struct {
	key  string
	desc string
}

func (t theme) helpBar(items []helpItem, width int) string {
	keyStyle := t.fg(t.text).Bold(true)
	descStyle := t.fg(t.muted)
	sep := t.fg(t.subtle).Render(" · ")
	sepWidth := lipgloss.Width(sep)

	var lines []string
	var line string
	lineWidth := 0
	for _, it := range items {
		part := keyStyle.Render(it.key) + " " + descStyle.Render(it.desc)
		partWidth := lipgloss.Width(part)
		if lineWidth > 0 && lineWidth+sepWidth+partWidth > width {
			lines = append(lines, line)
			line, lineWidth = "", 0
		}
		if lineWidth > 0 {
			line += sep
			lineWidth += sepWidth
		}
		line += part
		lineWidth += partWidth
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func row(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func truncate(s string, width int) string {
	return ansi.Truncate(s, width, "…")
}

const (
	cardMaxWidth = 64
	cardMinWidth = 44
	cardChrome   = 8 // 2 border + 2×3 padding
)

func (m model) cardOuterWidth() int {
	w := cardMaxWidth
	if m.width > 0 && m.width-2 < w {
		w = max(cardMinWidth, m.width-2)
	}
	return w
}

func (m model) contentWidth() int {
	return m.cardOuterWidth() - cardChrome
}

func (m model) frame(accent color.Color, section, body string, help []helpItem) string {
	t := m.theme
	w := m.contentWidth()

	brand := t.fg(accent).Bold(true).Render("🍅 gomodoro")
	header := row(brand, t.fg(t.muted).Render(section), w)
	divider := t.fg(t.border).Render(strings.Repeat("─", w))

	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.border).
		Padding(1, 3).
		Width(m.cardOuterWidth()).
		Render(header + "\n" + divider + "\n\n" + strings.TrimRight(body, "\n"))

	parts := []string{card}
	if m.saveError != "" {
		parts = append(parts, t.fg(t.danger).Bold(true).Width(m.cardOuterWidth()).Render("! "+m.saveError))
	}
	if help != nil {
		parts = append(parts, lipgloss.NewStyle().Padding(0, 1).Render(t.helpBar(help, m.cardOuterWidth()-2)))
	}
	out := lipgloss.JoinVertical(lipgloss.Left, parts...)

	if m.width == 0 || m.height == 0 {
		return out
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, out)
}

func (m model) listItem(selected bool, accent color.Color, label, value string) string {
	t := m.theme
	w := m.contentWidth()
	if selected {
		left := t.fg(accent).Render("▌ ") + t.fg(t.text).Bold(true).Render(label)
		return row(left, t.fg(accent).Render(value), w)
	}
	return row("  "+t.fg(t.muted).Render(label), t.fg(t.subtle).Render(value), w)
}

func (m model) sectionLabel(text string) string {
	return m.theme.fg(m.theme.subtle).Bold(true).Render(strings.ToUpper(text))
}

func (m model) errorLine(text string) string {
	if text == "" {
		return ""
	}
	return "\n" + m.theme.fg(m.theme.danger).Width(m.contentWidth()).Render("✗ "+text) + "\n"
}
