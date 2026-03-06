package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"im-tui/alert"
	"im-tui/collector"
	"im-tui/export"
	"im-tui/model"
	"im-tui/portforward"
	"im-tui/view"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config.yaml")
	flag.Parse()

	cfg, err := LoadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Build model config
	mcfg := model.Config{
		Namespace:          cfg.Namespace,
		Environment:        cfg.Environment,
		Kubeconfig:         expandHome(cfg.Kubeconfig),
		SparklineCap:       cfg.SparklineCap,
		PrometheusURL:      cfg.Prometheus.URL, // may be empty; resolved below
		PrometheusInterval: cfg.Prometheus.Interval,
		CloudWatchRegion:   cfg.CloudWatch.Region,
		CloudWatchInterval: cfg.CloudWatch.Interval,
		KubernetesInterval: cfg.Kubernetes.Interval,
		LocustURL:          cfg.Locust.URL,
		LocustInterval:     cfg.Locust.Interval,
		LogInterval:        cfg.Logs.Interval,
	}

	m := model.NewModel(mcfg)

	// Initialize Prometheus: auto port-forward or direct URL
	var promPF *portforward.PortForward
	promURL := cfg.Prometheus.URL
	if promURL == "" && cfg.Prometheus.Service != "" {
		// Auto port-forward to Prometheus
		promPF = portforward.New(
			expandHome(cfg.Kubeconfig),
			cfg.Prometheus.Namespace,
			cfg.Prometheus.Service,
			cfg.Prometheus.Port,
			0, // auto-pick local port
		)
		fmt.Fprintf(os.Stderr, "Starting port-forward to %s/%s:%d...\n",
			cfg.Prometheus.Namespace, cfg.Prometheus.Service, cfg.Prometheus.Port)
		if err := promPF.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Prometheus port-forward failed: %v\n", err)
		} else {
			promURL = promPF.LocalURL()
			fmt.Fprintf(os.Stderr, "Prometheus available at %s\n", promURL)
		}
	}
	if promURL != "" {
		m.PromCollector = collector.NewPrometheusCollector(promURL)
	}

	var mskCGs []collector.MSKConsumerGroupConfig
	for _, cg := range cfg.AWS.MSK.ConsumerGroups {
		mskCGs = append(mskCGs, collector.MSKConsumerGroupConfig{
			Group: cg.Group,
			Topic: cg.Topic,
		})
	}
	cwCollector, err := collector.NewCloudWatchCollector(
		cfg.CloudWatch.Region,
		cfg.AWS.DocDB.ClusterID,
		cfg.AWS.DocDB.ClusterName,
		cfg.AWS.RDS.InstanceID,
		cfg.AWS.ElastiCache.Nodes,
		cfg.AWS.ALB.LoadBalancers,
		cfg.AWS.MSK.ClusterName,
		mskCGs,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: CloudWatch init failed: %v\n", err)
	} else {
		m.CWCollector = cwCollector
	}

	m.K8sCollector = collector.NewKubernetesCollector(
		expandHome(cfg.Kubeconfig),
		cfg.Namespace,
	)

	m.LocustCollector = collector.NewLocustCollector(cfg.Locust.URL)

	m.LogCollector = collector.NewLogCollector(
		expandHome(cfg.Kubeconfig),
		cfg.Namespace,
		cfg.Logs.Services,
		cfg.Logs.SinceSec,
	)

	// Initialize alert evaluator
	m.Evaluator = alert.NewEvaluator(alert.Thresholds{
		CPUWarn:          cfg.Thresholds.CPUWarn,
		CPUCrit:          cfg.Thresholds.CPUCrit,
		MemoryWarn:       cfg.Thresholds.MemoryWarn,
		Error5XXWarn:     cfg.Thresholds.Error5XXWarn,
		PodRestartCrit:   cfg.Thresholds.PodRestartCrit,
		LocustFailWarn:   cfg.Thresholds.LocustFailWarn,
		ResponseTimeWarn: cfg.Thresholds.ResponseTimeWarn,
		DocDBConnWarn:    cfg.Thresholds.DocDBConnWarn,
		DocDBConnCrit:    cfg.Thresholds.DocDBConnCrit,
		RDSLatencyWarnMs: cfg.Thresholds.RDSLatencyWarnMs,
		RDSLatencyCritMs: cfg.Thresholds.RDSLatencyCritMs,
		RDSDiskQueueWarn: cfg.Thresholds.RDSDiskQueueWarn,
		RDSDiskQueueCrit: cfg.Thresholds.RDSDiskQueueCrit,
		RedisEvictWarn:   cfg.Thresholds.RedisEvictWarn,
		RedisEvictCrit:   cfg.Thresholds.RedisEvictCrit,
		GoroutineWarn:    cfg.Thresholds.GoroutineWarn,
		GoroutineCrit:    cfg.Thresholds.GoroutineCrit,
		KafkaLagWarn:     cfg.Thresholds.KafkaLagWarn,
		KafkaLagCrit:     cfg.Thresholds.KafkaLagCrit,
	})

	// Initialize JSONL exporter
	if cfg.Export.Enabled {
		startRecord := export.SessionStartRecord{
			Environment: cfg.Environment,
			Namespace:   cfg.Namespace,
			Collectors: export.CollectorStatus{
				Prometheus: m.PromCollector != nil,
				CloudWatch: m.CWCollector != nil,
				Kubernetes: m.K8sCollector != nil,
				Locust:     m.LocustCollector != nil,
			},
			Intervals: export.IntervalConfig{
				PrometheusSec: int(cfg.Prometheus.Interval.Seconds()),
				CloudWatchSec: int(cfg.CloudWatch.Interval.Seconds()),
				KubernetesSec: int(cfg.Kubernetes.Interval.Seconds()),
				LocustSec:     int(cfg.Locust.Interval.Seconds()),
				ExportSec:     int(cfg.Export.Interval.Seconds()),
			},
			Thresholds: export.ThresholdConfig{
				CPUWarn:          cfg.Thresholds.CPUWarn,
				CPUCrit:          cfg.Thresholds.CPUCrit,
				MemoryWarn:       cfg.Thresholds.MemoryWarn,
				Error5XXWarn:     cfg.Thresholds.Error5XXWarn,
				PodRestartCrit:   cfg.Thresholds.PodRestartCrit,
				LocustFailWarn:   cfg.Thresholds.LocustFailWarn,
				ResponseTimeWarn: cfg.Thresholds.ResponseTimeWarn,
			},
		}
		exporter, err := export.New(export.Config{
			Enabled:  true,
			Path:     cfg.Export.Path,
			Interval: cfg.Export.Interval,
		}, startRecord)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: export init failed: %v\n", err)
		} else {
			m.Exporter = exporter
			defer exporter.Close()
		}
	}

	// Clean up port-forward on exit
	if promPF != nil {
		defer promPF.Stop()
	}

	// Wrap model with view rendering
	app := appModel{Model: m}

	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// appModel wraps model.Model to provide View() rendering.
