package view

import (
	"fmt"
	"strings"

	"im-tui/alert"

	"github.com/charmbracelet/lipgloss"
)

// RenderAlerts renders Tab 6: Alert history and active alerts.
func RenderAlerts(width, height int, ev *alert.Evaluator, scrollPos int) string {
	if width < 20 {
		return "Terminal too narrow"
	}

	if ev == nil {
		return renderCentered(width, height-2, LabelStyle.Render("Alert system initializing..."))
	}

	active := ev.Active()
	history := ev.History()

	// Top: active alerts
	activeH := 8
	if len(active) > 5 {
		activeH = len(active) + 4
	}
	if activeH > height/3 {
		activeH = height / 3
	}

	historyH := height - 2 - activeH

	activeContent := renderActiveAlerts(width-2, activeH-2, active)
	activePanel := Panel("Active Alerts", activeContent, width, activeH)

	// Bottom: thresholds (left) + history (right)
	threshW := 35
	histW := width - threshW - 1

	threshContent := renderThresholds(threshW-2, historyH-2)
	threshPanel := Panel("Thresholds", threshContent, threshW, historyH)

	histContent := renderAlertHistory(histW-2, historyH-2, history, scrollPos)
	histPanel := Panel("Alert History", histContent, histW, historyH)

	bottomRow := lipgloss.JoinHorizontal(lipgloss.Top, threshPanel, histPanel)

	return lipgloss.JoinVertical(lipgloss.Left, activePanel, bottomRow)
}

func renderActiveAlerts(w, h int, active []alert.Alert) string {
	if len(active) == 0 {
		return lipgloss.NewStyle().Foreground(ColorGreen).Bold(true).Render(" ● All systems normal")
	}

	var lines []string
	for _, a := range active {
		icon := "▲"
		color := ColorYellow
		if a.Level == alert.LevelCritical {
			icon = "█"
			color = ColorRed
		}

		line := lipgloss.NewStyle().Foreground(color).Bold(true).Render(icon+" "+string(a.Level)) +
			LabelStyle.Render("  ") +
			ValueStyle.Render(a.Metric) +
			LabelStyle.Render(": ") +
			lipgloss.NewStyle().Foreground(color).Render(a.Value) +
			LabelStyle.Render("  ─  ") +
			LabelStyle.Render(a.Message)
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func renderThresholds(w, h int) string {
	warn := lipgloss.NewStyle().Foreground(ColorYellow)
	crit := lipgloss.NewStyle().Foreground(ColorRed)

	lines := []string{
		lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render("Metric        Warn  Crit"),
		lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", w)),
		LabelStyle.Render("CPU %         ") + warn.Render(">50%  ") + crit.Render(">80%"),
		LabelStyle.Render("Memory %      ") + warn.Render(">70%  ") + LabelStyle.Render("─"),
		LabelStyle.Render("DocDB Conns   ") + warn.Render(">80   ") + crit.Render(">100"),
		LabelStyle.Render("RDS Latency   ") + warn.Render(">5ms  ") + crit.Render(">10ms"),
		LabelStyle.Render("RDS DiskQueue ") + warn.Render(">5    ") + crit.Render(">10"),
		LabelStyle.Render("Redis Evict   ") + warn.Render(">0    ") + crit.Render(">100"),
		LabelStyle.Render("Goroutines    ") + warn.Render(">5K   ") + crit.Render(">10K"),
		LabelStyle.Render("5XX errors    ") + warn.Render(">0    ") + crit.Render(">10"),
		LabelStyle.Render("Pod restarts  ") + LabelStyle.Render("─     ") + crit.Render(">0"),
		LabelStyle.Render("Locust fail%  ") + warn.Render(">1%   ") + crit.Render(">5%"),
	}
	return strings.Join(lines, "\n")
}

func renderAlertHistory(w, h int, history []alert.Alert, scrollPos int) string {
	if len(history) == 0 {
		return LabelStyle.Render("No alerts recorded")
	}

	header := fmt.Sprintf("%-8s %-8s %-20s %-10s %s", "TIME", "LEVEL", "METRIC", "VALUE", "MESSAGE")
	headerStyled := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render(header)

	sep := lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", w))

	var lines []string
	lines = append(lines, headerStyled)
	lines = append(lines, sep)

	startIdx := scrollPos
	if startIdx >= len(history) {
		startIdx = len(history) - 1
	}
	if startIdx < 0 {
		startIdx = 0
	}

	maxRows := h - 2
	if maxRows < 1 {
		maxRows = 1
	}

	endIdx := startIdx + maxRows
	if endIdx > len(history) {
		endIdx = len(history)
	}

	for _, a := range history[startIdx:endIdx] {
		timeStr := a.Time.Format("15:04:05")

		color := ColorYellow
		if a.Level == alert.LevelCritical {
			color = ColorRed
		}

		row := fmt.Sprintf("%-8s %s %-20s %s %s",
			LabelStyle.Render(timeStr),
			lipgloss.NewStyle().Foreground(color).Bold(true).Render(PadRight(string(a.Level), 8)),
			Truncate(a.Metric, 20),
			lipgloss.NewStyle().Foreground(color).Render(PadRight(a.Value, 10)),
			LabelStyle.Render(Truncate(a.Message, w-52)),
		)
		lines = append(lines, row)
	}

	if endIdx < len(history) {
		lines = append(lines, LabelStyle.Render(fmt.Sprintf(" ... %d more (scroll with j/k)", len(history)-endIdx)))
	}

	return strings.Join(lines, "\n")
}
