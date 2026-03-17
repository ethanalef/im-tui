package view

import (
	"fmt"
	"math"
	"strings"

	"im-tui/collector"

	"github.com/charmbracelet/lipgloss"
)

// RenderApplication renders the Application tab (Tab 2) showing detailed
// Prometheus application metrics in a btop-inspired layout:
//   - Top panel: sparkline graphs for online users, msgs in 5 min, send rate
//   - Middle-left: message processing success/fail counters
//   - Middle-right: msg-transfer storage pipeline + push/login/API
//   - Bottom panel: per-pod table (goroutines, memory alloc)
func RenderApplication(
	width, height int,
	prom *collector.PrometheusSnapshot,
	cw *collector.CloudWatchSnapshot,
	tsOnline, tsMsgs, tsSendRate *collector.TimeSeries,
	tsRedisOK, tsMongoOK, tsLogin *collector.TimeSeries,
	tsGatewaySend *collector.TimeSeries,
	tsKafkaLag, tsMsgLagGrowth *collector.TimeSeries,
	tsLongTimePush *collector.TimeSeries,
	scrollPos int,
) string {
	if prom == nil || prom.Err != nil {
		msg := "Waiting for data..."
		if prom != nil && prom.Err != nil {
			msg = fmt.Sprintf("Error: %v", prom.Err)
		}
		placeholder := lipgloss.NewStyle().
			Foreground(ColorSubtext).
			Width(width).
			Align(lipgloss.Center).
			Render(msg)
		return placeholder
	}

	// Layout proportions: top ~25%, mid-top ~20%, mid-bot ~20%, bottom ~35%
	topHeight := height * 25 / 100
	if topHeight < 7 {
		topHeight = 7
	}
	midTopHeight := height * 20 / 100
	if midTopHeight < 6 {
		midTopHeight = 6
	}
	midBotHeight := height * 20 / 100
	if midBotHeight < 6 {
		midBotHeight = 6
	}
	botHeight := height - topHeight - midTopHeight - midBotHeight
	if botHeight < 5 {
		botHeight = 5
	}

	topPanel := renderSparklinePanel(width, topHeight, prom, tsOnline, tsMsgs, tsSendRate, tsGatewaySend)
	midTopPanel := renderMessageCounters(width, midTopHeight, prom)
	midBotPanel := renderStoragePipeline(width, midBotHeight, prom, cw, tsRedisOK, tsMongoOK, tsLogin, tsKafkaLag, tsMsgLagGrowth, tsLongTimePush)
	botPanel := renderPodMetricsTable(width, botHeight, prom.PodMetrics, scrollPos)

	return lipgloss.JoinVertical(lipgloss.Left, topPanel, midTopPanel, midBotPanel, botPanel)
}

// renderSparklinePanel draws sparkline rows inside a single panel.
func renderSparklinePanel(width, height int, prom *collector.PrometheusSnapshot, tsOnline, tsMsgs, tsSendRate, tsGatewaySend *collector.TimeSeries) string {
	innerW := width - 4 // panel border + small padding

	// Each sparkline row: label (fixed width) + sparkline + current value
	labelW := 18
	valueW := 12
	sparkW := innerW - labelW - valueW - 2
	if sparkW < 10 {
		sparkW = 10
	}

	rows := []struct {
		label string
		ts    *collector.TimeSeries
		cur   float64
		fmt   string
	}{
		{"Online Users", tsOnline, prom.OnlineUsers, FormatNum(prom.OnlineUsers)},
		{"Msgs / 5 min", tsMsgs, prom.MsgsIn5Min * 300, FormatNum(prom.MsgsIn5Min * 300)},
		{"Send Rate", tsSendRate, prom.SendRate, FormatRate(prom.SendRate)},
		{"GW Send Rate", tsGatewaySend, prom.GatewaySendRate, FormatRate(prom.GatewaySendRate)},
	}

	var lines []string
	for _, r := range rows {
		label := LabelStyle.Render(PadRight(r.label, labelW))
		var spark string
		if r.ts != nil {
			spark = SparklineStr(r.ts.Values(), sparkW)
		} else {
			spark = SparklineStr(nil, sparkW)
		}
		value := ValueStyle.Render(fmt.Sprintf("%*s", valueW, r.fmt))
		lines = append(lines, label+spark+" "+value)
	}

	content := strings.Join(lines, "\n")
	return Panel("Metrics", content, width, height)
}

