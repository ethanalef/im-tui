package view

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"im-tui/alert"
	"im-tui/collector"
)

const (
	naText    = "N/A"
	noDash    = "──"
	sparkW    = 20
	barWidth  = 16
)

// RenderOverview composites key metrics from all 4 data sources into a single
// 2x2 panel grid styled after btop.
func RenderOverview(
	width, height int,
	prom *collector.PrometheusSnapshot,
	cw *collector.CloudWatchSnapshot,
	k8s *collector.KubernetesSnapshot,
	locust *collector.LocustSnapshot,
	ev *alert.Evaluator,
	tsOnline, tsMsgs, tsSendRate,
	tsLocustRPS, tsLocustFail,
	tsDocDBCPU, tsRdsCPU, tsAlbRT *collector.TimeSeries,
) string {
	// Calculate panel dimensions
	halfW := width / 2
	if halfW < 20 {
		halfW = 20
	}
	// Reserve 1 row for any alert banner at the bottom
	alertCount := 0
	var activeAlerts []alert.Alert
	if ev != nil {
		activeAlerts = ev.Active()
		alertCount = len(activeAlerts)
	}
	availableH := height
	if alertCount > 0 {
		availableH = height - 1
	}
	halfH := availableH / 2
	if halfH < 6 {
		halfH = 6
	}

	topLeft := renderAppPanel(halfW, halfH, prom, tsOnline, tsMsgs, tsSendRate)
	topRight := renderClientPanel(halfW, halfH, locust, tsLocustRPS, tsLocustFail)
	botLeft := renderInfraPanel(halfW, halfH, cw, tsDocDBCPU, tsRdsCPU, tsAlbRT)
	botRight := renderK8sPanel(halfW, halfH, k8s)

	topRow := lipgloss.JoinHorizontal(lipgloss.Top, topLeft, topRight)
	botRow := lipgloss.JoinHorizontal(lipgloss.Top, botLeft, botRight)
	grid := lipgloss.JoinVertical(lipgloss.Left, topRow, botRow)

	// Append a one-line alert banner when there are active alerts
	if alertCount > 0 {
		banner := renderAlertBanner(width, activeAlerts)
		grid = lipgloss.JoinVertical(lipgloss.Left, grid, banner)
	}

	return grid
}

// ---------------------------------------------------------------------------
// Top-left: Application (Prometheus)
// ---------------------------------------------------------------------------

