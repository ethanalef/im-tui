package view

import (
	"fmt"
	"strings"

	"im-tui/collector"

	"github.com/charmbracelet/lipgloss"
)

// RenderInfrastructure renders Tab 3: CloudWatch infrastructure metrics.
func RenderInfrastructure(width, height int, cw *collector.CloudWatchSnapshot, specs collector.InfraSpecs, tsDocDBCPU, tsRdsCPU, tsAlbRT *collector.TimeSeries, redisCPUWarn, redisCPUCrit, redisEvictWarn, redisEvictCrit float64, scrollPos int) string {
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
	docdbContent := renderDocDB(halfW-2, topH-2, cw.DocDB, cw.DocDBShards, specs.DocDB, tsDocDBCPU, scrollPos)
	docdbPanel := Panel("DocumentDB", docdbContent, halfW, topH)

	// Top-right: RDS MySQL
	rdsContent := renderRDS(halfW-2, topH-2, cw.RDS, specs.RDS, tsRdsCPU)
	rdsPanel := Panel("RDS MySQL", rdsContent, halfW, topH)

	// Bottom-left: ElastiCache Redis (per-replica table)
	redisContent := renderRedis(halfW-2, botH-2, cw.Redis, specs.Redis, redisCPUWarn, redisCPUCrit, redisEvictWarn, redisEvictCrit)
	redisPanel := Panel(fmt.Sprintf("ElastiCache Redis (%d nodes)", len(cw.Redis)), redisContent, halfW, botH)

	// Bottom-right: ALB
	albContent := renderALB(halfW-2, botH-2, cw.ALB, tsAlbRT)
	albPanel := Panel("ALB", albContent, halfW, botH)

	topRow := lipgloss.JoinHorizontal(lipgloss.Top, docdbPanel, rdsPanel)
	botRow := lipgloss.JoinHorizontal(lipgloss.Top, redisPanel, albPanel)

	return lipgloss.JoinVertical(lipgloss.Left, topRow, botRow)
}

func renderDocDB(w, h int, d collector.DocDBMetrics, shards []collector.DocDBMetrics, spec collector.DocDBSpec, ts *collector.TimeSeries, scrollPos int) string {
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

	var lines []string
	if specLine := collector.FormatDocDBSpec(spec); specLine != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(ColorSubtext).Render(specLine))
	}
	if len(shards) > 0 {
		lines = append(lines,
			ProgressBarLabeled("CPU   ", d.CPUPercent, barW, w),
			LabelStyle.Render("Conn: ")+lipgloss.NewStyle().Foreground(connColor).Bold(true).Render(fmt.Sprintf("%.0f", d.Connections))+
				LabelStyle.Render("  Vol: ")+ValueStyle.Render(FormatBytes(d.VolumeUsed))+
				LabelStyle.Render("  Cur: ")+cursorValue(d.CursorsTimedOut),
			renderDocDBOpsLine(d),
			renderDocDBIOPSLine(d),
		)
	} else {
		lines = append(lines,
			ProgressBarLabeled("CPU   ", d.CPUPercent, barW, w),
			"",
			LabelStyle.Render("Connections: ")+lipgloss.NewStyle().Foreground(connColor).Bold(true).Render(fmt.Sprintf("%.0f", d.Connections)),
			LabelStyle.Render("Volume:      ")+ValueStyle.Render(FormatBytes(d.VolumeUsed)),
			LabelStyle.Render("Cursors T/O: ")+cursorValue(d.CursorsTimedOut),
			"",
			renderDocDBOpsLine(d),
			renderDocDBIOPSLine(d),
		)
	}

	if len(shards) > 0 {
		lines = append(lines, "")
		lines = append(lines, renderDocDBShardTable(w, h-len(lines), shards, scrollPos)...)
	}

	if ts != nil && ts.Len() > 0 && len(lines)+2 <= h {
		lines = append(lines, "", LabelStyle.Render("CPU trend: ")+SparklineStr(ts.Values(), sparkW))
	}

	return strings.Join(lines, "\n")
}

