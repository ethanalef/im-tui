package view

import (
	"fmt"
	"strings"

	"im-tui/collector"

	"github.com/charmbracelet/lipgloss"
)

// RenderLogs renders Tab 7: Error summary (top) + live error tail (bottom).
func RenderLogs(width, height int, snap *collector.LogSnapshot, scrollPos, scrollXPos int) string {
	if width < 20 {
		return "Terminal too narrow"
	}

	if snap == nil {
		return renderCentered(width, height-2, LabelStyle.Render("Log collector initializing..."))
	}

	// Top panel: error summary table (~40% height, min 10 rows)
	summaryH := height * 2 / 5
	if summaryH < 10 {
		summaryH = 10
	}
	if summaryH > height-6 {
		summaryH = height - 6
	}

	tailH := height - 2 - summaryH

	summaryContent := renderLogSummary(width-2, summaryH-2, snap.Services)
	summaryPanel := Panel("Error Summary", summaryContent, width, summaryH)

	sinceLabel := "Error Log (last 60s)"
	if scrollXPos > 0 {
		sinceLabel = fmt.Sprintf("Error Log (last 60s) ← h/l →  offset:%d", scrollXPos)
	}
	tailContent := renderLogTail(width-2, tailH-2, snap.Lines, scrollPos, scrollXPos)
	tailPanel := Panel(sinceLabel, tailContent, width, tailH)

	return lipgloss.JoinVertical(lipgloss.Left, summaryPanel, tailPanel)
}

func renderLogSummary(w, h int, services []collector.ServiceLogSummary) string {
	if len(services) == 0 {
		return LabelStyle.Render("No services configured")
	}

	header := fmt.Sprintf("%-20s %5s %5s %5s %5s %6s",
		"SERVICE", "ERR", "FAIL", "TOUT", "PNC", "TOTAL")
	headerStyled := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render(header)
	sep := lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", w))

	var lines []string
	lines = append(lines, headerStyled)
	lines = append(lines, sep)

	for _, svc := range services {
		nameStyled := ValueStyle.Render(PadRight(svc.Name, 20))

		errStr := formatLogCount(svc.Errors, 5, ColorRed)
		failStr := formatLogCount(svc.Fails, 5, ColorYellow)
		timeoutStr := formatLogCount(svc.Timeouts, 5, ColorCyan)
		panicStr := formatLogCount(svc.Panics, 5, ColorRed)
		totalStr := LabelStyle.Render(fmt.Sprintf("%6d", svc.Total))

		row := fmt.Sprintf("%s %s %s %s %s %s",
			nameStyled, errStr, failStr, timeoutStr, panicStr, totalStr)
		lines = append(lines, row)
	}

	// Total row
	var totalErrors, totalFails, totalTimeouts, totalPanics, totalLines int
	for _, svc := range services {
		totalErrors += svc.Errors
		totalFails += svc.Fails
		totalTimeouts += svc.Timeouts
		totalPanics += svc.Panics
		totalLines += svc.Total
	}

	lines = append(lines, lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", w)))
	totalLabel := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render(PadRight("TOTAL", 20))
	lines = append(lines, fmt.Sprintf("%s %s %s %s %s %s",
		totalLabel,
		formatLogCount(totalErrors, 5, ColorRed),
		formatLogCount(totalFails, 5, ColorYellow),
		formatLogCount(totalTimeouts, 5, ColorCyan),
		formatLogCount(totalPanics, 5, ColorRed),
		LabelStyle.Render(fmt.Sprintf("%6d", totalLines)),
	))

	return strings.Join(lines, "\n")
}

func renderLogTail(w, h int, logLines []collector.LogLine, scrollPos, scrollXPos int) string {
	if len(logLines) == 0 {
		return lipgloss.NewStyle().Foreground(ColorGreen).Bold(true).Render(" ● No errors detected")
	}

	// Compact columns: TIME(9) SVC(8) LVL(8) MSG(rest)
	header := fmt.Sprintf("%-9s %-8s %-8s %s", "TIME", "SVC", "LEVEL", "MESSAGE")
	headerStyled := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render(header)
	sep := lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", w))

	var lines []string
	lines = append(lines, headerStyled)
	lines = append(lines, sep)

	startIdx := scrollPos
	if startIdx >= len(logLines) {
		startIdx = len(logLines) - 1
	}
	if startIdx < 0 {
		startIdx = 0
	}

	maxRows := h - 2
	if maxRows < 1 {
		maxRows = 1
	}

	endIdx := startIdx + maxRows
	if endIdx > len(logLines) {
		endIdx = len(logLines)
	}

	// Message gets remaining width after TIME(9)+SVC(8)+LVL(8)+spaces(3) = 28
	msgW := w - 28
	if msgW < 20 {
		msgW = 20
	}

	for _, ll := range logLines[startIdx:endIdx] {
		timeStr := LabelStyle.Render(PadRight(ll.Time, 9))
		svcStr := ValueStyle.Render(PadRight(shortSvc(ll.Service), 8))
		levelStr := levelStyled(ll.Level)

		// Apply horizontal scroll to message
		msg := ll.Message
		runes := []rune(msg)
		if scrollXPos > 0 && scrollXPos < len(runes) {
			msg = "…" + string(runes[scrollXPos:])
		} else if scrollXPos >= len(runes) {
			msg = ""
		}
		msgStyled := LabelStyle.Render(Truncate(msg, msgW))

		row := fmt.Sprintf("%s %s %s %s", timeStr, svcStr, levelStr, msgStyled)
		lines = append(lines, row)
	}

	remaining := len(logLines) - endIdx
	if remaining > 0 {
		lines = append(lines, LabelStyle.Render(
			fmt.Sprintf(" ... %d more (j/k:vert  h/l:horiz)", remaining)))
	}

	return strings.Join(lines, "\n")
}

// shortSvc abbreviates service names for compact display.
func shortSvc(name string) string {
	switch name {
	case "msg-gateway":
		return "gateway"
	case "msg-transfer":
		return "transfer"
	case "openim-push":
		return "push"
	case "openim-auth":
		return "auth"
	case "openim-conversation":
		return "convo"
	case "openim-msg":
		return "msg"
	case "chat-api":
		return "chat"
	default:
		return Truncate(name, 8)
	}
}

func levelStyled(level string) string {
	padded := PadRight(level, 8)
	switch level {
	case "PANIC":
		return lipgloss.NewStyle().Foreground(ColorRed).Bold(true).Render(padded)
	case "ERROR":
		return lipgloss.NewStyle().Foreground(ColorRed).Render(padded)
	case "TIMEOUT":
		return lipgloss.NewStyle().Foreground(ColorCyan).Render(padded)
	case "FAIL":
		return lipgloss.NewStyle().Foreground(ColorYellow).Render(padded)
	default:
		return LabelStyle.Render(padded)
	}
}

func formatLogCount(n int, width int, color lipgloss.Color) string {
	s := fmt.Sprintf("%*d", width, n)
	if n > 0 {
		return lipgloss.NewStyle().Foreground(color).Bold(true).Render(s)
	}
	return lipgloss.NewStyle().Foreground(ColorSubtext).Render(s)
}