type appModel struct {
	model.Model
}

func (a appModel) Init() tea.Cmd {
	return a.Model.Init()
}

func (a appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := a.Model.Update(msg)
	if updated, ok := m.(model.Model); ok {
		a.Model = updated
	}
	return a, cmd
}

func (a appModel) View() string {
	m := a.Model
	w, h := m.Width, m.Height
	if w == 0 || h == 0 {
		return "Loading..."
	}

	// Help overlay
	if m.ShowHelp {
		return renderHelp(w, h)
	}

	// Reserve 2 lines: tab bar (top) + status bar (bottom)
	tabBar := renderTabBar(m.ActiveTab, m.LocustStatus == "ok", w)
	statusBar := renderStatusBar(m, w)
	contentH := h - 2

	// Render active tab content
	var content string
	switch m.ActiveTab {
	case model.TabOverview:
		content = view.RenderOverview(w, contentH, m.PromSnapshot, m.CWSnapshot, m.K8sSnapshot, m.LocustSnapshot,
			m.Evaluator, m.TSOnlineUsers, m.TSMsgs5Min, m.TSSendRate, m.TSLocustRPS, m.TSLocustFail,
			m.TSDocDBCPU, m.TSRdsCPU, m.TSAlbRT)
	case model.TabApp:
		content = view.RenderApplication(w, contentH, m.PromSnapshot, m.CWSnapshot,
			m.TSOnlineUsers, m.TSMsgs5Min, m.TSSendRate,
			m.TSRedisInsertOK, m.TSMongoInsertOK, m.TSUserLogin,
			m.TSGatewaySendRate,
			m.TSKafkaLag, m.TSMsgLagGrowth,
			m.ScrollPos)
	case model.TabInfra:
		content = view.RenderInfrastructure(w, contentH, m.CWSnapshot, m.TSDocDBCPU, m.TSRdsCPU, m.TSAlbRT, m.ScrollPos)
	case model.TabKubernetes:
		content = view.RenderKubernetes(w, contentH, m.K8sSnapshot, m.ScrollPos)
	case model.TabLocust:
		content = view.RenderLocust(w, contentH, m.LocustSnapshot, m.TSLocustRPS, m.TSLocustFail, m.ScrollPos)
	case model.TabAlerts:
		content = view.RenderAlerts(w, contentH, m.Evaluator, m.ScrollPos)
	case model.TabLogs:
		content = view.RenderLogs(w, contentH, m.LogSnapshot, m.ScrollPos, m.ScrollXPos)
	case model.TabSystemMap:
		content = view.RenderSystemMap(w, contentH, m.K8sSnapshot, m.PromSnapshot, m.CWSnapshot, m.LocustSnapshot, m.Evaluator)
	}

	return lipgloss.JoinVertical(lipgloss.Left, tabBar, content, statusBar)
}