func renderDocDBOpsLine(d collector.DocDBMetrics) string {
	return LabelStyle.Render("Ops/min: ") +
		lipgloss.NewStyle().Foreground(ColorCyan).Render(fmt.Sprintf("I:%.0f ", d.InsertOps)) +
		lipgloss.NewStyle().Foreground(ColorGreen).Render(fmt.Sprintf("Q:%.0f ", d.QueryOps)) +
		lipgloss.NewStyle().Foreground(ColorYellow).Render(fmt.Sprintf("U:%.0f ", d.UpdateOps)) +
		lipgloss.NewStyle().Foreground(ColorRed).Render(fmt.Sprintf("D:%.0f", d.DeleteOps))
}

func renderDocDBIOPSLine(d collector.DocDBMetrics) string {
	if !d.HasReadIOPS && !d.HasWriteIOPS {
		return LabelStyle.Render("IOPS: ") +
			lipgloss.NewStyle().Foreground(ColorCyan).Render("R:-- ") +
			lipgloss.NewStyle().Foreground(ColorGreen).Render("W:-- ") +
			LabelStyle.Render("Total: ") + ValueStyle.Render("--")
	}
	return LabelStyle.Render("IOPS: ") +
		lipgloss.NewStyle().Foreground(ColorCyan).Render(fmt.Sprintf("R:%.0f ", d.ReadIOPS)) +
		lipgloss.NewStyle().Foreground(ColorGreen).Render(fmt.Sprintf("W:%.0f ", d.WriteIOPS)) +
		LabelStyle.Render("Total: ") + ValueStyle.Render(fmt.Sprintf("%.0f", d.ReadIOPS+d.WriteIOPS))
}

func renderDocDBShardTable(w, h int, shards []collector.DocDBMetrics, scrollPos int) []string {
	if h <= 0 {
		return nil
	}

	rows := docDBShardInstanceRows(shards)
	visibleRows := h - 1 // header
	if len(rows) > visibleRows {
		visibleRows-- // scroll indicator
	}
	if visibleRows < 1 {
		visibleRows = 1
	}
	if visibleRows > len(rows) {
		visibleRows = len(rows)
	}

	maxScroll := len(rows) - visibleRows
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scrollPos > maxScroll {
		scrollPos = maxScroll
	}
	if scrollPos < 0 {
		scrollPos = 0
	}

	endIdx := scrollPos + visibleRows
	if endIdx > len(rows) {
		endIdx = len(rows)
	}

	var lines []string
	header := fmt.Sprintf("%-8s %-6s %6s %8s %7s", "shard", "role", "cpu", "free", "lag")
	lines = append(lines, lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render(header))
	lines = append(lines, rows[scrollPos:endIdx]...)
	if len(rows) > visibleRows {
		lines = append(lines, LabelStyle.Render(fmt.Sprintf("[%d-%d of %d]", scrollPos+1, endIdx, len(rows))))
	}

	return lines
}

func docDBShardInstanceRows(shards []collector.DocDBMetrics) []string {
	var rows []string
	for _, s := range shards {
		shardID := shortShardID(s.ShardID)
		rows = append(rows,
			fmt.Sprintf("%-8s %-6s %s %s %s",
				shardID,
				"PRI",
				docDBCPUCell(s.CPUPercent),
				docDBMemoryCell(s.PrimaryFreeMem, s.HasPrimaryFree),
				fmt.Sprintf("%7s", "--"),
			),
			fmt.Sprintf("%-8s %-6s %s %s %s",
				shardID,
				"REPavg",
				docDBMaybeCPUCell(s.ReplicaCPU, s.HasReplicaCPU),
				docDBMemoryCell(s.ReplicaFreeMem, s.HasReplicaFree),
				docDBReplicaLagCell(s),
			),
		)
	}
	return rows
}

func shortShardID(shardID string) string {
	if shardID == "" {
		return "-"
	}
	if len(shardID) > 8 {
		return shardID[:8]
	}
	return shardID
}

func docDBDocsCell(d collector.DocDBMetrics) string {
	if !d.HasDocumentOps {
		return fmt.Sprintf("%8s", "--")
	}
	return fmt.Sprintf("%8s", FormatNum(d.InsertOps+d.QueryOps+d.UpdateOps+d.DeleteOps))
}

func docDBIOPSCell(d collector.DocDBMetrics) string {
	if !d.HasReadIOPS && !d.HasWriteIOPS {
		return fmt.Sprintf("%7s", "--")
	}
	return fmt.Sprintf("%7s", FormatNum(d.ReadIOPS+d.WriteIOPS))
}

