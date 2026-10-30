// Package tui draws both halves of nsp. The presenter and the speaker run
// the same binary and the same look, differing only in what they are each
// able to do: the presenter admits and mutes speakers, the speaker drives.
package tui

import "github.com/charmbracelet/lipgloss"

// Adaptive colours so the same palette reads on light and dark terminals.
var (
	accent = lipgloss.AdaptiveColor{Light: "#8839ef", Dark: "#cba6f7"}
	good   = lipgloss.AdaptiveColor{Light: "#40a02b", Dark: "#a6e3a1"}
	bad    = lipgloss.AdaptiveColor{Light: "#d20f39", Dark: "#f38ba8"}
	muted  = lipgloss.AdaptiveColor{Light: "#6c6f85", Dark: "#9399b2"}
	border = lipgloss.AdaptiveColor{Light: "#bcc0cc", Dark: "#45475a"}
)

var (
	titleStyle = lipgloss.NewStyle().
			Foreground(accent).
			Bold(true)

	codeStyle = lipgloss.NewStyle().
			Foreground(accent).
			Bold(true).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).
			Padding(0, 2)

	labelStyle = lipgloss.NewStyle().Foreground(muted)

	goodStyle = lipgloss.NewStyle().Foreground(good)
	badStyle  = lipgloss.NewStyle().Foreground(bad)

	helpStyle = lipgloss.NewStyle().Foreground(muted)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Padding(0, 1)

	selectedStyle = lipgloss.NewStyle().
			Foreground(accent).
			Bold(true)
)
