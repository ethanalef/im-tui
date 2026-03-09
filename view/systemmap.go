package view

import (
	"fmt"
	"strings"

	"im-tui/alert"
	"im-tui/collector"

	"github.com/charmbracelet/lipgloss"
)

// serviceGroup aggregates pods by service name.
type serviceGroup struct {
	Name     string
	Pods     []collector.PodInfo
	Running  int
	Total    int
	Restarts int
}

func (sg serviceGroup) healthColor() lipgloss.Color {
	if sg.Total == 0 {
		return ColorBorder
	}
	if sg.Running < sg.Total {
		return ColorRed
	}
	if sg.Restarts > 0 {
		return ColorYellow
	}
	return ColorGreen
}

func (sg serviceGroup) dot() string {
	return lipgloss.NewStyle().Foreground(sg.healthColor()).Render("●")
}

func serviceName(podName string) string {
	parts := strings.Split(podName, "-")
	if len(parts) <= 2 {
		return podName
	}
	candidate := strings.Join(parts[:len(parts)-2], "-")
	if candidate == "" {
		return podName
	}
	return candidate
}

func groupPods(pods []collector.PodInfo) map[string]*serviceGroup {
	groups := make(map[string]*serviceGroup)
	for _, p := range pods {
		name := serviceName(p.Name)
		sg, ok := groups[name]
		if !ok {
			sg = &serviceGroup{Name: name}
			groups[name] = sg
		}
		sg.Pods = append(sg.Pods, p)
		sg.Total++
		if p.Status == "Running" {
			sg.Running++
		}
		sg.Restarts += p.Restarts
	}
	return groups
}

func findHPA(name string, hpas []collector.HPAInfo) *collector.HPAInfo {
	for _, h := range hpas {
		if h.Name == name {
			return &h
		}
	}
	return nil
}

func hasAlertFor(ev *alert.Evaluator, keyword string) (bool, alert.Level) {
	if ev == nil {
		return false, ""
	}
	for _, a := range ev.Active() {
		if strings.Contains(strings.ToLower(a.Metric), strings.ToLower(keyword)) {
			return true, a.Level
		}
	}
	return false, ""
}

func sysBox(lines []string, boxWidth int) []string {
	bdr := lipgloss.NewStyle().Foreground(ColorBorder)
	inner := boxWidth - 2
	if inner < 1 {
		inner = 1
	}
	var out []string
	out = append(out, bdr.Render("┌"+strings.Repeat("─", inner)+"┐"))
	for _, l := range lines {
		w := lipgloss.Width(l)
		pad := inner - w
		if pad < 0 {
			pad = 0
		}
		out = append(out, bdr.Render("│")+l+strings.Repeat(" ", pad)+bdr.Render("│"))
	}
	out = append(out, bdr.Render("└"+strings.Repeat("─", inner)+"┘"))
	return out
}

func sysArrow(length int, style string) string {
	if length < 2 {
		length = 2
	}
	var s lipgloss.Style
	body := ""
	head := "▸"
	switch style {
	case "bold":
		s = lipgloss.NewStyle().Foreground(ColorGreen)
		body = strings.Repeat("━", length-1)
	case "error":
		s = lipgloss.NewStyle().Foreground(ColorRed).Bold(true)
		body = strings.Repeat("━", length-1)
	case "dashed":
		s = lipgloss.NewStyle().Foreground(ColorBorder)
		body = strings.Repeat("╌", length-1)
	default:
		s = lipgloss.NewStyle().Foreground(ColorSubtext)
		body = strings.Repeat("─", length-1)
	}
	return s.Render(body + head)
}

// arrowSpec describes an arrow between two columns in a tier.
type arrowSpec struct {
	show  bool
	style string
}