func docDBReplicaLagCell(d collector.DocDBMetrics) string {
	if !d.HasReplicaLag {
		return fmt.Sprintf("%7s", "--")
	}
	if d.ReplicaLagMS >= 1000 {
		return fmt.Sprintf("%7s", fmt.Sprintf("%.1fs", d.ReplicaLagMS/1000))
	}
	return fmt.Sprintf("%7s", fmt.Sprintf("%.0fms", d.ReplicaLagMS))
}

func docDBMemoryCell(bytes float64, ok bool) string {
	if !ok {
		return fmt.Sprintf("%8s", "--")
	}
	return fmt.Sprintf("%8s", FormatBytes(bytes))
}

func docDBMaybeCPUCell(cpu float64, ok bool) string {
	if !ok {
		return fmt.Sprintf("%6s", "--")
	}
	return docDBCPUCell(cpu)
}

func docDBCPUCell(cpu float64) string {
	color := ColorGreen
	if cpu >= 80 {
		color = ColorRed
	} else if cpu >= 60 {
		color = ColorYellow
	}
	return lipgloss.NewStyle().Foreground(color).Bold(true).Render(fmt.Sprintf("%5.1f%%", cpu))
}

func renderRDS(w, h int, r collector.RDSMetrics, spec collector.RDSSpec, ts *collector.TimeSeries) string {
	barW := w - 20
	if barW < 8 {
		barW = 8
	}
	sparkW := w - 4
	if sparkW < 10 {
		sparkW = 10
	}

	var lines []string
	if specLine := collector.FormatRDSSpec(spec); specLine != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(ColorSubtext).Render(specLine))
	}
	lines = append(lines,
		ProgressBarLabeled("CPU   ", r.CPUPercent, barW, w),
		"",
		LabelStyle.Render("Connections: ")+ValueStyle.Render(fmt.Sprintf("%.0f", r.Connections)),
		LabelStyle.Render("Free Memory: ")+ValueStyle.Render(FormatBytes(r.FreeMemory)),
		"",
		LabelStyle.Render("Read Latency:  ")+formatLatency(r.ReadLatency),
		LabelStyle.Render("Write Latency: ")+formatLatency(r.WriteLatency),
		LabelStyle.Render("Disk Queue:    ")+ValueStyle.Render(fmt.Sprintf("%.1f", r.DiskQueue)),
		"",
		LabelStyle.Render("IOPS: ")+
			lipgloss.NewStyle().Foreground(ColorCyan).Render(fmt.Sprintf("R:%.0f ", r.ReadIOPS))+
			lipgloss.NewStyle().Foreground(ColorGreen).Render(fmt.Sprintf("W:%.0f ", r.WriteIOPS))+
			LabelStyle.Render("Total: ")+ValueStyle.Render(fmt.Sprintf("%.0f", r.ReadIOPS+r.WriteIOPS)),
	)

	if ts != nil && ts.Len() > 0 {
		lines = append(lines, "", LabelStyle.Render("CPU trend: ")+SparklineStr(ts.Values(), sparkW))
	}

	return strings.Join(lines, "\n")
}

