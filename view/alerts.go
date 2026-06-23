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

	threshContent := renderThresholds(threshW-2, historyH-2, ev.Thresholds())
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

func renderThresholds(w, h int, t alert.Thresholds) string {
	warn := lipgloss.NewStyle().Foreground(ColorYellow)
	crit := lipgloss.NewStyle().Foreground(ColorRed)

	lines := []string{
		lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render("Metric        Warn  Crit"),
		lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", w)),
		thresholdRow("CPU %", thresholdGT(t.CPUWarn, "%", false), thresholdGT(t.CPUCrit, "%", false), warn, crit),
		thresholdRow("Memory %", thresholdGT(t.MemoryWarn, "%", false), "─", warn, crit),
		thresholdRow("DocDB Conns", thresholdGT(t.DocDBConnWarn, "", false), thresholdGT(t.DocDBConnCrit, "", false), warn, crit),
		thresholdRow("RDS Latency", thresholdGT(t.RDSLatencyWarnMs, "ms", false), thresholdGT(t.RDSLatencyCritMs, "ms", false), warn, crit),
		thresholdRow("RDS DiskQ", thresholdGT(t.RDSDiskQueueWarn, "", false), thresholdGT(t.RDSDiskQueueCrit, "", false), warn, crit),
		thresholdRow("Redis eCPU", thresholdGT(t.RedisCPUWarn, "%", false), thresholdGT(t.RedisCPUCrit, "%", false), warn, crit),
		thresholdRow("Redis Evict", thresholdGT(t.RedisEvictWarn, "", false), thresholdGT(t.RedisEvictCrit, "", false), warn, crit),
		thresholdRow("Kafka Lag", thresholdGT(t.KafkaLagWarn, "", false), thresholdGT(t.KafkaLagCrit, "", false), warn, crit),
		thresholdRow("Push Fail/s", thresholdGT(t.PushFailWarnPerSec, "", true), thresholdGT(t.PushFailCritPerSec, "", false), warn, crit),
		thresholdRow("Push Slow/s", thresholdGT(t.LongTimePushWarnPerSec, "", true), thresholdGT(t.LongTimePushCritPerSec, "", false), warn, crit),
		thresholdRow("SMS Fail/s", thresholdGT(t.SMSFailWarnPerSec, "", true), thresholdGT(t.SMSFailCritPerSec, "", false), warn, crit),
		thresholdRow("E2E Group", thresholdGT(t.E2EGroupWarnS, "s", false), thresholdGT(t.E2EGroupCritS, "s", false), warn, crit),
		thresholdRow("E2E Single", thresholdGT(t.E2ESingleWarnS, "s", false), thresholdGT(t.E2ESingleCritS, "s", false), warn, crit),
		thresholdRow("5XX errors", thresholdGT(float64(t.Error5XXWarn), "", true), thresholdGT(float64(t.Error5XXCrit), "", false), warn, crit),
		thresholdRow("Pod restarts", "─", thresholdGT(float64(t.PodRestartCrit), "", false), warn, crit),
		thresholdRow("Locust fail%", thresholdGT(t.LocustFailWarn, "%", false), ">5%", warn, crit),
	}
	return strings.Join(lines, "\n")
}

func thresholdRow(label, warnValue, critValue string, warn, crit lipgloss.Style) string {
	return LabelStyle.Render(fmt.Sprintf("%-12s ", label)) +
		warn.Render(fmt.Sprintf("%-6s", warnValue)) +
		crit.Render(critValue)
}

func thresholdGT(v float64, suffix string, zeroMeansAny bool) string {
	if v <= 0 {
		if zeroMeansAny {
			return ">0"
		}
		return "─"
	}
	return ">" + thresholdValue(v, suffix)
}

func thresholdValue(v float64, suffix string) string {
	if suffix == "ms" || suffix == "s" {
		if v == float64(int64(v)) {
			return fmt.Sprintf("%.0f%s", v, suffix)
		}
		return fmt.Sprintf("%.2g%s", v, suffix)
	}
	if suffix != "" {
		return fmt.Sprintf("%.0f%s", v, suffix)
	}
	if v >= 1000 {
		return fmt.Sprintf("%.0fK", v/1000)
	}
	if v == float64(int64(v)) {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.2g", v)
}

func renderAlertHistory(w, h int, history []alert.Alert, scrollPos int) string {
	if len(history) == 0 {
		return LabelStyle.Render("No alerts recorded")
	}

	header := fmt.Sprintf("%-14s %-8s %-20s %-10s %s", "TIME", "LEVEL", "METRIC", "VALUE", "MESSAGE")
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
		// Include the date (MM-DD) so events from earlier days aren't mistaken
		// for today — a restart from days ago must not look like it just happened.
		timeStr := a.Time.Format("01-02 15:04:05")

		color := ColorYellow
		if a.Level == alert.LevelCritical {
			color = ColorRed
		}

		row := fmt.Sprintf("%-14s %s %-20s %s %s",
			LabelStyle.Render(timeStr),
			lipgloss.NewStyle().Foreground(color).Bold(true).Render(PadRight(string(a.Level), 8)),
			Truncate(a.Metric, 20),
			lipgloss.NewStyle().Foreground(color).Render(PadRight(a.Value, 10)),
			LabelStyle.Render(Truncate(a.Message, w-58)),
		)
		lines = append(lines, row)
	}

	if endIdx < len(history) {
		lines = append(lines, LabelStyle.Render(fmt.Sprintf(" ... %d more (scroll with j/k)", len(history)-endIdx)))
	}

	return strings.Join(lines, "\n")
}