func renderTabBar(active int, locustOK bool, width int) string {
	var tabs []string
	for i, name := range model.TabNames {
		label := fmt.Sprintf("%d:%s", i+1, name)

		// Dim the Locust tab if not available
		if i == model.TabLocust && !locustOK {
			label = fmt.Sprintf("%d:%s", i+1, name)
			if i == active {
				tabs = append(tabs, view.TabActive.Faint(true).Render(label))
			} else {
				tabs = append(tabs, lipgloss.NewStyle().Foreground(view.ColorBorder).Render(" "+label+" "))
			}
			continue
		}

		if i == active {
			tabs = append(tabs, view.TabActive.Render(label))
		} else {
			tabs = append(tabs, view.TabInactive.Render(label))
		}
	}

	bar := strings.Join(tabs, " ")

	// Pad to full width
	barWidth := lipgloss.Width(bar)
	if barWidth < width {
		bar += strings.Repeat(" ", width-barWidth)
	}

	return bar
}

func renderStatusBar(m model.Model, width int) string {
	env := lipgloss.NewStyle().Foreground(view.ColorCyan).Bold(true).Render(strings.ToUpper(m.Config.Environment))
	ns := lipgloss.NewStyle().Foreground(view.ColorText).Render(m.Config.Namespace)

	promSt := sourceStatus("Prom", m.PromStatus)
	cwSt := sourceStatus("CW", m.CWStatus)
	k8sSt := sourceStatus("k8s", m.K8sStatus)
	locustSt := sourceStatus("Locust", m.LocustStatus)
	logsSt := sourceStatus("Logs", m.LogStatus)

	updated := ""
	if !m.LastUpdated.IsZero() {
		updated = lipgloss.NewStyle().Foreground(view.ColorSubtext).Render("Updated " + m.LastUpdated.Format("15:04:05"))
	}

	pauseIndicator := ""
	if m.Paused {
		pauseIndicator = lipgloss.NewStyle().Foreground(view.ColorYellow).Bold(true).Render(" PAUSED")
	}

	quit := lipgloss.NewStyle().Foreground(view.ColorSubtext).Render("q:quit ?:help")

	parts := []string{env, ns, promSt, cwSt, k8sSt, locustSt, logsSt, updated, pauseIndicator, quit}

	bar := strings.Join(parts, lipgloss.NewStyle().Foreground(view.ColorBorder).Render(" │ "))

	barWidth := lipgloss.Width(bar)
	if barWidth < width {
		bar += strings.Repeat(" ", width-barWidth)
	}

	return bar
}

