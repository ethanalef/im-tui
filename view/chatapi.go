package view

import (
	"fmt"
	"strings"

	"im-tui/collector"

	"github.com/charmbracelet/lipgloss"
)

// RenderChatAPI renders Tab 9: Chat API & service-level metrics.
// It combines the ChatAPISnapshot (new queries) with PrometheusSnapshot (existing counters).
func RenderChatAPI(
	width, height int,
	chatAPI *collector.ChatAPISnapshot,
	prom *collector.PrometheusSnapshot,
	tsHTTPRate, ts5XX *collector.TimeSeries,
	scrollPos int,
) string {
	if width < 20 {
		return "Terminal too narrow"
	}

	if chatAPI == nil {
		msg := lipgloss.NewStyle().Foreground(ColorSubtext).Render("Waiting for data...") + "\n\n" +
			LabelStyle.Render("Chat API metrics are collected via Prometheus.")
		return renderCentered(width, height-2, msg)
	}
	if chatAPI.Err != nil {
		return renderCentered(width, height-2,
			lipgloss.NewStyle().Foreground(ColorRed).Render("Chat API error: "+chatAPI.Err.Error()))
	}

	// Layout: top row (summary + operations), bottom (endpoint table + counters)
	topH := 12
	bottomH := height - 2 - topH

	// Top row: summary (left) + operations (right)
	halfW := width / 2

	summaryContent := renderAPISummary(halfW-2, topH-2, chatAPI, tsHTTPRate, ts5XX)
	summaryPanel := Panel("HTTP Summary", summaryContent, halfW, topH)

	opsContent := renderAPIOperations(halfW-2, topH-2, chatAPI, prom)
	opsPanel := Panel("Operations", opsContent, halfW, topH)

	topRow := lipgloss.JoinHorizontal(lipgloss.Top, summaryPanel, opsPanel)

	// Bottom row: endpoint table (wide) + counters (right)
	epW := width*2/3 + 1
	counterW := width - epW

	epContent := renderHTTPEndpoints(epW-2, bottomH-2, chatAPI.Endpoints, scrollPos)
	epPanel := Panel("HTTP Endpoints", epContent, epW, bottomH)

	counterContent := renderAPICounters(counterW-2, bottomH-2, chatAPI, prom)
	counterPanel := Panel("Counters", counterContent, counterW, bottomH)

	bottomRow := lipgloss.JoinHorizontal(lipgloss.Top, epPanel, counterPanel)

	return lipgloss.JoinVertical(lipgloss.Left, topRow, bottomRow)
}

func renderAPISummary(w, h int, s *collector.ChatAPISnapshot, tsHTTP, ts5XX *collector.TimeSeries) string {
	sparkW := (w - 16)
	if sparkW < 10 {
		sparkW = 10
	}

	rate2xxColor := ColorGreen
	rate4xxColor := ColorYellow
	rate5xxColor := ColorRed

	lines := []string{
		LabelStyle.Render("HTTP Rate: ") + ValueStyle.Render(FormatRate(s.TotalHTTPRate)),
		"",
		LabelStyle.Render("  2XX: ") + lipgloss.NewStyle().Foreground(rate2xxColor).Bold(true).Render(FormatRate(s.Rate2XX)) +
			LabelStyle.Render("  4XX: ") + lipgloss.NewStyle().Foreground(rate4xxColor).Bold(true).Render(FormatRate(s.Rate4XX)) +
			LabelStyle.Render("  5XX: ") + lipgloss.NewStyle().Foreground(rate5xxColor).Bold(true).Render(FormatRate(s.Rate5XX)),
		"",
		LabelStyle.Render("Rate  ") + SparklineStr(tsHTTP.Values(), sparkW),
		LabelStyle.Render("5XX   ") + sparklineRed(ts5XX.Values(), sparkW),
	}

	// Success ratio
	if s.TotalHTTPRate > 0 {
		successPct := s.Rate2XX / s.TotalHTTPRate * 100
		lines = append(lines, "")
		lines = append(lines, LabelStyle.Render("Success: ")+
			ProgressBar(successPct, w-12)+
			lipgloss.NewStyle().Foreground(ColorGreen).Bold(true).Render(fmt.Sprintf(" %.1f%%", successPct)))
	}

	return strings.Join(lines, "\n")
}

