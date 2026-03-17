package view

import (
	"fmt"
	"strings"

	"im-tui/collector"

	"github.com/charmbracelet/lipgloss"
)

// RenderInfrastructure renders Tab 3: CloudWatch infrastructure metrics.
func RenderInfrastructure(width, height int, cw *collector.CloudWatchSnapshot, tsDocDBCPU, tsRdsCPU, tsAlbRT *collector.TimeSeries, scrollPos int) string {
	if width < 20 {
		return "Terminal too narrow"
	}

	if cw == nil {
		return renderCentered(width, height-2, "Waiting for CloudWatch data...")
	}
	if cw.Err != nil {
		return renderCentered(width, height-2, lipgloss.NewStyle().Foreground(ColorRed).Render("CloudWatch error: "+cw.Err.Error()))
	}

	halfW := (width - 1) / 2
	topH := (height - 2) / 2
	botH := height - 2 - topH

	// Top-left: DocumentDB
	docdbContent := renderDocDB(halfW-2, topH-2, cw.DocDB, tsDocDBCPU)
	docdbPanel := Panel("DocumentDB", docdbContent, halfW, topH)

	// Top-right: RDS MySQL
	rdsContent := renderRDS(halfW-2, topH-2, cw.RDS, tsRdsCPU)
	rdsPanel := Panel("RDS MySQL", rdsContent, halfW, topH)

	// Bottom-left: ElastiCache Redis
	redisContent := renderRedis(halfW-2, botH-2, cw.Redis)
	redisPanel := Panel("ElastiCache Redis", redisContent, halfW, botH)

	// Bottom-right: ALB
	albContent := renderALB(halfW-2, botH-2, cw.ALB, tsAlbRT)
	albPanel := Panel("ALB", albContent, halfW, botH)

	topRow := lipgloss.JoinHorizontal(lipgloss.Top, docdbPanel, rdsPanel)
	botRow := lipgloss.JoinHorizontal(lipgloss.Top, redisPanel, albPanel)

	return lipgloss.JoinVertical(lipgloss.Left, topRow, botRow)
}

func renderDocDB(w, h int, d collector.DocDBMetrics, ts *collector.TimeSeries) string {
	barW := w - 20
	if barW < 8 {
		barW = 8
	}
	sparkW := w - 4
	if sparkW < 10 {
		sparkW = 10
	}

	connColor := ColorGreen
	if d.Connections >= 100 {
		connColor = ColorRed
	} else if d.Connections >= 80 {
		connColor = ColorYellow
	}

	lines := []string{
		ProgressBarLabeled("CPU   ", d.CPUPercent, barW, w),
		"",
		LabelStyle.Render("Connections: ") + lipgloss.NewStyle().Foreground(connColor).Bold(true).Render(fmt.Sprintf("%.0f", d.Connections)),
		LabelStyle.Render("Volume:      ") + ValueStyle.Render(FormatBytes(d.VolumeUsed)),
		LabelStyle.Render("Cursors T/O: ") + cursorValue(d.CursorsTimedOut),
		"",
		LabelStyle.Render("Ops/min: ") +
			lipgloss.NewStyle().Foreground(ColorCyan).Render(fmt.Sprintf("I:%.0f ", d.InsertOps)) +
			lipgloss.NewStyle().Foreground(ColorGreen).Render(fmt.Sprintf("Q:%.0f ", d.QueryOps)) +
			lipgloss.NewStyle().Foreground(ColorYellow).Render(fmt.Sprintf("U:%.0f ", d.UpdateOps)) +
			lipgloss.NewStyle().Foreground(ColorRed).Render(fmt.Sprintf("D:%.0f", d.DeleteOps)),
		LabelStyle.Render("IOPS: ") +
			lipgloss.NewStyle().Foreground(ColorCyan).Render(fmt.Sprintf("R:%.0f ", d.ReadIOPS)) +
			lipgloss.NewStyle().Foreground(ColorGreen).Render(fmt.Sprintf("W:%.0f ", d.WriteIOPS)) +
			LabelStyle.Render("Total: ") + ValueStyle.Render(fmt.Sprintf("%.0f", d.ReadIOPS+d.WriteIOPS)),
	}

	if ts != nil && ts.Len() > 0 {
		lines = append(lines, "", LabelStyle.Render("CPU trend: ")+SparklineStr(ts.Values(), sparkW))
	}

	return strings.Join(lines, "\n")
}