// renderMessageCounters draws the message success/fail counters as a two-column layout.
func renderMessageCounters(width, height int, prom *collector.PrometheusSnapshot) string {
	innerW := width - 4

	okStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorGreen)
	failStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorRed)

	colW := innerW / 2
	if colW < 20 {
		colW = 20
	}

	// Single chat column
	singleHeader := ValueStyle.Render("Single Chat")
	singleOK := LabelStyle.Render("  OK:   ") + okStyle.Render(FormatNum(prom.SingleChatOK))
	singleFail := LabelStyle.Render("  Fail: ") + failStyle.Render(FormatNum(prom.SingleChatFail))

	// Group chat column
	groupHeader := ValueStyle.Render("Group Chat")
	groupOK := LabelStyle.Render("  OK:   ") + okStyle.Render(FormatNum(prom.GroupChatOK))
	groupFail := LabelStyle.Render("  Fail: ") + failStyle.Render(FormatNum(prom.GroupChatFail))

	// Build each column as a block
	leftLines := []string{singleHeader, singleOK, singleFail}
	rightLines := []string{groupHeader, groupOK, groupFail}

	var lines []string
	for i := 0; i < len(leftLines); i++ {
		left := PadRight(leftLines[i], colW)
		right := ""
		if i < len(rightLines) {
			right = rightLines[i]
		}
		lines = append(lines, left+right)
	}

	content := strings.Join(lines, "\n")
	return Panel("Message Processing", content, width, height)
}

// renderStoragePipeline draws the msg-transfer storage pipeline, Kafka lag, push/login/API metrics.
func renderStoragePipeline(width, height int, prom *collector.PrometheusSnapshot, cw *collector.CloudWatchSnapshot, tsRedisOK, tsMongoOK, tsLogin, tsKafkaLag, tsMsgLagGrowth, tsLongTimePush *collector.TimeSeries) string {
	innerW := width - 4

	okStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorGreen)

	halfW := innerW / 2
	if halfW < 20 {
		halfW = 20
	}

	// Lag growth rate from Prometheus (production - consumption)
	lagGrowth := prom.MsgLagGrowthRate
	var lagStyled string
	switch {
	case lagGrowth > 1:
		lagStyled = lipgloss.NewStyle().Bold(true).Foreground(ColorRed).Render(fmt.Sprintf("+%.2f/s", lagGrowth))
	case lagGrowth > 0:
		lagStyled = lipgloss.NewStyle().Bold(true).Foreground(ColorYellow).Render(fmt.Sprintf("+%.2f/s", lagGrowth))
	case lagGrowth < 0:
		lagStyled = okStyle.Render(fmt.Sprintf("%.2f/s", lagGrowth))
	default:
		lagStyled = okStyle.Render("0.00/s")
	}

	// Left column: msg-transfer pipeline + lag indicator (compact layout)
	leftLines := []string{
		LabelStyle.Render("Redis Insert  ") +
			LabelStyle.Render("OK ") + okStyle.Render(FormatRate(prom.RedisInsertOK)) +
			LabelStyle.Render("  Fail ") + failRateValue(prom.RedisInsertFail),
		LabelStyle.Render("Mongo Insert  ") +
			LabelStyle.Render("OK ") + okStyle.Render(FormatRate(prom.MongoInsertOK)) +
			LabelStyle.Render("  Fail ") + failRateValue(prom.MongoInsertFail),
		LabelStyle.Render("Seq Set Fail  ") + failRateValue(prom.SeqSetFail) +
			LabelStyle.Render("  Lag ") + lagStyled,
	}

	// Kafka consumer lag per group (from CloudWatch MSK, when available)
	if cw != nil && cw.Err == nil && len(cw.MSK.ConsumerLag) > 0 {
		var lagParts []string
		for _, cl := range cw.MSK.ConsumerLag {
			lagParts = append(lagParts, LabelStyle.Render(cl.Group+" ")+lagValue(cl.Lag))
		}
		leftLines = append(leftLines, LabelStyle.Render("Kafka Lag  ")+strings.Join(lagParts, LabelStyle.Render("  ")))
	}

	// Right column: push pipeline + activity + per-service 5XX
	rightLines := []string{
		LabelStyle.Render("Push Slow(>10s) ") + failRateValue(prom.LongTimePush) +
			LabelStyle.Render(" Fail ") + failRateValue(prom.PushFail),
		LabelStyle.Render("Push Delivery  ") +
			LabelStyle.Render("GW ") + okStyle.Render(FormatRate(prom.GatewaySendRate)) +
			LabelStyle.Render("  Ratio ") + pushRatioValue(prom.GatewaySendRate, prom.SingleChatOK+prom.GroupChatOK),
		LabelStyle.Render("Push Queue  ") +
			LabelStyle.Render("InFlight ") + inFlightValue(prom.PushMsgInFlight) +
			LabelStyle.Render("  Proc ") + pushLatencyValue(prom.PushProcessingP95) +
			LabelStyle.Render("  gRPC ") + pushLatencyValue(prom.PushGrpcDeliveryP95) +
			LabelStyle.Render("  WS ") + wsQueueValue(prom.GatewayWsQueueP95),
		LabelStyle.Render("Login ") + okStyle.Render(FormatRate(prom.UserLogin)) +
			LabelStyle.Render("  Register ") + okStyle.Render(FormatRate(prom.UserRegister)),
		LabelStyle.Render("5XX  chat-api ") + failRateValue(prom.ChatAPI5XX) +
			LabelStyle.Render("  openim-api ") + failRateValue(prom.OpenIMAPI5XX),
	}

	var lines []string
	maxRows := len(leftLines)
	if len(rightLines) > maxRows {
		maxRows = len(rightLines)
	}
	for i := 0; i < maxRows; i++ {
		left := ""
		if i < len(leftLines) {
			left = leftLines[i]
		}
		right := ""
		if i < len(rightLines) {
			right = rightLines[i]
		}
		lines = append(lines, PadRight(left, halfW)+right)
	}

	content := strings.Join(lines, "\n")
	return Panel("Storage Pipeline / Push / Kafka", content, width, height)
}