func renderAppPanel(w, h int, prom *collector.PrometheusSnapshot, tsOnline, tsMsgs, tsSendRate *collector.TimeSeries) string {
	if prom == nil || prom.Err != nil {
		msg := naText
		if prom != nil && prom.Err != nil {
			msg = "error: " + truncateErr(prom.Err)
		}
		return Panel("Application (Prometheus)", dimLine(msg), w, h)
	}

	innerW := w - 4 // borders + small padding

	var lines []string

	// Online users with sparkline
	lines = append(lines,
		fmtMetricSparkline("Online Users", FormatNum(prom.OnlineUsers), tsOnline, innerW),
	)

	// Messages in last 5 min with sparkline (rate * 300s = count)
	lines = append(lines,
		fmtMetricSparkline("Msgs/5min", FormatNum(prom.MsgsIn5Min*300), tsMsgs, innerW),
	)

	// Msgs/s (inbound) with sparkline
	lines = append(lines,
		fmtMetricSparkline("Msgs/s (In)", FormatRate(prom.SendRate), tsSendRate, innerW),
	)

	// Chat success / fail
	singleOK := FormatRate(prom.SingleChatOK)
	singleFail := FormatRate(prom.SingleChatFail)
	groupOK := FormatRate(prom.GroupChatOK)
	groupFail := FormatRate(prom.GroupChatFail)

	lines = append(lines, "")
	lines = append(lines,
		LabelStyle.Render("Single Chat  ")+
			StatusOK.Render("OK ")+ValueStyle.Render(singleOK)+
			LabelStyle.Render("  ")+
			StatusErr.Render("Fail ")+failValue(prom.SingleChatFail, singleFail),
	)
	lines = append(lines,
		LabelStyle.Render("Group  Chat  ")+
			StatusOK.Render("OK ")+ValueStyle.Render(groupOK)+
			LabelStyle.Render("  ")+
			StatusErr.Render("Fail ")+failValue(prom.GroupChatFail, groupFail),
	)

	// Gateway dead connection health check
	for _, pm := range prom.PodMetrics {
		isGW := strings.Contains(pm.Pod, "msg-gateway") || strings.Contains(pm.Pod, "gateway")
		if !isGW {
			continue
		}
		heapMB := pm.HeapInUse / (1 << 20)
		switch {
		case pm.Goroutines >= 10000:
			lines = append(lines, "")
			lines = append(lines,
				AlertCritical.Render("GW DEGRADED")+
					LabelStyle.Render(" goroutines=")+AlertCritical.Render(FormatNum(pm.Goroutines))+
					LabelStyle.Render(" heap=")+AlertCritical.Render(fmt.Sprintf("%.0fMi", heapMB))+
					LabelStyle.Render(" REDEPLOY"))
		case pm.Goroutines >= 1000:
			lines = append(lines, "")
			lines = append(lines,
				AlertWarning.Render("GW WARN")+
					LabelStyle.Render(" goroutines=")+AlertWarning.Render(FormatNum(pm.Goroutines))+
					LabelStyle.Render(" heap=")+AlertWarning.Render(fmt.Sprintf("%.0fMi", heapMB)))
		}
	}

	// Storage pipeline failure indicators (compact)
	anyStorageFail := prom.RedisInsertFail > 0 || prom.MongoInsertFail > 0 || prom.SeqSetFail > 0
	anyPushFail := prom.PushFail > 0 || prom.API5XX > 0 || prom.LongTimePush > 0
	anyLagIssue := false // lag growth removed (per-batch vs per-msg counter mismatch); use CloudWatch MSK lag
	if anyStorageFail || anyPushFail || anyLagIssue {
		lines = append(lines, "")
		var failParts []string
		if prom.RedisInsertFail > 0 {
			failParts = append(failParts, AlertCritical.Render("Redis:"+FormatRate(prom.RedisInsertFail)))
		}
		if prom.MongoInsertFail > 0 {
			failParts = append(failParts, AlertCritical.Render("Mongo:"+FormatRate(prom.MongoInsertFail)))
		}
		if prom.SeqSetFail > 0 {
			failParts = append(failParts, AlertCritical.Render("Seq:"+FormatRate(prom.SeqSetFail)))
		}
		if prom.LongTimePush > 0 {
			failParts = append(failParts, AlertWarning.Render("PushSlow:"+FormatRate(prom.LongTimePush)))
		}
		if prom.PushFail > 0 {
			failParts = append(failParts, AlertWarning.Render("Push:"+FormatRate(prom.PushFail)))
		}
		if prom.API5XX > 0 {
			failParts = append(failParts, AlertWarning.Render("5XX:"+FormatRate(prom.API5XX)))
		}
		_ = anyLagIssue // reserved for CloudWatch MSK lag alerts
		lines = append(lines, strings.Join(failParts, LabelStyle.Render(" ")))
	}

	return Panel("Application (Prometheus)", strings.Join(lines, "\n"), w, h)
}

// ---------------------------------------------------------------------------
// Top-right: Client (Locust)
// ---------------------------------------------------------------------------