func renderRDS(w, h int, r collector.RDSMetrics, ts *collector.TimeSeries) string {
	barW := w - 20
	if barW < 8 {
		barW = 8
	}
	sparkW := w - 4
	if sparkW < 10 {
		sparkW = 10
	}

	lines := []string{
		ProgressBarLabeled("CPU   ", r.CPUPercent, barW, w),
		"",
		LabelStyle.Render("Connections: ") + ValueStyle.Render(fmt.Sprintf("%.0f", r.Connections)),
		LabelStyle.Render("Free Memory: ") + ValueStyle.Render(FormatBytes(r.FreeMemory)),
		"",
		LabelStyle.Render("Read Latency:  ") + formatLatency(r.ReadLatency),
		LabelStyle.Render("Write Latency: ") + formatLatency(r.WriteLatency),
		LabelStyle.Render("Disk Queue:    ") + ValueStyle.Render(fmt.Sprintf("%.1f", r.DiskQueue)),
		"",
		LabelStyle.Render("IOPS: ") +
			lipgloss.NewStyle().Foreground(ColorCyan).Render(fmt.Sprintf("R:%.0f ", r.ReadIOPS)) +
			lipgloss.NewStyle().Foreground(ColorGreen).Render(fmt.Sprintf("W:%.0f ", r.WriteIOPS)) +
			LabelStyle.Render("Total: ") + ValueStyle.Render(fmt.Sprintf("%.0f", r.ReadIOPS+r.WriteIOPS)),
	}

	if ts != nil && ts.Len() > 0 {
		lines = append(lines, "", LabelStyle.Render("CPU trend: ")+SparklineStr(ts.Values(), sparkW))
	}

	return strings.Join(lines, "\n")
}

func renderRedis(w, h int, nodes []collector.RedisNodeMetrics) string {
	if len(nodes) == 0 {
		return LabelStyle.Render("No Redis nodes")
	}

	barW := w - 24
	if barW < 6 {
		barW = 6
	}

	var lines []string
	for i, n := range nodes {
		shortName := n.NodeID
		if len(shortName) > 20 {
			shortName = "..." + shortName[len(shortName)-17:]
		}
		lines = append(lines,
			lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render(fmt.Sprintf("Node %d ", i+1))+
				LabelStyle.Render(shortName),
		)
		lines = append(lines, ProgressBarLabeled("  CPU", n.CPUPercent, barW, w))
		lines = append(lines, ProgressBarLabeled("  Mem", n.MemoryPercent, barW, w))

		hitColor := ColorGreen
		if n.HitRate < 90 {
			hitColor = ColorYellow
		}
		if n.HitRate < 50 {
			hitColor = ColorRed
		}

		lines = append(lines,
			LabelStyle.Render("  Hit Rate: ")+lipgloss.NewStyle().Foreground(hitColor).Render(fmt.Sprintf("%.1f%%", n.HitRate))+
				LabelStyle.Render("  Evict: ")+ValueStyle.Render(fmt.Sprintf("%.0f", n.Evictions))+
				LabelStyle.Render("  Conn: ")+ValueStyle.Render(fmt.Sprintf("%.0f", n.Connections)),
		)
		lines = append(lines,
			LabelStyle.Render("  Cmds: ")+
				lipgloss.NewStyle().Foreground(ColorCyan).Render(fmt.Sprintf("GET:%.0f ", n.GetTypeCmds))+
				lipgloss.NewStyle().Foreground(ColorGreen).Render(fmt.Sprintf("SET:%.0f", n.SetTypeCmds)),
		)

		if i < len(nodes)-1 {
			lines = append(lines, "")
		}
	}

	return strings.Join(lines, "\n")
}

func renderALB(w, h int, a collector.ALBMetrics, ts *collector.TimeSeries) string {
	sparkW := w - 4
	if sparkW < 10 {
		sparkW = 10
	}

	p99Ms := a.ResponseTimeP99 * 1000

	rtColor := ColorGreen
	if p99Ms > 500 {
		rtColor = ColorYellow
	}
	if p99Ms > 2000 {
		rtColor = ColorRed
	}

	errColor := ColorGreen
	if a.Count5XX > 0 {
		errColor = ColorYellow
	}
	if a.Count5XX >= 10 {
		errColor = ColorRed
	}

	lines := []string{
		LabelStyle.Render("Response P99: ") + lipgloss.NewStyle().Foreground(rtColor).Bold(true).Render(fmt.Sprintf("%.0f ms", p99Ms)),
		LabelStyle.Render("5XX Errors:   ") + lipgloss.NewStyle().Foreground(errColor).Bold(true).Render(fmt.Sprintf("%.0f", a.Count5XX)),
		LabelStyle.Render("Active Conns: ") + ValueStyle.Render(fmt.Sprintf("%.0f", a.ActiveConns)),
		LabelStyle.Render("Request Rate: ") + ValueStyle.Render(fmt.Sprintf("%.0f req/min", a.RequestCount)),
	}

	if ts != nil && ts.Len() > 0 {
		lines = append(lines, "", LabelStyle.Render("P99 trend (ms): ")+SparklineStr(ts.Values(), sparkW))
	}

	return strings.Join(lines, "\n")
}

func cursorValue(count float64) string {
	s := fmt.Sprintf("%.0f", count)
	if count > 0 {
		return lipgloss.NewStyle().Foreground(ColorRed).Bold(true).Render(s)
	}
	return lipgloss.NewStyle().Foreground(ColorGreen).Bold(true).Render(s)
}

func formatLatency(seconds float64) string {
	ms := seconds * 1000
	color := ColorGreen
	if ms > 10 {
		color = ColorYellow
	}
	if ms > 50 {
		color = ColorRed
	}
	return lipgloss.NewStyle().Foreground(color).Bold(true).Render(fmt.Sprintf("%.2f ms", ms))
}

func renderCentered(w, h int, msg string) string {
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, msg)
}