// lagValue renders a consumer group lag value with color coding.
func lagValue(lag float64) string {
	s := FormatNum(lag)
	switch {
	case lag >= 10000:
		return lipgloss.NewStyle().Bold(true).Foreground(ColorRed).Render(s)
	case lag >= 1000:
		return lipgloss.NewStyle().Bold(true).Foreground(ColorYellow).Render(s)
	case lag > 0:
		return lipgloss.NewStyle().Bold(true).Foreground(ColorCyan).Render(s)
	default:
		return lipgloss.NewStyle().Bold(true).Foreground(ColorGreen).Render(s)
	}
}

// failRateValue renders a rate value in red if > 0, green otherwise.
func failRateValue(rate float64) string {
	s := FormatRate(rate)
	if rate > 0 {
		return lipgloss.NewStyle().Bold(true).Foreground(ColorRed).Render(s)
	}
	return lipgloss.NewStyle().Bold(true).Foreground(ColorGreen).Render(s)
}

// pushRatioValue renders the push delivery ratio (gateway sends per message processed).
// During group messaging, this equals the average fan-out factor.
// A dropping ratio during constant group size means the push pipeline is falling behind.
func pushRatioValue(gatewaySend, msgProcessed float64) string {
	if msgProcessed < 0.001 {
		return lipgloss.NewStyle().Foreground(ColorSubtext).Render("--")
	}
	ratio := gatewaySend / msgProcessed
	s := fmt.Sprintf("%.0f:1", ratio)
	if ratio < 1 && gatewaySend > 0 {
		// Gateway sending slower than production — push pipeline behind
		return lipgloss.NewStyle().Bold(true).Foreground(ColorYellow).Render(s)
	}
	return lipgloss.NewStyle().Bold(true).Foreground(ColorGreen).Render(s)
}

// inFlightValue renders a push in-flight gauge with color coding.
func inFlightValue(n float64) string {
	s := FormatNum(n)
	switch {
	case n >= 20:
		return lipgloss.NewStyle().Bold(true).Foreground(ColorRed).Render(s)
	case n >= 5:
		return lipgloss.NewStyle().Bold(true).Foreground(ColorYellow).Render(s)
	default:
		return lipgloss.NewStyle().Bold(true).Foreground(ColorGreen).Render(s)
	}
}

// pushLatencyValue renders a p95 latency in human-readable form with color coding.
// NaN means no observations in the time window (idle).
func pushLatencyValue(seconds float64) string {
	if math.IsNaN(seconds) {
		return lipgloss.NewStyle().Foreground(ColorSubtext).Render("--")
	}
	s := FormatLatency(seconds)
	switch {
	case seconds >= 1:
		return lipgloss.NewStyle().Bold(true).Foreground(ColorRed).Render(s)
	case seconds >= 0.1:
		return lipgloss.NewStyle().Bold(true).Foreground(ColorYellow).Render(s)
	default:
		return lipgloss.NewStyle().Bold(true).Foreground(ColorGreen).Render(s)
	}
}