// renderRedis renders a per-replica table of ElastiCache nodes. EngineCPU is colored
// by the redis_cpu_warn/crit thresholds, evictions by redis_evict_warn/crit, and the
// hottest replica (highest EngineCPU among replicas) is highlighted.
func renderRedis(w, h int, nodes []collector.RedisNodeMetrics, nodeSpecs []collector.RedisNodeSpec, cpuWarn, cpuCrit, evictWarn, evictCrit float64) string {
	if len(nodes) == 0 {
		return LabelStyle.Render("No Redis nodes")
	}

	// Identify the hottest replica by EngineCPU (primary excluded from the contest).
	hottestIdx := -1
	var hottestCPU float64
	for i, n := range nodes {
		if n.Role == collector.RedisRolePrimary {
			continue
		}
		if hottestIdx == -1 || n.EngineCPU > hottestCPU {
			hottestIdx = i
			hottestCPU = n.EngineCPU
		}
	}

	var lines []string
	if len(nodeSpecs) > 0 {
		if specLine := collector.FormatRedisSpec(nodeSpecs[0]); specLine != "" {
			lines = append(lines, lipgloss.NewStyle().Foreground(ColorSubtext).Render(specLine))
		}
	}

	// Table header: node | role | eCPU% | lag | conns | GETs | evict
	header := fmt.Sprintf("%-14s %-4s %6s %7s %6s %7s %6s", "node", "role", "eCPU", "lag", "conn", "GET", "evict")
	lines = append(lines, lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render(header))

	for i, n := range nodes {
		shortName := n.NodeID
		// Show the trailing node suffix (e.g. "...-003") which is what differs.
		if len(shortName) > 14 {
			shortName = "…" + shortName[len(shortName)-13:]
		}

		role := "rep"
		if n.Role == collector.RedisRolePrimary {
			role = "PRI"
		}

		nameCell := fmt.Sprintf("%-14s", shortName)
		if i == hottestIdx {
			// Highlight the hottest replica row name.
			nameCell = lipgloss.NewStyle().Foreground(ColorPurple).Bold(true).Render(nameCell)
		} else {
			nameCell = LabelStyle.Render(nameCell)
		}

		roleCell := lipgloss.NewStyle().Foreground(ColorSubtext).Render(fmt.Sprintf("%-4s", role))
		if n.Role == collector.RedisRolePrimary {
			roleCell = lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render(fmt.Sprintf("%-4s", role))
		}

		cpuCell := lipgloss.NewStyle().Foreground(cpuColor(n.EngineCPU, cpuWarn, cpuCrit)).Bold(true).
			Render(fmt.Sprintf("%5.1f%%", n.EngineCPU))
		lagCell := redisLagCell(n)
		connCell := ValueStyle.Render(fmt.Sprintf("%6s", FormatNum(n.Connections)))
		getCell := lipgloss.NewStyle().Foreground(ColorCyan).Render(fmt.Sprintf("%7s", FormatNum(n.GetTypeCmds)))
		evictCell := lipgloss.NewStyle().Foreground(evictColor(n.Evictions, evictWarn, evictCrit)).Bold(true).
			Render(fmt.Sprintf("%6s", FormatNum(n.Evictions)))

		lines = append(lines, fmt.Sprintf("%s %s %s %s %s %s %s", nameCell, roleCell, cpuCell, lagCell, connCell, getCell, evictCell))
	}

	if hottestIdx >= 0 {
		lines = append(lines, "",
			lipgloss.NewStyle().Foreground(ColorPurple).Render("◆ hottest replica: ")+
				ValueStyle.Render(fmt.Sprintf("%.1f%% eCPU", hottestCPU)))
	}

	return strings.Join(lines, "\n")
}

func redisLagCell(n collector.RedisNodeMetrics) string {
	if !n.HasReplicationLag {
		return LabelStyle.Render(fmt.Sprintf("%7s", "--"))
	}

	text := "0"
	if n.ReplicationLag > 0 && n.ReplicationLag < 1 {
		text = fmt.Sprintf("%.0fms", n.ReplicationLag*1000)
	} else if n.ReplicationLag >= 1 && n.ReplicationLag < 10 {
		text = fmt.Sprintf("%.1fs", n.ReplicationLag)
	} else if n.ReplicationLag >= 10 {
		text = fmt.Sprintf("%.0fs", n.ReplicationLag)
	}

	color := ColorGreen
	if n.ReplicationLag >= 5 {
		color = ColorRed
	} else if n.ReplicationLag >= 1 {
		color = ColorYellow
	}
	return lipgloss.NewStyle().Foreground(color).Bold(true).Render(fmt.Sprintf("%7s", text))
}

// cpuColor returns green/yellow/red for a CPU percentage against warn/crit thresholds.
func cpuColor(pct, warn, crit float64) lipgloss.Color {
	switch {
	case crit > 0 && pct >= crit:
		return ColorRed
	case warn > 0 && pct >= warn:
		return ColorYellow
	default:
		return ColorGreen
	}
}

// evictColor returns green/yellow/red for an eviction count against warn/crit thresholds.
func evictColor(evictions, warn, crit float64) lipgloss.Color {
	switch {
	case crit > 0 && evictions >= crit:
		return ColorRed
	case warn > 0 && evictions >= warn:
		return ColorYellow
	case evictions > 0:
		return ColorYellow
	default:
		return ColorGreen
	}
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