func renderClientPanel(w, h int, locust *collector.LocustSnapshot, tsRPS, tsFail *collector.TimeSeries) string {
	if locust == nil || locust.Err != nil {
		msg := naText
		if locust != nil && locust.Err != nil {
			msg = "error: " + truncateErr(locust.Err)
		}
		return Panel("Client (Locust)", dimLine(msg), w, h)
	}

	if !locust.Available || locust.State == "stopped" {
		content := LabelStyle.Render("  No test running")
		return Panel("Client (Locust)", content, w, h)
	}

	innerW := w - 4

	var lines []string

	// State + users
	stateStyled := ValueStyle.Render(locust.State)
	usersStyled := ValueStyle.Render(fmt.Sprintf("%d", locust.UserCount))
	lines = append(lines,
		LabelStyle.Render("State ")+stateStyled+
			LabelStyle.Render("   Users ")+usersStyled,
	)

	// RPS with sparkline
	lines = append(lines,
		fmtMetricSparkline("RPS", fmt.Sprintf("%.1f", locust.TotalRPS), tsRPS, innerW),
	)

	// Fail ratio with sparkline
	failPct := locust.FailRatio * 100
	failStr := fmt.Sprintf("%.2f%%", failPct)
	var failStyled string
	if failPct >= 5 {
		failStyled = AlertCritical.Render(failStr)
	} else if failPct >= 1 {
		failStyled = AlertWarning.Render(failStr)
	} else {
		failStyled = ValueStyle.Render(failStr)
	}
	sparkFail := sparkline(tsFail, sparkW)
	lines = append(lines,
		LabelStyle.Render("Fail%  ")+failStyled+LabelStyle.Render("  ")+sparkFail,
	)

	// Aggregated latencies: find "Aggregated" or compute from endpoints
	var p50, p95, p99 float64
	for _, ep := range locust.Endpoints {
		// Sum up a weighted average is hard; just take max as overview hint
		if ep.P50 > p50 {
			p50 = ep.P50
		}
		if ep.P95 > p95 {
			p95 = ep.P95
		}
		if ep.P99 > p99 {
			p99 = ep.P99
		}
	}

	lines = append(lines, "")
	lines = append(lines,
		LabelStyle.Render("P50 ")+latencyValue(p50)+
			LabelStyle.Render("  P95 ")+latencyValue(p95)+
			LabelStyle.Render("  P99 ")+latencyValue(p99),
	)

	return Panel("Client (Locust)", strings.Join(lines, "\n"), w, h)
}

// ---------------------------------------------------------------------------
// Bottom-left: Infrastructure (CloudWatch)
// ---------------------------------------------------------------------------

func renderInfraPanel(w, h int, cw *collector.CloudWatchSnapshot, tsDocDBCPU, tsRdsCPU, tsAlbRT *collector.TimeSeries) string {
	if cw == nil || cw.Err != nil {
		msg := naText
		if cw != nil && cw.Err != nil {
			msg = "error: " + truncateErr(cw.Err)
		}
		return Panel("Infrastructure (CloudWatch)", dimLine(msg), w, h)
	}

	innerBarW := barWidth
	// Ensure bar fits within panel
	if w-30 < innerBarW {
		innerBarW = w - 30
		if innerBarW < 6 {
			innerBarW = 6
		}
	}

	var lines []string

	// DocDB
	lines = append(lines,
		LabelStyle.Render("DocDB  ")+
			LabelStyle.Render("CPU ")+ProgressBar(cw.DocDB.CPUPercent, innerBarW)+" "+pctColored(cw.DocDB.CPUPercent)+
			LabelStyle.Render("  Conns ")+connColored(cw.DocDB.Connections, 80, 100),
	)

	// MySQL (RDS) - estimate memory percent from FreeMemory (show raw if unknown total)
	rdsMemLabel := FormatBytes(cw.RDS.FreeMemory) + " free"
	totalIOPS := cw.RDS.ReadIOPS + cw.RDS.WriteIOPS
	lines = append(lines,
		LabelStyle.Render("MySQL  ")+
			LabelStyle.Render("CPU ")+ProgressBar(cw.RDS.CPUPercent, innerBarW)+" "+pctColored(cw.RDS.CPUPercent)+
			LabelStyle.Render("  Mem ")+ValueStyle.Render(rdsMemLabel)+
			LabelStyle.Render("  IOPS ")+ValueStyle.Render(fmt.Sprintf("%.0f", totalIOPS)),
	)

	// Redis - show first node summary or average if multiple
	if len(cw.Redis) > 0 {
		r := cw.Redis[0]
		nodeLabel := "Redis  "
		if len(cw.Redis) > 1 {
			nodeLabel = fmt.Sprintf("Redis(%d)", len(cw.Redis))
		}
		lines = append(lines,
			LabelStyle.Render(nodeLabel)+
				LabelStyle.Render("CPU ")+ProgressBar(r.CPUPercent, innerBarW)+" "+pctColored(r.CPUPercent)+
				LabelStyle.Render("  Mem ")+ProgressBar(r.MemoryPercent, innerBarW)+" "+pctColored(r.MemoryPercent),
		)
	} else {
		lines = append(lines, LabelStyle.Render("Redis  ")+LabelStyle.Render(noDash))
	}

	// Separator
	lines = append(lines, "")

	// ALB summary
	rtStr := fmt.Sprintf("%.0fms", cw.ALB.ResponseTimeP99*1000)
	e5xx := fmt.Sprintf("%.0f", cw.ALB.Count5XX)
	conns := FormatNum(cw.ALB.ActiveConns)
	reqs := FormatNum(cw.ALB.RequestCount)

	var rtStyled string
	rtMs := cw.ALB.ResponseTimeP99 * 1000
	switch {
	case rtMs >= 2000:
		rtStyled = AlertCritical.Render(rtStr)
	case rtMs >= 1000:
		rtStyled = AlertWarning.Render(rtStr)
	default:
		rtStyled = ValueStyle.Render(rtStr)
	}

	var e5xxStyled string
	if cw.ALB.Count5XX > 0 {
		e5xxStyled = AlertWarning.Render(e5xx)
	} else {
		e5xxStyled = ValueStyle.Render(e5xx)
	}

	lines = append(lines,
		LabelStyle.Render("ALB  ")+
			LabelStyle.Render("P99 ")+rtStyled+
			LabelStyle.Render("  5XX ")+e5xxStyled+
			LabelStyle.Render("  Conns ")+ValueStyle.Render(conns)+
			LabelStyle.Render("  Reqs ")+ValueStyle.Render(reqs),
	)

	// Kafka (MSK) consumer lag summary
	if cw.MSK.TotalLag > 0 || len(cw.MSK.ConsumerLag) > 0 {
		var lagStr string
		switch {
		case cw.MSK.TotalLag >= 10000:
			lagStr = AlertCritical.Render(FormatNum(cw.MSK.TotalLag))
		case cw.MSK.TotalLag >= 1000:
			lagStr = AlertWarning.Render(FormatNum(cw.MSK.TotalLag))
		case cw.MSK.TotalLag > 0:
			lagStr = ValueStyle.Render(FormatNum(cw.MSK.TotalLag))
		default:
			lagStr = StatusOK.Render("0")
		}
		lines = append(lines,
			LabelStyle.Render("Kafka  ")+
				LabelStyle.Render("Lag ")+lagStr,
		)
	}

	return Panel("Infrastructure (CloudWatch)", strings.Join(lines, "\n"), w, h)
}

