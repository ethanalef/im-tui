package view

import (
	"fmt"
	"strings"

	"im-tui/collector"

	"github.com/charmbracelet/lipgloss"
)

// RenderKubernetes renders Tab 4: Kubernetes pod/HPA/event details.
func RenderKubernetes(width, height int, k8s *collector.KubernetesSnapshot, scrollPos int) string {
	if width < 20 {
		return "Terminal too narrow"
	}

	if k8s == nil {
		return renderCentered(width, height-2, "Waiting for Kubernetes data...")
	}
	if k8s.Err != nil {
		return renderCentered(width, height-2, lipgloss.NewStyle().Foreground(ColorRed).Render("kubectl error: "+k8s.Err.Error()))
	}

	// Layout: pods table (top, ~60%), HPA + events (bottom, ~40%)
	podH := (height - 2) * 6 / 10
	bottomH := height - 2 - podH

	// Pod table
	podContent := renderPodTable(width-2, podH-2, k8s.Pods, scrollPos)
	podPanel := Panel("Pods", podContent, width, podH)

	// Bottom: HPA (left) + Events (right)
	halfW := (width - 1) / 2

	hpaContent := renderHPATable(halfW-2, bottomH-2, k8s.HPAs)
	hpaPanel := Panel("HPA Autoscalers", hpaContent, halfW, bottomH)

	eventContent := renderEventTable(halfW-2, bottomH-2, k8s.Events)
	eventPanel := Panel("Warning Events", eventContent, halfW, bottomH)

	bottomRow := lipgloss.JoinHorizontal(lipgloss.Top, hpaPanel, eventPanel)

	return lipgloss.JoinVertical(lipgloss.Left, podPanel, bottomRow)
}

func renderPodTable(w, h int, pods []collector.PodInfo, scrollPos int) string {
	if len(pods) == 0 {
		return LabelStyle.Render("No pods found")
	}

	// Column widths
	nameW := w - 55
	if nameW < 15 {
		nameW = 15
	}

	header := fmt.Sprintf("%-*s %-8s %-5s %-8s %-5s %-10s %-10s",
		nameW, "NAME", "STATUS", "READY", "RESTARTS", "AGE", "CPU", "MEMORY")
	headerStyled := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render(header)

	sep := lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", w))

	var lines []string
	lines = append(lines, headerStyled)
	lines = append(lines, sep)

	// Summary line
	running, total, totalRestarts := 0, len(pods), 0
	for _, p := range pods {
		if p.Status == "Running" {
			running++
		}
		totalRestarts += p.Restarts
	}

	summaryColor := ColorGreen
	if running < total {
		summaryColor = ColorYellow
	}
	summary := fmt.Sprintf(" %s  Restarts: %s",
		lipgloss.NewStyle().Foreground(summaryColor).Bold(true).Render(fmt.Sprintf("%d/%d Running", running, total)),
		formatRestarts(totalRestarts),
	)
	lines = append(lines, summary)
	lines = append(lines, sep)

	// Apply scroll
	startIdx := scrollPos
	if startIdx >= len(pods) {
		startIdx = len(pods) - 1
	}
	if startIdx < 0 {
		startIdx = 0
	}

	maxRows := h - 4 // header + sep + summary + sep
	if maxRows < 1 {
		maxRows = 1
	}

	endIdx := startIdx + maxRows
	if endIdx > len(pods) {
		endIdx = len(pods)
	}

	for _, p := range pods[startIdx:endIdx] {
		name := Truncate(p.Name, nameW)

		statusColor := ColorGreen
		switch p.Status {
		case "Running":
			statusColor = ColorGreen
		case "Pending":
			statusColor = ColorYellow
		case "CrashLoopBackOff", "Error", "OOMKilled":
			statusColor = ColorRed
		default:
			statusColor = ColorSubtext
		}

		cpu := p.CPUUsage
		if cpu == "" {
			cpu = "─"
		}
		mem := p.MemUsage
		if mem == "" {
			mem = "─"
		}

		row := fmt.Sprintf("%-*s %s %-5s %s %-5s %-10s %-10s",
			nameW, name,
			lipgloss.NewStyle().Foreground(statusColor).Render(PadRight(p.Status, 8)),
			p.Ready,
			formatRestarts(p.Restarts),
			p.Age,
			cpu,
			mem,
		)
		lines = append(lines, row)
	}

	if endIdx < len(pods) {
		remaining := len(pods) - endIdx
		lines = append(lines, LabelStyle.Render(fmt.Sprintf(" ... %d more (scroll with j/k)", remaining)))
	}

	return strings.Join(lines, "\n")
}

func renderHPATable(w, h int, hpas []collector.HPAInfo) string {
	if len(hpas) == 0 {
		return LabelStyle.Render("No HPAs configured")
	}

	var lines []string
	header := fmt.Sprintf("%-20s %-18s %s", "NAME", "TARGETS", "REPLICAS")
	lines = append(lines, lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render(header))
	lines = append(lines, lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", w)))

	for _, h := range hpas {
		name := Truncate(h.Name, 20)
		targets := Truncate(h.Targets, 18)

		replicaColor := ColorGreen
		if h.Current >= h.MaxReplicas {
			replicaColor = ColorRed
		} else if h.Current > h.MinReplicas {
			replicaColor = ColorYellow
		}

		replicas := lipgloss.NewStyle().Foreground(replicaColor).Render(
			fmt.Sprintf("%d/%d/%d", h.MinReplicas, h.Current, h.MaxReplicas))

		row := fmt.Sprintf("%-20s %-18s %s", name, targets, replicas)
		lines = append(lines, row)
	}

	return strings.Join(lines, "\n")
}

func renderEventTable(w, h int, events []collector.EventInfo) string {
	if len(events) == 0 {
		return lipgloss.NewStyle().Foreground(ColorGreen).Render("No warnings ✓")
	}

	var lines []string
	header := fmt.Sprintf("%-5s %-12s %-15s %s", "AGE", "REASON", "OBJECT", "MESSAGE")
	lines = append(lines, lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render(header))
	lines = append(lines, lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", w)))

	maxRows := h - 2
	shown := events
	if len(shown) > maxRows && maxRows > 0 {
		shown = shown[:maxRows]
	}

	for _, e := range shown {
		msg := Truncate(e.Message, w-35)
		obj := Truncate(e.Object, 15)

		row := fmt.Sprintf("%-5s %-12s %-15s %s",
			e.Age,
			lipgloss.NewStyle().Foreground(ColorYellow).Render(PadRight(e.Reason, 12)),
			obj,
			LabelStyle.Render(msg),
		)
		lines = append(lines, row)
	}

	return strings.Join(lines, "\n")
}

func formatRestarts(n int) string {
	s := fmt.Sprintf("%-8d", n)
	if n > 0 {
		return lipgloss.NewStyle().Foreground(ColorRed).Bold(true).Render(s)
	}
	return lipgloss.NewStyle().Foreground(ColorGreen).Render(s)
}
