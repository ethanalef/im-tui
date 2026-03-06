package view

import (
	"fmt"
	"strings"

	"im-tui/collector"

	"github.com/charmbracelet/lipgloss"
)

// RenderLocust renders Tab 5: Locust load test metrics.
func RenderLocust(width, height int, locust *collector.LocustSnapshot, tsRPS, tsFail *collector.TimeSeries, scrollPos int) string {
	if width < 20 {
		return "Terminal too narrow"
	}

	if locust == nil || !locust.Available {
		msg := lipgloss.NewStyle().Foreground(ColorSubtext).Render("No load test running") + "\n\n" +
			LabelStyle.Render("Locust is not reachable. Start a load test to see metrics here.")
		return renderCentered(width, height-2, msg)
	}
	if locust.Err != nil {
		return renderCentered(width, height-2, lipgloss.NewStyle().Foreground(ColorRed).Render("Locust error: "+locust.Err.Error()))
	}

	// Top: summary + sparklines
	summaryH := 10
	tableH := height - 2 - summaryH

	summaryContent := renderLocustSummary(width-2, summaryH-2, locust, tsRPS, tsFail)
	summaryPanel := Panel("Locust Load Test", summaryContent, width, summaryH)

	// Bottom: endpoint table + failures
	halfW := (width - 1) / 2

	epContent := renderEndpointTable(halfW-2, tableH-2, locust.Endpoints, scrollPos)
	epPanel := Panel("Endpoints", epContent, halfW, tableH)

	failContent := renderFailureTable(halfW-2, tableH-2, locust.Failures)
	failPanel := Panel("Failures", failContent, halfW, tableH)

	bottomRow := lipgloss.JoinHorizontal(lipgloss.Top, epPanel, failPanel)

	return lipgloss.JoinVertical(lipgloss.Left, summaryPanel, bottomRow)
}

func renderLocustSummary(w, h int, l *collector.LocustSnapshot, tsRPS, tsFail *collector.TimeSeries) string {
	sparkW := (w - 20) / 2
	if sparkW < 10 {
		sparkW = 10
	}

	stateColor := ColorGreen
	switch l.State {
	case "running":
		stateColor = ColorGreen
	case "stopped":
		stateColor = ColorSubtext
	case "spawning":
		stateColor = ColorYellow
	}

	failColor := ColorGreen
	failPct := l.FailRatio * 100
	if failPct > 1 {
		failColor = ColorYellow
	}
	if failPct > 5 {
		failColor = ColorRed
	}

	lines := []string{
		LabelStyle.Render("State: ") + lipgloss.NewStyle().Foreground(stateColor).Bold(true).Render(l.State) +
			LabelStyle.Render("    Users: ") + ValueStyle.Render(fmt.Sprintf("%d", l.UserCount)),
		"",
		LabelStyle.Render("Total RPS:  ") + ValueStyle.Render(fmt.Sprintf("%.1f", l.TotalRPS)) +
			"  " + SparklineStr(tsRPS.Values(), sparkW),
		LabelStyle.Render("Fail Rate:  ") + lipgloss.NewStyle().Foreground(failColor).Bold(true).Render(fmt.Sprintf("%.2f%%", failPct)) +
			"  " + SparklineStr(tsFail.Values(), sparkW),
	}

	return strings.Join(lines, "\n")
}

func renderEndpointTable(w, h int, endpoints []collector.LocustEndpoint, scrollPos int) string {
	if len(endpoints) == 0 {
		return LabelStyle.Render("No endpoint data")
	}

	nameW := w - 65
	if nameW < 10 {
		nameW = 10
	}

	header := fmt.Sprintf("%-6s %-*s %7s %6s %7s %7s %7s %7s",
		"METHOD", nameW, "NAME", "RPS", "FAIL%", "AVG", "P50", "P95", "P99")
	headerStyled := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render(header)

	sep := lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", w))

	var lines []string
	lines = append(lines, headerStyled)
	lines = append(lines, sep)

	startIdx := scrollPos
	if startIdx >= len(endpoints) {
		startIdx = len(endpoints) - 1
	}
	if startIdx < 0 {
		startIdx = 0
	}

	maxRows := h - 2
	if maxRows < 1 {
		maxRows = 1
	}

	endIdx := startIdx + maxRows
	if endIdx > len(endpoints) {
		endIdx = len(endpoints)
	}

	for _, ep := range endpoints[startIdx:endIdx] {
		name := Truncate(ep.Name, nameW)

		failColor := ColorGreen
		if ep.FailPercent > 1 {
			failColor = ColorYellow
		}
		if ep.FailPercent > 5 {
			failColor = ColorRed
		}

		p99Color := ColorGreen
		if ep.P99 > 500 {
			p99Color = ColorYellow
		}
		if ep.P99 > 2000 {
			p99Color = ColorRed
		}

		row := fmt.Sprintf("%-6s %-*s %7.1f %s %7.0f %7.0f %s %s",
			lipgloss.NewStyle().Foreground(ColorCyan).Render(ep.Method),
			nameW, name,
			ep.RPS,
			lipgloss.NewStyle().Foreground(failColor).Render(fmt.Sprintf("%5.1f%%", ep.FailPercent)),
			ep.AvgResponseTime,
			ep.P50,
			lipgloss.NewStyle().Foreground(p99Color).Render(fmt.Sprintf("%7.0f", ep.P95)),
			lipgloss.NewStyle().Foreground(p99Color).Render(fmt.Sprintf("%7.0f", ep.P99)),
		)
		lines = append(lines, row)
	}

	return strings.Join(lines, "\n")
}

func renderFailureTable(w, h int, failures []collector.LocustFailure) string {
	if len(failures) == 0 {
		return lipgloss.NewStyle().Foreground(ColorGreen).Render("No failures ✓")
	}

	var lines []string
	header := fmt.Sprintf("%-6s %-20s %-6s %s", "METHOD", "NAME", "COUNT", "ERROR")
	lines = append(lines, lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render(header))
	lines = append(lines, lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", w)))

	maxRows := h - 2
	shown := failures
	if len(shown) > maxRows && maxRows > 0 {
		shown = shown[:maxRows]
	}

	for _, f := range shown {
		errMsg := Truncate(f.Error, w-36)
		row := fmt.Sprintf("%-6s %-20s %s %s",
			lipgloss.NewStyle().Foreground(ColorCyan).Render(f.Method),
			Truncate(f.Name, 20),
			lipgloss.NewStyle().Foreground(ColorRed).Bold(true).Render(fmt.Sprintf("%-6d", f.Occurrences)),
			LabelStyle.Render(errMsg),
		)
		lines = append(lines, row)
	}

	return strings.Join(lines, "\n")
}