// composeTier renders one horizontal band of up to 4 boxes with arrows between them.
// Arrows are placed at the vertical midpoint of the tallest box in the tier.
func composeTier(
	boxes [4][]string,
	colWidths [4]int,
	arrows [3]arrowSpec,
	arrowW int,
) []string {
	maxH := 0
	for _, b := range boxes {
		if len(b) > maxH {
			maxH = len(b)
		}
	}
	if maxH == 0 {
		return nil
	}

	// Pad each box to maxH
	padded := boxes
	for i := range padded {
		for len(padded[i]) < maxH {
			padded[i] = append(padded[i], "")
		}
	}

	mid := maxH / 2

	var lines []string
	for row := 0; row < maxH; row++ {
		line := " " // left margin
		for col := 0; col < 4; col++ {
			line += PadRight(padded[col][row], colWidths[col])
			if col < 3 {
				if row == mid && arrows[col].show {
					line += sysArrow(arrowW, arrows[col].style)
				} else {
					line += strings.Repeat(" ", arrowW)
				}
			}
		}
		lines = append(lines, line)
	}
	return lines
}

// metricsAnnotation builds a line of small metric labels positioned under each arrow gap.
func metricsAnnotation(colWidths [4]int, arrowW int, labels [3]string) string {
	style := lipgloss.NewStyle().Foreground(ColorCyan)

	// Gap start positions (visual column)
	gapStart := [3]int{
		1 + colWidths[0],
		1 + colWidths[0] + arrowW + colWidths[1],
		1 + colWidths[0] + arrowW + colWidths[1] + arrowW + colWidths[2],
	}

	line := ""
	pos := 0
	for i := 0; i < 3; i++ {
		if labels[i] == "" {
			continue
		}
		styled := style.Render(labels[i])
		w := lipgloss.Width(styled)
		target := gapStart[i]
		if target < pos {
			target = pos + 1
		}
		if target > pos {
			line += strings.Repeat(" ", target-pos)
		}
		line += styled
		pos = target + w
	}
	return line
}

func alertDot(ev *alert.Evaluator, keyword string, normalDotFn func(...string) string) string {
	if has, lvl := hasAlertFor(ev, keyword); has {
		if lvl == alert.LevelCritical {
			return lipgloss.NewStyle().Foreground(ColorRed).Bold(true).Render("●")
		}
		return lipgloss.NewStyle().Foreground(ColorYellow).Render("●")
	}
	return normalDotFn("●")
}

