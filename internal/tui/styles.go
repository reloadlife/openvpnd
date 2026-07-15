package tui

import "github.com/charmbracelet/lipgloss"

// Adaptive colors: readable on both dark and light terminals.
var (
	cAccent  = lipgloss.AdaptiveColor{Light: "#0E7490", Dark: "#67E8F9"} // cyan (OpenVPN-ish)
	cAccent2 = lipgloss.AdaptiveColor{Light: "#4F46E5", Dark: "#A5B4FC"}
	cText    = lipgloss.AdaptiveColor{Light: "#111827", Dark: "#F3F4F6"}
	cMuted   = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}
	cBorder  = lipgloss.AdaptiveColor{Light: "#A5F3FC", Dark: "#155E75"}
	cOK      = lipgloss.AdaptiveColor{Light: "#047857", Dark: "#34D399"}
	cWarn    = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FBBF24"}
	cErr     = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"}
	cSelFg   = lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#0B1020"}
	cSelBg   = lipgloss.AdaptiveColor{Light: "#0E7490", Dark: "#67E8F9"}
	cBarBg   = lipgloss.AdaptiveColor{Light: "#ECFEFF", Dark: "#164E63"}
	cBarFg   = lipgloss.AdaptiveColor{Light: "#155E75", Dark: "#CFFAFE"}
	cBadgeFg = lipgloss.AdaptiveColor{Light: "#064E3B", Dark: "#022C22"}
	cUpBg    = lipgloss.AdaptiveColor{Light: "#6EE7B7", Dark: "#059669"}
	cDownBg  = lipgloss.AdaptiveColor{Light: "#D1D5DB", Dark: "#4B5563"}
	cSuspBg  = lipgloss.AdaptiveColor{Light: "#FCD34D", Dark: "#D97706"}
	cConnBg  = lipgloss.AdaptiveColor{Light: "#93C5FD", Dark: "#2563EB"}
	cSrvBg   = lipgloss.AdaptiveColor{Light: "#C4B5FD", Dark: "#7C3AED"}
	cCliBg   = lipgloss.AdaptiveColor{Light: "#FBCFE8", Dark: "#DB2777"}
	cHead    = lipgloss.AdaptiveColor{Light: "#155E75", Dark: "#67E8F9"}
)

var (
	tabActive   = lipgloss.NewStyle().Bold(true).Foreground(cSelFg).Background(cSelBg).Padding(0, 2)
	tabInactive = lipgloss.NewStyle().Foreground(cMuted).Padding(0, 2)
	helpStyle   = lipgloss.NewStyle().Foreground(cMuted)
	errStyle    = lipgloss.NewStyle().Foreground(cErr).Bold(true)
	okStyle     = lipgloss.NewStyle().Foreground(cOK).Bold(true)
	warnStyle   = lipgloss.NewStyle().Foreground(cWarn).Bold(true)
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(cHead)
	statusStyle = lipgloss.NewStyle().Foreground(cBarFg).Background(cBarBg).Padding(0, 1)
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(cAccent).MarginBottom(1)
	panelStyle  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cBorder).Padding(1, 2)
	labelStyle  = lipgloss.NewStyle().Foreground(cAccent2).Width(18)
	valueStyle  = lipgloss.NewStyle().Foreground(cText)
	focusStyle  = lipgloss.NewStyle().Bold(true).Foreground(cSelFg).Background(cSelBg)
	dimStyle    = lipgloss.NewStyle().Foreground(cMuted)
	selStyle    = lipgloss.NewStyle().Bold(true).Foreground(cSelFg).Background(cSelBg)
	badgeUp     = lipgloss.NewStyle().Foreground(cBadgeFg).Background(cUpBg).Padding(0, 1).Bold(true)
	badgeDown   = lipgloss.NewStyle().Foreground(cText).Background(cDownBg).Padding(0, 1)
	badgeSusp   = lipgloss.NewStyle().Foreground(cBadgeFg).Background(cSuspBg).Padding(0, 1).Bold(true)
	badgeConn   = lipgloss.NewStyle().Foreground(cBadgeFg).Background(cConnBg).Padding(0, 1).Bold(true)
	badgeSrv    = lipgloss.NewStyle().Foreground(cSelFg).Background(cSrvBg).Padding(0, 1).Bold(true)
	badgeCli    = lipgloss.NewStyle().Foreground(cSelFg).Background(cCliBg).Padding(0, 1).Bold(true)
	sectionStyle = lipgloss.NewStyle().Bold(true).Foreground(cAccent).MarginTop(1).MarginBottom(0)
	tipBoxStyle  = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cAccent2).
			Padding(0, 1).
			MarginBottom(1)
)