// wsQueueValue renders a WebSocket write queue depth (out of 256 capacity).
// NaN means no observations in the time window (idle) — display as 0.
func wsQueueValue(depth float64) string {
	if math.IsNaN(depth) {
		depth = 0
	}
	s := fmt.Sprintf("%.0f/256", depth)
	switch {
	case depth >= 200:
		return lipgloss.NewStyle().Bold(true).Foreground(ColorRed).Render(s)
	case depth >= 50:
		return lipgloss.NewStyle().Bold(true).Foreground(ColorYellow).Render(s)
	default:
		return lipgloss.NewStyle().Bold(true).Foreground(ColorGreen).Render(s)
	}
}

// isGatewayPod returns true if the pod name indicates a msg-gateway pod.
func isGatewayPod(podName string) bool {
	return strings.Contains(podName, "msg-gateway") || strings.Contains(podName, "gateway")
}

// gwHealthStatus evaluates gateway health by comparing goroutines to online users.
// Each online user needs ~3 goroutines (readMessage + loopSend + doPing/heartbeat).
// A healthy gateway has goroutines ≈ users*3 + 2200 (2048 MemoryQueue workers + ~150 infra).
// Returns: "OK", "WARN", "DEGRADED" and a descriptive reason.
func gwHealthStatus(p collector.PodMetric) (string, string) {
	goroutines := p.Goroutines
	users := p.OnlineUsers
	heapMB := p.HeapInUse / (1 << 20)

	// When online user data is available, use ratio-based detection.
	if users > 0 {
		expected := users*3 + 2200 // 3 goroutines per user + 2048 MemoryQueue workers + ~150 infra
		excess := goroutines - expected
		switch {
		case excess >= 5000:
			return "DEGRADED", fmt.Sprintf("%.0f goroutines vs %.0f expected (%.0f users) — zombie leak", goroutines, expected, users)
		case excess >= 2000:
			return "WARN", fmt.Sprintf("%.0f goroutines vs %.0f expected (%.0f users) — possible leak", goroutines, expected, users)
		}
	} else {
		// No online user data — fall back to high absolute thresholds.
		switch {
		case goroutines >= 50000:
			return "DEGRADED", fmt.Sprintf("%.0f goroutines — no online_user_num data", goroutines)
		case goroutines >= 20000:
			return "WARN", fmt.Sprintf("%.0f goroutines — no online_user_num data", goroutines)
		}
	}

	if heapMB >= 500 {
		return "WARN", fmt.Sprintf("%.0f MiB heap — elevated memory", heapMB)
	}
	return "OK", ""
}

// goroutineStyled renders a goroutine count with color coding.
// For gateway pods, color is based on excess above expected (users*3 + base).
func goroutineStyled(count float64, isGW bool, onlineUsers float64) string {
	s := FormatNum(count)
	if isGW {
		expected := onlineUsers*3 + 2200
		excess := count - expected
		switch {
		case excess >= 5000:
			return lipgloss.NewStyle().Bold(true).Foreground(ColorRed).Render(s)
		case excess >= 2000:
			return lipgloss.NewStyle().Bold(true).Foreground(ColorOrange).Render(s)
		case excess >= 500:
			return lipgloss.NewStyle().Bold(true).Foreground(ColorYellow).Render(s)
		default:
			return lipgloss.NewStyle().Bold(true).Foreground(ColorGreen).Render(s)
		}
	}
	// General service thresholds
	switch {
	case count >= 10000:
		return lipgloss.NewStyle().Bold(true).Foreground(ColorRed).Render(s)
	case count >= 5000:
		return lipgloss.NewStyle().Bold(true).Foreground(ColorYellow).Render(s)
	default:
		return ValueStyle.Render(s)
	}
}

// heapStyled renders heap in-use with color coding.
func heapStyled(bytes float64) string {
	s := FormatBytes(bytes)
	mb := bytes / (1 << 20)
	switch {
	case mb >= 1000:
		return lipgloss.NewStyle().Bold(true).Foreground(ColorRed).Render(s)
	case mb >= 500:
		return lipgloss.NewStyle().Bold(true).Foreground(ColorOrange).Render(s)
	case mb >= 200:
		return lipgloss.NewStyle().Bold(true).Foreground(ColorYellow).Render(s)
	default:
		return lipgloss.NewStyle().Bold(true).Foreground(ColorGreen).Render(s)
	}
}