// RenderSystemMap renders the 4-column system topology diagram.
func RenderSystemMap(
	width, height int,
	k8s *collector.KubernetesSnapshot,
	prom *collector.PrometheusSnapshot,
	cw *collector.CloudWatchSnapshot,
	locust *collector.LocustSnapshot,
	ev *alert.Evaluator,
) string {
	if width < 80 {
		msg := lipgloss.NewStyle().Foreground(ColorYellow).Render(
			fmt.Sprintf("Terminal too narrow for system map (need 80+ cols, have %d)", width))
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, msg)
	}

	compact := width < 120

	// Column widths
	var extW, gwW, svcW, stW, arrowW int
	if compact {
		extW, gwW, svcW, stW, arrowW = 13, 15, 14, 16, 3
	} else {
		extW, gwW, svcW, stW, arrowW = 15, 18, 16, 20, 5
	}
	colWidths := [4]int{extW, gwW, svcW, stW}

	// Gather service groups
	var groups map[string]*serviceGroup
	var hpas []collector.HPAInfo
	if k8s != nil && k8s.Err == nil {
		groups = groupPods(k8s.Pods)
		hpas = k8s.HPAs
	} else {
		groups = make(map[string]*serviceGroup)
	}

	svc := func(name string) *serviceGroup {
		if sg, ok := groups[name]; ok {
			return sg
		}
		if sg, ok := groups["openim-"+name]; ok {
			return sg
		}
		return &serviceGroup{Name: name}
	}

	// Style helpers
	greenDot := lipgloss.NewStyle().Foreground(ColorGreen).Render
	dimDot := lipgloss.NewStyle().Foreground(ColorBorder).Render
	lbl := lipgloss.NewStyle().Foreground(ColorSubtext).Render
	v := lipgloss.NewStyle().Foreground(ColorText).Bold(true).Render

	// ─── Build boxes ───────────────────────────────────────────

	// ALB
	var albBox []string
	if cw != nil && cw.Err == nil {
		d := alertDot(ev, "ALB", greenDot)
		p99 := cw.ALB.ResponseTimeP99 * 1000
		c := []string{
			fmt.Sprintf(" %s %s", d, v("ALB")),
			fmt.Sprintf(" %s%s", lbl("P99: "), v(fmt.Sprintf("%.0fms", p99))),
			fmt.Sprintf(" %s%s", lbl("5XX: "), fmtCount5XX(cw.ALB.Count5XX)),
		}
		if !compact {
			c = append(c, fmt.Sprintf(" %s%s", lbl("conn:"), v(fmt.Sprintf("%.0f", cw.ALB.ActiveConns))))
		}
		albBox = sysBox(c, extW)
	} else {
		albBox = sysBox([]string{
			fmt.Sprintf(" %s %s", dimDot("●"), lbl("ALB")),
			" " + lbl("no data"),
		}, extW)
	}

	// Locust
	var locustBox []string
	if locust != nil && locust.Available && locust.Err == nil {
		d := alertDot(ev, "Locust", greenDot)
		c := []string{
			fmt.Sprintf(" %s %s", d, v("Locust")),
			fmt.Sprintf(" %s%s", v(fmt.Sprintf("%.0f", locust.TotalRPS)), lbl(" RPS")),
			fmt.Sprintf(" %s%s", lbl("fail:"), v(fmt.Sprintf("%.1f%%", locust.FailRatio*100))),
		}
		if !compact {
			c = append(c, fmt.Sprintf(" %s%s", lbl("users:"), v(fmt.Sprintf("%d", locust.UserCount))))
		}
		locustBox = sysBox(c, extW)
	} else {
		locustBox = sysBox([]string{
			fmt.Sprintf(" %s %s", dimDot("●"), lbl("Locust")),
			" " + lbl("inactive"),
		}, extW)
	}

	// msg-gateway
	gw := svc("msg-gateway")
	gwContent := []string{fmt.Sprintf(" %s %s", gw.dot(), v("msg-gw"))}
	if gw.Total > 0 {
		gwContent = append(gwContent, fmt.Sprintf(" %s", lbl(fmt.Sprintf("%d/%d pods", gw.Running, gw.Total))))
		if prom != nil && prom.Err == nil {
			gwContent = append(gwContent, fmt.Sprintf(" %s%s", lbl("online:"), v(fmt.Sprintf("%.0f", prom.OnlineUsers))))
		}
		if h := findHPA("msg-gateway", hpas); h != nil {
			gwContent = append(gwContent, fmt.Sprintf(" %s", lbl(fmt.Sprintf("HPA %d/%d..%d", h.Current, h.MinReplicas, h.MaxReplicas))))
		}
		if prom != nil && prom.Err == nil && prom.GatewaySendRate > 0 {
			gwContent = append(gwContent, fmt.Sprintf(" %s%s", lbl("recv:"), v(FormatRate(prom.GatewaySendRate))))
		}
	} else {
		gwContent = append(gwContent, " "+lbl("no pods"))
	}
	gwBox := sysBox(gwContent, gwW)

	// api
	api := svc("api")
	apiContent := []string{fmt.Sprintf(" %s %s", api.dot(), v("api"))}
	if api.Total > 0 {
		apiContent = append(apiContent, fmt.Sprintf(" %s  %s", lbl("OK"), v(fmt.Sprintf("%d/%d", api.Running, api.Total))))
		if api.Restarts > 0 {
			apiContent = append(apiContent, " "+lipgloss.NewStyle().Foreground(ColorYellow).Render(fmt.Sprintf("%d restarts", api.Restarts)))
		}
	} else {
		apiContent = append(apiContent, " "+lbl("no pods"))
	}
	apiBox := sysBox(apiContent, gwW)

	// msg
	msg := svc("msg")
	msgContent := []string{fmt.Sprintf(" %s %s", msg.dot(), v("msg"))}
	if msg.Total > 0 {
		msgContent = append(msgContent, fmt.Sprintf(" %s  %s", lbl("OK"), v(fmt.Sprintf("%d/%d", msg.Running, msg.Total))))
		if msg.Restarts > 0 {
			msgContent = append(msgContent, " "+lipgloss.NewStyle().Foreground(ColorYellow).Render(fmt.Sprintf("%d restarts", msg.Restarts)))
		}
	} else {
		msgContent = append(msgContent, " "+lbl("no pods"))
	}
	msgBox := sysBox(msgContent, svcW)

	// transfer
	tr := svc("msg-transfer")
	trContent := []string{fmt.Sprintf(" %s %s", tr.dot(), v("transfer"))}
	if tr.Total > 0 {
		trContent = append(trContent, fmt.Sprintf(" %s  %s", lbl("OK"), v(fmt.Sprintf("%d/%d", tr.Running, tr.Total))))
		if prom != nil && prom.Err == nil && (prom.MongoInsertOK > 0 || prom.RedisInsertOK > 0) {
			if compact {
				trContent = append(trContent, fmt.Sprintf(" %s%s", lbl("w:"), v(FormatRate(prom.MongoInsertOK+prom.RedisInsertOK))))
			} else {
				trContent = append(trContent, fmt.Sprintf(" %s%s", lbl("mgo:"), v(FormatRate(prom.MongoInsertOK))))
				trContent = append(trContent, fmt.Sprintf(" %s%s", lbl("rds:"), v(FormatRate(prom.RedisInsertOK))))
			}
		}
	} else {
		trContent = append(trContent, " "+lbl("no pods"))
	}
	trBox := sysBox(trContent, svcW)

	// push
	push := svc("push")
	pushContent := []string{fmt.Sprintf(" %s %s", push.dot(), v("push"))}
	if push.Total > 0 {
		pushContent = append(pushContent, fmt.Sprintf(" %s  %s", lbl("OK"), v(fmt.Sprintf("%d/%d", push.Running, push.Total))))
		if prom != nil && prom.Err == nil && prom.LongTimePush > 0 {
			pushContent = append(pushContent, " "+lipgloss.NewStyle().Foreground(ColorYellow).Render(fmt.Sprintf("slow:%.1f/s", prom.LongTimePush)))
		}
	} else {
		pushContent = append(pushContent, " "+lbl("no pods"))
	}
	pushBox := sysBox(pushContent, svcW)

	// RPC services (compact: 2 per line)
	rpcNames := []string{"auth", "user", "friend", "group", "conversation", "third"}
	var rpcContent []string
	if compact {
		for i := 0; i < len(rpcNames); i += 2 {
			sg1 := svc(rpcNames[i])
			d1 := rpcNames[i]
			if d1 == "conversation" {
				d1 = "conv."
			}
			line := fmt.Sprintf(" %s%s", sg1.dot(), lbl(d1))
			if i+1 < len(rpcNames) {
				sg2 := svc(rpcNames[i+1])
				d2 := rpcNames[i+1]
				if d2 == "conversation" {
					d2 = "conv."
				}
				line += fmt.Sprintf(" %s%s", sg2.dot(), lbl(d2))
			}
			rpcContent = append(rpcContent, line)
		}
	} else {
		for _, name := range rpcNames {
			sg := svc(name)
			display := name
			if name == "conversation" {
				display = "convers."
			}
			status := ""
			if sg.Total > 0 {
				status = lbl(fmt.Sprintf(" %d/%d", sg.Running, sg.Total))
			}
			rpcContent = append(rpcContent, fmt.Sprintf(" %s %s%s", sg.dot(), lbl(display), status))
		}
	}
	rpcBox := sysBox(rpcContent, svcW)

	// Redis
	barW := stW - 4
	if barW < 5 {
		barW = 5
	}
	var redisBox []string
	if cw != nil && cw.Err == nil && len(cw.Redis) > 0 {
		avgCPU, totalConn, avgMem := 0.0, 0.0, 0.0
		for _, r := range cw.Redis {
			avgCPU += r.CPUPercent
			totalConn += r.Connections
			avgMem += r.MemoryPercent
		}
		n := float64(len(cw.Redis))
		avgCPU /= n
		avgMem /= n
		d := alertDot(ev, "Redis", greenDot)
		rc := []string{
			fmt.Sprintf(" %s %s %s", d, v("Redis"), v(fmt.Sprintf("%.0f%%", avgCPU))),
			" " + ProgressBar(avgCPU, barW),
			fmt.Sprintf(" %s %s", lbl(fmt.Sprintf("%d nodes", len(cw.Redis))), v(fmt.Sprintf("%.0fc", totalConn))),
		}
		if !compact {
			rc = append(rc, fmt.Sprintf(" %s%s %s%s",
				lbl("mem:"), v(fmt.Sprintf("%.0f%%", avgMem)),
				lbl("hit:"), v(fmt.Sprintf("%.0f%%", cw.Redis[0].HitRate))))
		}
		redisBox = sysBox(rc, stW)
	} else {
		redisBox = sysBox([]string{
			fmt.Sprintf(" %s %s", dimDot("●"), lbl("Redis")),
			" " + lbl("no data"),
		}, stW)
	}

	// DocDB
	var docdbBox []string
	if cw != nil && cw.Err == nil {
		d := alertDot(ev, "DocDB", greenDot)
		dc := []string{
			fmt.Sprintf(" %s %s %s", d, v("DocDB"), v(fmt.Sprintf("%.0f%%", cw.DocDB.CPUPercent))),
			" " + ProgressBar(cw.DocDB.CPUPercent, barW),
			fmt.Sprintf(" %s", v(fmt.Sprintf("%.0f conn", cw.DocDB.Connections))),
		}
		if !compact {
			dc = append(dc, fmt.Sprintf(" %s%s %s%s",
				lbl("ins:"), v(fmt.Sprintf("%.0f", cw.DocDB.InsertOps)),
				lbl("qry:"), v(fmt.Sprintf("%.0f", cw.DocDB.QueryOps))))
		}
		docdbBox = sysBox(dc, stW)
	} else {
		docdbBox = sysBox([]string{
			fmt.Sprintf(" %s %s", dimDot("●"), lbl("DocDB")),
			" " + lbl("no data"),
		}, stW)
	}

	// MySQL
	var mysqlBox []string
	if cw != nil && cw.Err == nil {
		d := alertDot(ev, "RDS", greenDot)
		totalIOPS := cw.RDS.ReadIOPS + cw.RDS.WriteIOPS
		mc := []string{
			fmt.Sprintf(" %s %s %s", d, v("MySQL"), v(fmt.Sprintf("%.0f%%", cw.RDS.CPUPercent))),
			" " + ProgressBar(cw.RDS.CPUPercent, barW),
			fmt.Sprintf(" %s %s", v(fmt.Sprintf("%.0fc", cw.RDS.Connections)), lbl(fmt.Sprintf("%.0f iops", totalIOPS))),
		}
		if !compact {
			mc = append(mc, fmt.Sprintf(" %s%s %s%s",
				lbl("r:"), v(fmt.Sprintf("%.0f", cw.RDS.ReadIOPS)),
				lbl("w:"), v(fmt.Sprintf("%.0f", cw.RDS.WriteIOPS))))
		}
		mysqlBox = sysBox(mc, stW)
	} else {
		mysqlBox = sysBox([]string{
			fmt.Sprintf(" %s %s", dimDot("●"), lbl("MySQL")),
			" " + lbl("no data"),
		}, stW)
	}

	// Kafka queue (bridge between msg and transfer)
	kafkaColor := ColorGreen
	if has, lvl := hasAlertFor(ev, "Kafka"); has {
		if lvl == alert.LevelCritical {
			kafkaColor = ColorRed
		} else {
			kafkaColor = ColorYellow
		}
	}
	var kafkaBox []string
	if cw != nil && cw.Err == nil && len(cw.MSK.ConsumerLag) > 0 {
		kDot := lipgloss.NewStyle().Foreground(kafkaColor).Render("●")
		kc := []string{fmt.Sprintf(" %s %s", kDot, v("Kafka"))}
		if compact {
			kc = append(kc, fmt.Sprintf(" %s%s", lbl("lag:"), fmtLagStyled(cw.MSK.TotalLag, kafkaColor)))
		} else {
			for _, cl := range cw.MSK.ConsumerLag {
				kc = append(kc, fmt.Sprintf(" %s%s", lbl(fmt.Sprintf("%-7s", cl.Group+":")), fmtLagStyled(cl.Lag, kafkaColor)))
			}
		}
		if prom != nil && prom.Err == nil {
			rate := prom.MsgLagGrowthRate
			var rateStr string
			switch {
			case rate > 0.5:
				rateStr = lipgloss.NewStyle().Foreground(ColorRed).Render(fmt.Sprintf(" ▲+%.0f/s", rate))
			case rate < -0.5:
				rateStr = lipgloss.NewStyle().Foreground(ColorGreen).Render(fmt.Sprintf(" ▼%.0f/s", rate))
			default:
				rateStr = lipgloss.NewStyle().Foreground(ColorGreen).Render(" = steady")
			}
			kc = append(kc, rateStr)
		}
		kafkaBox = sysBox(kc, svcW)
	} else {
		kafkaBox = sysBox([]string{
			fmt.Sprintf(" %s %s", dimDot("●"), lbl("Kafka")),
			" " + lbl("no data"),
		}, svcW)
	}

	// ─── Arrow styles ──────────────────────────────────────────

	extToGwStyle := "normal"
	if cw != nil && cw.Err == nil {
		if cw.ALB.Count5XX > 0 {
			extToGwStyle = "error"
		} else if cw.ALB.RequestCount > 0 {
			extToGwStyle = "bold"
		}
	}

	gwToSvcStyle := "normal"
	if k8s != nil && k8s.Err == nil {
		allOK := true
		for _, name := range []string{"msg", "msg-transfer", "push"} {
			sg := svc(name)
			if sg.Total > 0 && sg.Running < sg.Total {
				allOK = false
			}
		}
		if allOK && len(groups) > 0 {
			gwToSvcStyle = "bold"
		}
	}

	svcToStStyle := "normal"
	if cw != nil && cw.Err == nil {
		h1, _ := hasAlertFor(ev, "DocDB")
		h2, _ := hasAlertFor(ev, "RDS")
		h3, _ := hasAlertFor(ev, "Redis")
		if h1 || h2 || h3 {
			svcToStStyle = "error"
		} else {
			svcToStStyle = "bold"
		}
	}

	locToGwStyle := "dashed"
	if locust != nil && locust.Available && locust.Err == nil {
		if locust.FailRatio > 0.05 {
			locToGwStyle = "error"
		} else if locust.TotalRPS > 0 {
			locToGwStyle = "bold"
		}
	}

	// ─── Compose tiers ─────────────────────────────────────────

	empty := []string{}

	// Tier 1: ALB → msg-gw → msg → Redis
	tier1 := composeTier(
		[4][]string{albBox, gwBox, msgBox, redisBox},
		colWidths,
		[3]arrowSpec{
			{true, extToGwStyle},
			{true, gwToSvcStyle},
			{true, svcToStStyle},
		},
		arrowW,
	)

	// Metric annotations between tier 1 and 2
	var m1 [3]string
	if cw != nil && cw.Err == nil && cw.ALB.RequestCount > 0 {
		m1[0] = fmt.Sprintf("%.0f req", cw.ALB.RequestCount)
	}
	if prom != nil && prom.Err == nil && prom.GatewaySendRate > 0 {
		m1[1] = FormatRate(prom.GatewaySendRate)
	}
	if prom != nil && prom.Err == nil && prom.RedisInsertOK > 0 {
		m1[2] = FormatRate(prom.RedisInsertOK) + " wr"
	}
	ml1 := metricsAnnotation(colWidths, arrowW, m1)

	// Kafka bridge: vertical connector and Kafka box in services column
	col2Center := 1 + extW + arrowW + gwW + arrowW + svcW/2
	kafkaVLine := strings.Repeat(" ", col2Center) + lipgloss.NewStyle().Foreground(kafkaColor).Render("│")
	kafkaTier := composeTier(
		[4][]string{empty, empty, kafkaBox, empty},
		colWidths,
		[3]arrowSpec{{false, ""}, {false, ""}, {false, ""}},
		arrowW,
	)

	// Tier 2: Locust → api → transfer → DocDB
	tier2 := composeTier(
		[4][]string{locustBox, apiBox, trBox, docdbBox},
		colWidths,
		[3]arrowSpec{
			{true, locToGwStyle},
			{true, gwToSvcStyle},
			{true, svcToStStyle},
		},
		arrowW,
	)

	// Metric annotations between tier 2 and 3
	var m2 [3]string
	if cw != nil && cw.Err == nil {
		ops := cw.DocDB.InsertOps + cw.DocDB.QueryOps
		if ops > 0 {
			m2[2] = fmt.Sprintf("%.0f ops/m", ops)
		} else if cw.DocDB.Connections > 0 {
			m2[2] = fmt.Sprintf("%.0f conn", cw.DocDB.Connections)
		}
	}
	ml2 := metricsAnnotation(colWidths, arrowW, m2)

	// Tier 3: (empty) → (empty) → push → MySQL
	tier3 := composeTier(
		[4][]string{empty, empty, pushBox, mysqlBox},
		colWidths,
		[3]arrowSpec{
			{false, ""},
			{false, ""},
			{true, svcToStStyle},
		},
		arrowW,
	)

	// Tier 4: (empty) → (empty) → rpc → (empty)
	tier4 := composeTier(
		[4][]string{empty, empty, rpcBox, empty},
		colWidths,
		[3]arrowSpec{{false, ""}, {false, ""}, {false, ""}},
		arrowW,
	)

	// ─── Assemble output ───────────────────────────────────────

	hdr := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
	headerLine := " " +
		PadRight(hdr.Render("EXTERNAL"), extW) + strings.Repeat(" ", arrowW) +
		PadRight(hdr.Render("GATEWAY"), gwW) + strings.Repeat(" ", arrowW) +
		PadRight(hdr.Render("SERVICES"), svcW) + strings.Repeat(" ", arrowW) +
		PadRight(hdr.Render("STORAGE"), stW)

	var output []string
	output = append(output, headerLine, "")
	output = append(output, tier1...)
	output = append(output, ml1)
	output = append(output, kafkaVLine)
	output = append(output, kafkaTier...)
	output = append(output, kafkaVLine)
	output = append(output, tier2...)
	output = append(output, ml2)
	if len(tier3) > 0 {
		output = append(output, tier3...)
	}
	output = append(output, "")
	if len(tier4) > 0 {
		output = append(output, tier4...)
	}

	// Legend
	output = append(output, "")
	gd := lipgloss.NewStyle().Foreground(ColorGreen).Render("●")
	yd := lipgloss.NewStyle().Foreground(ColorYellow).Render("●")
	rd := lipgloss.NewStyle().Foreground(ColorRed).Render("●")
	dd := lipgloss.NewStyle().Foreground(ColorBorder).Render("●")
	output = append(output,
		fmt.Sprintf(" %s Running  %s Degraded  %s Down  %s Unknown", gd, yd, rd, dd))
	output = append(output,
		fmt.Sprintf("  %s active  %s normal  %s idle  %s errors",
			lipgloss.NewStyle().Foreground(ColorGreen).Render("━━"),
			lipgloss.NewStyle().Foreground(ColorSubtext).Render("──"),
			lipgloss.NewStyle().Foreground(ColorBorder).Render("╌╌"),
			lipgloss.NewStyle().Foreground(ColorRed).Render("━━")))
	output = append(output,
		fmt.Sprintf("  %s queue OK  %s lag warning  %s lag critical",
			lipgloss.NewStyle().Foreground(ColorGreen).Render("│"),
			lipgloss.NewStyle().Foreground(ColorYellow).Render("│"),
			lipgloss.NewStyle().Foreground(ColorRed).Render("│")))

	content := strings.Join(output, "\n")
	return Panel("System Topology", content, width, height)
}

func fmtLag(n float64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", n/1_000_000)
	case n >= 10_000:
		return fmt.Sprintf("%.0fk", n/1000)
	default:
		return fmt.Sprintf("%.0f", n)
	}
}

// fmtLagStyled renders a lag number: green if zero, kafkaColor if non-zero.
func fmtLagStyled(lag float64, kafkaColor lipgloss.Color) string {
	s := fmtLag(lag)
	if lag == 0 {
		return lipgloss.NewStyle().Foreground(ColorGreen).Bold(true).Render(s)
	}
	return lipgloss.NewStyle().Foreground(kafkaColor).Bold(true).Render(s)
}

func fmtCount5XX(count float64) string {
	if count == 0 {
		return lipgloss.NewStyle().Foreground(ColorGreen).Render("0")
	}
	return lipgloss.NewStyle().Foreground(ColorRed).Bold(true).Render(fmt.Sprintf("%.0f", count))
}