func renderAPIOperations(w, h int, s *collector.ChatAPISnapshot, prom *collector.PrometheusSnapshot) string {
	var lines []string

	// API requests
	apiFailColor := rateColor(s.APIFailRate, 0.1, 1)
	lines = append(lines,
		lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render("API")+
			LabelStyle.Render("  Total:")+ValueStyle.Render(FormatRate(s.APIRequestRate))+
			LabelStyle.Render("  OK:")+lipgloss.NewStyle().Foreground(ColorGreen).Bold(true).Render(FormatRate(s.APISuccessRate))+
			LabelStyle.Render("  Fail:")+lipgloss.NewStyle().Foreground(apiFailColor).Bold(true).Render(FormatRate(s.APIFailRate)))

	// gRPC requests
	grpcFailColor := rateColor(s.GRPCFailRate, 0.1, 1)
	lines = append(lines,
		lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render("gRPC")+
			LabelStyle.Render(" Total:")+ValueStyle.Render(FormatRate(s.GRPCRequestRate))+
			LabelStyle.Render("  OK:")+lipgloss.NewStyle().Foreground(ColorGreen).Bold(true).Render(FormatRate(s.GRPCSuccessRate))+
			LabelStyle.Render("  Fail:")+lipgloss.NewStyle().Foreground(grpcFailColor).Bold(true).Render(FormatRate(s.GRPCFailRate)))

	lines = append(lines, "")

	// Auth
	lines = append(lines,
		lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render("Auth")+
			LabelStyle.Render(" Login:")+ValueStyle.Render(FormatRate(getUserLogin(s, prom)))+
			LabelStyle.Render("  Register:")+ValueStyle.Render(FormatRate(getUserRegister(s, prom))))

	// Message send
	lines = append(lines,
		lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render("Msg ")+
			LabelStyle.Render(" Send:")+ValueStyle.Render(FormatRate(s.SendMsgRate))+
			LabelStyle.Render("  Recv:")+ValueStyle.Render(FormatRate(s.MsgRecvTotalRate)))

	lines = append(lines, "")

	// Push summary
	lines = append(lines,
		lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render("Push")+
			LabelStyle.Render(" Online:")+lipgloss.NewStyle().Foreground(ColorGreen).Bold(true).Render(FormatRate(s.OnlinePushOKRate))+
			LabelStyle.Render("  Offline:")+lipgloss.NewStyle().Foreground(ColorGreen).Bold(true).Render(FormatRate(s.OfflinePushOKRate)))

	// Conversation push
	convFailColor := rateColor(s.ConvPushFailRate, 0.1, 1)
	lines = append(lines,
		lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render("Conv")+
			LabelStyle.Render(" PushOK:")+lipgloss.NewStyle().Foreground(ColorGreen).Bold(true).Render(FormatRate(s.ConvPushOKRate))+
			LabelStyle.Render("  PushFail:")+lipgloss.NewStyle().Foreground(convFailColor).Bold(true).Render(FormatRate(s.ConvPushFailRate)))

	return strings.Join(lines, "\n")
}