// ---------------------------------------------------------------------------
// Bottom-right: Kubernetes
// ---------------------------------------------------------------------------

func renderK8sPanel(w, h int, k8s *collector.KubernetesSnapshot) string {
	if k8s == nil || k8s.Err != nil {
		msg := naText
		if k8s != nil && k8s.Err != nil {
			msg = "error: " + truncateErr(k8s.Err)
		}
		return Panel("Kubernetes", dimLine(msg), w, h)
	}

	var lines []string

	// Pod summary counts
	totalPods := len(k8s.Pods)
	running := 0
	totalRestarts := 0
	var warnings []string
	for _, p := range k8s.Pods {
		if p.Status == "Running" {
			running++
		}
		totalRestarts += p.Restarts
		if p.Restarts > 5 {
			warnings = append(warnings, fmt.Sprintf("%s (%d restarts)", Truncate(p.Name, 28), p.Restarts))
		}
	}

	podCountStr := ValueStyle.Render(fmt.Sprintf("%d", totalPods))
	runningStr := StatusOK.Render(fmt.Sprintf("%d", running))
	notRunning := totalPods - running
	var notRunStr string
	if notRunning > 0 {
		notRunStr = LabelStyle.Render("  Not ready ") + AlertWarning.Render(fmt.Sprintf("%d", notRunning))
	}

	lines = append(lines,
		LabelStyle.Render("Pods ")+podCountStr+
			LabelStyle.Render("  Running ")+runningStr+
			notRunStr,
	)

	// Restarts
	var restartStyled string
	switch {
	case totalRestarts >= 20:
		restartStyled = AlertCritical.Render(fmt.Sprintf("%d", totalRestarts))
	case totalRestarts >= 5:
		restartStyled = AlertWarning.Render(fmt.Sprintf("%d", totalRestarts))
	default:
		restartStyled = ValueStyle.Render(fmt.Sprintf("%d", totalRestarts))
	}
	lines = append(lines,
		LabelStyle.Render("Total Restarts ")+restartStyled,
	)

	// HPA summary
	if len(k8s.HPAs) > 0 {
		lines = append(lines, "")
		lines = append(lines, LabelStyle.Render("HPA:"))
		maxHPA := 3
		if len(k8s.HPAs) < maxHPA {
			maxHPA = len(k8s.HPAs)
		}
		for _, hpa := range k8s.HPAs[:maxHPA] {
			lines = append(lines,
				LabelStyle.Render("  ")+
					ValueStyle.Render(Truncate(hpa.Name, 20))+
					LabelStyle.Render(" ")+
					ValueStyle.Render(fmt.Sprintf("%d", hpa.Current))+
					LabelStyle.Render(fmt.Sprintf("/%d ", hpa.MaxReplicas))+
					LabelStyle.Render("(")+ValueStyle.Render(hpa.Targets)+LabelStyle.Render(")"),
			)
		}
		if len(k8s.HPAs) > 3 {
			lines = append(lines, LabelStyle.Render(fmt.Sprintf("  ... +%d more", len(k8s.HPAs)-3)))
		}
	}

	// Warnings (recent events)
	warnEvents := 0
	for _, e := range k8s.Events {
		if e.Type == "Warning" {
			warnEvents++
		}
	}
	if warnEvents > 0 || len(warnings) > 0 {
		lines = append(lines, "")
		if warnEvents > 0 {
			lines = append(lines,
				AlertWarning.Render(fmt.Sprintf("  %d warning event(s)", warnEvents)),
			)
		}
		for _, w := range warnings {
			lines = append(lines, AlertWarning.Render("  "+w))
		}
	}

	return Panel("Kubernetes", strings.Join(lines, "\n"), w, h)
}