func sourceStatus(name string, status string) string {
	label := lipgloss.NewStyle().Foreground(view.ColorSubtext).Render(name + ":")
	switch status {
	case "ok":
		return label + lipgloss.NewStyle().Foreground(view.ColorGreen).Bold(true).Render("OK")
	case "off":
		return label + lipgloss.NewStyle().Foreground(view.ColorBorder).Render("--")
	case "err":
		return label + lipgloss.NewStyle().Foreground(view.ColorRed).Bold(true).Render("ERR")
	default:
		// Not yet polled
		return label + lipgloss.NewStyle().Foreground(view.ColorBorder).Render("··")
	}
}

func renderHelp(w, h int) string {
	help := []string{
		lipgloss.NewStyle().Foreground(view.ColorCyan).Bold(true).Render("IM System Monitor - Help"),
		"",
		lipgloss.NewStyle().Foreground(view.ColorCyan).Render("Navigation"),
		view.LabelStyle.Render("  1-8          ") + view.ValueStyle.Render("Switch tabs"),
		view.LabelStyle.Render("  Tab/Shift+Tab") + view.ValueStyle.Render("Next/prev tab"),
		view.LabelStyle.Render("  j/k or ↑/↓   ") + view.ValueStyle.Render("Scroll vertical"),
		view.LabelStyle.Render("  h/l or ←/→   ") + view.ValueStyle.Render("Scroll logs horiz"),
		"",
		lipgloss.NewStyle().Foreground(view.ColorCyan).Render("Actions"),
		view.LabelStyle.Render("  r            ") + view.ValueStyle.Render("Force refresh all"),
		view.LabelStyle.Render("  p            ") + view.ValueStyle.Render("Pause/resume polling"),
		view.LabelStyle.Render("  ?            ") + view.ValueStyle.Render("Toggle this help"),
		view.LabelStyle.Render("  q / Ctrl+C   ") + view.ValueStyle.Render("Quit"),
		"",
		lipgloss.NewStyle().Foreground(view.ColorCyan).Render("Tabs"),
		view.LabelStyle.Render("  1 Overview   ") + view.ValueStyle.Render("Combined dashboard"),
		view.LabelStyle.Render("  2 App        ") + view.ValueStyle.Render("Prometheus metrics"),
		view.LabelStyle.Render("  3 Infra      ") + view.ValueStyle.Render("AWS CloudWatch"),
		view.LabelStyle.Render("  4 Kubernetes ") + view.ValueStyle.Render("Pods, HPA, events"),
		view.LabelStyle.Render("  5 Locust     ") + view.ValueStyle.Render("Load test (when active)"),
		view.LabelStyle.Render("  6 Alerts     ") + view.ValueStyle.Render("Threshold violations"),
		view.LabelStyle.Render("  7 Logs       ") + view.ValueStyle.Render("Service error logs"),
		view.LabelStyle.Render("  8 Map        ") + view.ValueStyle.Render("System topology"),
		"",
		lipgloss.NewStyle().Foreground(view.ColorSubtext).Render("Press ? to close"),
	}

	content := strings.Join(help, "\n")
	helpBox := view.HelpStyle.Render(content)
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, helpBox)
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return home + path[1:]
		}
	}
	return path
}