func renderHTTPEndpoints(w, h int, endpoints []collector.HTTPEndpointMetric, scrollPos int) string {
	if len(endpoints) == 0 {
		return LabelStyle.Render("No HTTP endpoint data")
	}

	pathW := w - 52
	if pathW < 15 {
		pathW = 15
	}

	header := fmt.Sprintf("%-6s %-*s %8s %8s %8s %8s",
		"METHOD", pathW, "PATH", "TOTAL", "2XX", "4XX", "5XX")
	headerStyled := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render(header)
	sep := lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", w))

	var lines []string
	lines = append(lines, headerStyled, sep)

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
		path := Truncate(ep.Path, pathW)
		method := lipgloss.NewStyle().Foreground(ColorCyan).Render(fmt.Sprintf("%-6s", ep.Method))

		totalStr := formatEndpointRate(ep.Total)
		r2xxStr := lipgloss.NewStyle().Foreground(ColorGreen).Render(formatEndpointRate(ep.Rate2XX))
		r4xxStr := lipgloss.NewStyle().Foreground(rateColor(ep.Rate4XX, 0.01, 0.1)).Render(formatEndpointRate(ep.Rate4XX))
		r5xxStr := lipgloss.NewStyle().Foreground(rateColor(ep.Rate5XX, 0.01, 0.1)).Render(formatEndpointRate(ep.Rate5XX))

		row := fmt.Sprintf("%s %-*s %8s %8s %8s %8s",
			method, pathW, path, totalStr, r2xxStr, r4xxStr, r5xxStr)
		lines = append(lines, row)
	}

	if len(endpoints) > maxRows {
		scrollInfo := lipgloss.NewStyle().Foreground(ColorSubtext).Render(
			fmt.Sprintf("  showing %d-%d of %d (j/k to scroll)", startIdx+1, endIdx, len(endpoints)))
		lines = append(lines, scrollInfo)
	}

	return strings.Join(lines, "\n")
}

func renderAPICounters(w, h int, s *collector.ChatAPISnapshot, prom *collector.PrometheusSnapshot) string {
	var lines []string

	// Seq operations
	lines = append(lines, lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render("Seq"))
	lines = append(lines, counterLine("  Get", s.SeqGetOKRate, s.SeqGetFailRate, w))
	lines = append(lines, counterLine("  Set", s.SeqSetOKRate, getSeqSetFail(prom), w))
	lines = append(lines, "")

	// Redis operations
	lines = append(lines, lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render("Redis"))
	lines = append(lines, counterLine("  Insert", getRedisInsOK(prom), getRedisInsFail(prom), w))
	lines = append(lines, counterLine("  Pull", s.MsgPullRedisOKRate, s.MsgPullRedisFailRate, w))
	lines = append(lines, "")

	// Mongo operations
	lines = append(lines, lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render("Mongo"))
	lines = append(lines, counterLine("  Insert", getMongoInsOK(prom), getMongoInsFail(prom), w))
	lines = append(lines, counterLine("  Pull", s.MsgPullMongoOKRate, s.MsgPullMongoFailRate, w))
	lines = append(lines, "")

	// Message processing
	lines = append(lines, lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render("Process"))
	lines = append(lines, counterLine("  1v1", getSingleOK(prom), getSingleFail(prom), w))
	lines = append(lines, counterLine("  Group", getGroupOK(prom), getGroupFail(prom), w))
	lines = append(lines, counterLine("  Super", s.SuperGroupProcOKRate, s.SuperGroupProcFailRate, w))
	lines = append(lines, "")

	// WebSocket recv
	lines = append(lines, lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render("WS Recv"))
	lines = append(lines,
		LabelStyle.Render("  1v1:")+ValueStyle.Render(FormatRate(s.SingleChatRecvRate))+
			LabelStyle.Render(" Grp:")+ValueStyle.Render(FormatRate(s.GroupChatRecvRate)))
	lines = append(lines,
		LabelStyle.Render("  Seq:")+ValueStyle.Render(FormatRate(s.NewestSeqTotalRate))+
			LabelStyle.Render(" Pull:")+ValueStyle.Render(FormatRate(s.PullBySeqListRate)))

	return strings.Join(lines, "\n")
}

// counterLine formats "  Label  OK: X/s  Fail: Y/s"
func counterLine(label string, okRate, failRate float64, _ int) string {
	failColor := rateColor(failRate, 0.01, 0.1)
	return LabelStyle.Render(label) +
		LabelStyle.Render(" OK:") + lipgloss.NewStyle().Foreground(ColorGreen).Bold(true).Render(FormatRate(okRate)) +
		LabelStyle.Render(" Fail:") + lipgloss.NewStyle().Foreground(failColor).Bold(true).Render(FormatRate(failRate))
}