// ---------------------------------------------------------------------------
// Alert banner
// ---------------------------------------------------------------------------

func renderAlertBanner(width int, alerts []alert.Alert) string {
	if len(alerts) == 0 {
		return ""
	}

	parts := make([]string, 0, len(alerts))
	for _, a := range alerts {
		var s string
		switch a.Level {
		case alert.LevelCritical:
			s = AlertCritical.Render("CRIT " + a.Metric + "=" + a.Value)
		default:
			s = AlertWarning.Render("WARN " + a.Metric + "=" + a.Value)
		}
		parts = append(parts, s)
	}

	banner := strings.Join(parts, LabelStyle.Render(" | "))
	return Truncate(banner, width)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// fmtMetricSparkline renders: "Label  value  ▁▂▃▅▇"
func fmtMetricSparkline(label, value string, ts *collector.TimeSeries, innerW int) string {
	spark := sparkline(ts, sparkW)
	return LabelStyle.Render(label+"  ") + ValueStyle.Render(value) + LabelStyle.Render("  ") + spark
}

func sparkline(ts *collector.TimeSeries, w int) string {
	if ts == nil || ts.Len() == 0 {
		return LabelStyle.Render(noDash)
	}
	return SparklineStr(ts.Values(), w)
}

func dimLine(msg string) string {
	return LabelStyle.Render("  " + msg)
}

func pctColored(pct float64) string {
	s := fmt.Sprintf("%5.1f%%", pct)
	switch {
	case pct >= 80:
		return lipgloss.NewStyle().Foreground(ColorRed).Bold(true).Render(s)
	case pct >= 60:
		return lipgloss.NewStyle().Foreground(ColorYellow).Bold(true).Render(s)
	default:
		return lipgloss.NewStyle().Foreground(ColorGreen).Bold(true).Render(s)
	}
}

func failValue(raw float64, formatted string) string {
	if raw > 0 {
		return AlertCritical.Render(formatted)
	}
	return ValueStyle.Render(formatted)
}

func latencyValue(ms float64) string {
	s := fmt.Sprintf("%.0fms", ms)
	switch {
	case ms >= 2000:
		return AlertCritical.Render(s)
	case ms >= 500:
		return AlertWarning.Render(s)
	default:
		return ValueStyle.Render(s)
	}
}

func connColored(count, warn, crit float64) string {
	s := fmt.Sprintf("%.0f", count)
	switch {
	case count >= crit:
		return AlertCritical.Render(s)
	case count >= warn:
		return AlertWarning.Render(s)
	default:
		return ValueStyle.Render(s)
	}
}

func truncateErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) > 40 {
		return s[:37] + "..."
	}
	return s
}