// renderPodMetricsTable draws a scrollable per-pod table showing goroutines, memory,
// and gateway health status. Gateway pods are color-coded with dead connection leak detection.
func renderPodMetricsTable(width, height int, pods []collector.PodMetric, scrollPos int) string {
	innerW := width - 4
	innerH := height - 2 // panel borders

	// Column widths — expanded to include HeapInUse, Status, and Online columns
	statusCol := 10
	onlineCol := 10
	goroutineCol := 12
	memCol := 12
	heapCol := 12
	podCol := innerW - statusCol - onlineCol - goroutineCol - memCol - heapCol - 8 // separators
	if podCol < 10 {
		podCol = 10
	}

	// Header
	header := TableHeader.Render(
		PadRight("Pod", podCol) + "  " +
			PadRight("Status", statusCol) +
			PadRight("Online", onlineCol) +
			PadRight("Goroutines", goroutineCol) +
			PadRight("Heap InUse", heapCol) +
			PadRight("Mem Alloc", memCol),
	)

	if len(pods) == 0 {
		noData := LabelStyle.Render("No pod metrics available")
		content := header + "\n" + noData
		return Panel("Pod Resources", content, width, height)
	}

	// Check if any gateway pod is degraded for panel title
	gwDegraded := false
	for _, p := range pods {
		if isGatewayPod(p.Pod) {
			status, _ := gwHealthStatus(p)
			if status == "DEGRADED" {
				gwDegraded = true
				break
			}
		}
	}

	// Clamp scroll position
	maxScroll := len(pods) - (innerH - 1) // -1 for header
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scrollPos > maxScroll {
		scrollPos = maxScroll
	}
	if scrollPos < 0 {
		scrollPos = 0
	}

	// Visible rows
	visibleCount := innerH - 1 // minus header
	if visibleCount < 1 {
		visibleCount = 1
	}
	endIdx := scrollPos + visibleCount
	if endIdx > len(pods) {
		endIdx = len(pods)
	}
	visible := pods[scrollPos:endIdx]

	var rows []string
	for _, p := range visible {
		isGW := isGatewayPod(p.Pod)
		name := Truncate(p.Pod, podCol)
		name = PadRight(name, podCol)

		// Status column — gateway-specific health check
		var statusStr string
		if isGW {
			status, _ := gwHealthStatus(p)
			switch status {
			case "DEGRADED":
				statusStr = lipgloss.NewStyle().Bold(true).Foreground(ColorRed).Render("DEGRADED")
			case "WARN":
				statusStr = lipgloss.NewStyle().Bold(true).Foreground(ColorYellow).Render("WARN")
			default:
				statusStr = lipgloss.NewStyle().Bold(true).Foreground(ColorGreen).Render("OK")
			}
		} else {
			statusStr = LabelStyle.Render("--")
		}
		statusStr = PadRight(statusStr, statusCol)

		// Online users column — only meaningful for gateway pods
		var onlineStr string
		if isGW && p.OnlineUsers > 0 {
			onlineStr = ValueStyle.Render(FormatNum(p.OnlineUsers))
		} else if isGW {
			onlineStr = LabelStyle.Render("0")
		} else {
			onlineStr = LabelStyle.Render("--")
		}
		onlineStr = PadRight(onlineStr, onlineCol)

		goroutines := PadRight(goroutineStyled(p.Goroutines, isGW, p.OnlineUsers), goroutineCol)
		heap := PadRight(heapStyled(p.HeapInUse), heapCol)
		mem := PadRight(FormatBytes(p.MemAlloc), memCol)

		row := name + "  " + statusStr + onlineStr + goroutines + heap + mem
		if isGW {
			rows = append(rows, row) // already color-coded per cell
		} else {
			rows = append(rows, TableRow.Render(row))
		}
	}

	// Scroll indicator
	scrollInfo := ""
	if maxScroll > 0 {
		scrollInfo = LabelStyle.Render(fmt.Sprintf(" [%d-%d of %d]", scrollPos+1, endIdx, len(pods)))
	}

	// Panel title highlights degraded gateway
	panelTitle := "Pod Resources"
	if gwDegraded {
		panelTitle = "Pod Resources " + lipgloss.NewStyle().Bold(true).Foreground(ColorRed).Render("GATEWAY DEGRADED")
	}

	content := header + "\n" + strings.Join(rows, "\n")
	return Panel(panelTitle+scrollInfo, content, width, height)
}