func formatEndpointRate(r float64) string {
	if r < 0.01 {
		return "  --"
	}
	if r >= 1000 {
		return fmt.Sprintf("%.1fk", r/1000)
	}
	if r >= 1 {
		return fmt.Sprintf("%.1f", r)
	}
	return fmt.Sprintf("%.2f", r)
}

// rateColor returns green for zero, yellow for warn, red for crit.
func rateColor(val, warnThreshold, critThreshold float64) lipgloss.Color {
	switch {
	case val >= critThreshold:
		return ColorRed
	case val >= warnThreshold:
		return ColorYellow
	default:
		return ColorGreen
	}
}

// sparklineRed renders a red-tinted sparkline (for error rates).
func sparklineRed(values []float64, width int) string {
	if len(values) == 0 {
		return lipgloss.NewStyle().Foreground(ColorSubtext).Render("─")
	}

	blocks := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

	start := 0
	if len(values) > width {
		start = len(values) - width
	}
	vals := values[start:]

	min, max := vals[0], vals[0]
	for _, v := range vals {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}

	result := ""
	rng := max - min
	for _, v := range vals {
		idx := 0
		if rng > 0 {
			idx = int((v - min) / rng * float64(len(blocks)-1))
		}
		if idx >= len(blocks) {
			idx = len(blocks) - 1
		}
		if idx < 0 {
			idx = 0
		}

		var color lipgloss.Color
		ratio := float64(idx) / float64(len(blocks)-1)
		switch {
		case ratio > 0.75:
			color = ColorRed
		case ratio > 0.5:
			color = ColorOrange
		case ratio > 0.25:
			color = lipgloss.Color("#8b4040")
		default:
			color = lipgloss.Color("#5a3030")
		}
		result += lipgloss.NewStyle().Foreground(color).Render(string(blocks[idx]))
	}

	return result
}

// Helper functions to safely extract values from PrometheusSnapshot.
func getUserLogin(s *collector.ChatAPISnapshot, p *collector.PrometheusSnapshot) float64 {
	if p != nil && p.UserLogin > 0 {
		return p.UserLogin
	}
	return 0
}

func getUserRegister(s *collector.ChatAPISnapshot, p *collector.PrometheusSnapshot) float64 {
	if p != nil && p.UserRegister > 0 {
		return p.UserRegister
	}
	return 0
}

func getSeqSetFail(p *collector.PrometheusSnapshot) float64 {
	if p != nil {
		return p.SeqSetFail
	}
	return 0
}

func getRedisInsOK(p *collector.PrometheusSnapshot) float64 {
	if p != nil {
		return p.RedisInsertOK
	}
	return 0
}

func getRedisInsFail(p *collector.PrometheusSnapshot) float64 {
	if p != nil {
		return p.RedisInsertFail
	}
	return 0
}

func getMongoInsOK(p *collector.PrometheusSnapshot) float64 {
	if p != nil {
		return p.MongoInsertOK
	}
	return 0
}

func getMongoInsFail(p *collector.PrometheusSnapshot) float64 {
	if p != nil {
		return p.MongoInsertFail
	}
	return 0
}

func getSingleOK(p *collector.PrometheusSnapshot) float64 {
	if p != nil {
		return p.SingleChatOK
	}
	return 0
}

func getSingleFail(p *collector.PrometheusSnapshot) float64 {
	if p != nil {
		return p.SingleChatFail
	}
	return 0
}

func getGroupOK(p *collector.PrometheusSnapshot) float64 {
	if p != nil {
		return p.GroupChatOK
	}
	return 0
}

func getGroupFail(p *collector.PrometheusSnapshot) float64 {
	if p != nil {
		return p.GroupChatFail
	}
	return 0
}
