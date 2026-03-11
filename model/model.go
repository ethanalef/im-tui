package model

import (
	"strings"
	"time"

	"im-tui/alert"
	"im-tui/collector"
	"im-tui/export"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// connStatus classifies an error into a status string:
//   - "ok"  = no error
//   - "off" = connection refused / unreachable (not running)
//   - "err" = other error (API error, parse error, etc.)
func connStatus(err error) string {
	if err == nil {
		return "ok"
	}
	msg := err.Error()
	if strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "dial tcp") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "context deadline exceeded") {
		return "off"
	}
	return "err"
}

const (
	TabOverview = iota
	TabApp
	TabInfra
	TabKubernetes
	TabLocust
	TabAlerts
	TabLogs
	TabSystemMap
	TabCount
)

var TabNames = []string{
	"Overview",
	"App",
	"Infra",
	"Kubernetes",
	"Locust",
	"Alerts",
	"Logs",
	"Map",
}

type Config struct {
	Namespace    string
	Environment  string
	Kubeconfig   string
	SparklineCap int

	PrometheusURL      string
	PrometheusInterval time.Duration

	CloudWatchRegion   string
	CloudWatchInterval time.Duration

	KubernetesInterval time.Duration

	LocustURL      string
	LocustInterval time.Duration

	LogInterval time.Duration
}

// Model is the root Bubble Tea model.
type Model struct {
	Config Config

	// Environment switching
	Envs     []EnvBundle
	EnvIndex int

	// Tab state
	ActiveTab int
	Width     int
	Height    int

	// Polling
	Paused bool

	// Collectors
	PromCollector   *collector.PrometheusCollector
	CWCollector     *collector.CloudWatchCollector
	K8sCollector    *collector.KubernetesCollector
	LocustCollector *collector.LocustCollector
	LogCollector    *collector.LogCollector

	// Snapshots (latest data)
	PromSnapshot   *collector.PrometheusSnapshot
	CWSnapshot     *collector.CloudWatchSnapshot
	K8sSnapshot    *collector.KubernetesSnapshot
	LocustSnapshot *collector.LocustSnapshot
	LogSnapshot    *collector.LogSnapshot

	// Source connectivity: "ok", "err", "off" (unreachable/not configured)
	PromStatus   string
	CWStatus     string
	K8sStatus    string
	LocustStatus string
	LogStatus    string

	// Time series for sparklines
	TSOnlineUsers  *collector.TimeSeries
	TSMsgs5Min     *collector.TimeSeries
	TSSendRate     *collector.TimeSeries
	TSLocustRPS    *collector.TimeSeries
	TSLocustFail   *collector.TimeSeries
	TSDocDBCPU     *collector.TimeSeries
	TSRdsCPU       *collector.TimeSeries
	TSAlbRT        *collector.TimeSeries

	// Tier 2 sparklines
	TSRedisInsertOK   *collector.TimeSeries
	TSMongoInsertOK   *collector.TimeSeries
	TSUserLogin       *collector.TimeSeries
	TSGatewaySendRate *collector.TimeSeries

	// Push pipeline sparklines
	TSLongTimePush    *collector.TimeSeries // Prometheus msg_long_time_push rate
	TSPushInFlight    *collector.TimeSeries // push_msg_in_flight gauge

	// Kafka / msg-transfer sparklines
	TSKafkaLag        *collector.TimeSeries // CloudWatch MSK total SumOffsetLag
	TSMsgLagGrowth    *collector.TimeSeries // Prometheus production-consumption rate delta

	// Alerts
	Evaluator *alert.Evaluator

	// JSONL export
	Exporter *export.Exporter

	// UI state
	ShowHelp    bool
	ScrollPos   int // per-tab vertical scroll
	ScrollXPos  int // horizontal scroll for logs message column
	LastUpdated time.Time

	// Error for display
	LastErr string
}

func NewModel(envs []EnvBundle) Model {
	first := envs[0]
	cap := first.Config.SparklineCap
	if cap == 0 {
		cap = 60
	}

	return Model{
		Envs:     envs,
		EnvIndex: 0,

		Config:          first.Config,
		PromCollector:   first.PromCollector,
		CWCollector:     first.CWCollector,
		K8sCollector:    first.K8sCollector,
		LocustCollector: first.LocustCollector,
		LogCollector:    first.LogCollector,
		Evaluator:       first.Evaluator,
		Exporter:        first.Exporter,

		ActiveTab: TabOverview,

		TSOnlineUsers:     collector.NewTimeSeries(cap),
		TSMsgs5Min:        collector.NewTimeSeries(cap),
		TSSendRate:        collector.NewTimeSeries(cap),
		TSLocustRPS:       collector.NewTimeSeries(cap),
		TSLocustFail:      collector.NewTimeSeries(cap),
		TSDocDBCPU:        collector.NewTimeSeries(cap),
		TSRdsCPU:          collector.NewTimeSeries(cap),
		TSAlbRT:           collector.NewTimeSeries(cap),
		TSRedisInsertOK:   collector.NewTimeSeries(cap),
		TSMongoInsertOK:   collector.NewTimeSeries(cap),
		TSUserLogin:       collector.NewTimeSeries(cap),
		TSGatewaySendRate: collector.NewTimeSeries(cap),
		TSLongTimePush:    collector.NewTimeSeries(cap),
		TSPushInFlight:    collector.NewTimeSeries(cap),
		TSKafkaLag:        collector.NewTimeSeries(cap),
		TSMsgLagGrowth:    collector.NewTimeSeries(cap),
	}
}

// switchEnv changes the active environment, swapping collectors and resetting state.
func (m Model) switchEnv(idx int) Model {
	env := m.Envs[idx]
	m.EnvIndex = idx
	m.Config = env.Config
	m.PromCollector = env.PromCollector
	m.CWCollector = env.CWCollector
	m.K8sCollector = env.K8sCollector
	m.LocustCollector = env.LocustCollector
	m.LogCollector = env.LogCollector
	m.Evaluator = env.Evaluator
	m.Exporter = env.Exporter

	// Reset snapshots
	m.PromSnapshot = nil
	m.CWSnapshot = nil
	m.K8sSnapshot = nil
	m.LocustSnapshot = nil
	m.LogSnapshot = nil

	// Reset statuses
	m.PromStatus = ""
	m.CWStatus = ""
	m.K8sStatus = ""
	m.LocustStatus = ""
	m.LogStatus = ""

	// Reset timeseries
	cap := m.Config.SparklineCap
	if cap == 0 {
		cap = 60
	}
	m.TSOnlineUsers = collector.NewTimeSeries(cap)
	m.TSMsgs5Min = collector.NewTimeSeries(cap)
	m.TSSendRate = collector.NewTimeSeries(cap)
	m.TSLocustRPS = collector.NewTimeSeries(cap)
	m.TSLocustFail = collector.NewTimeSeries(cap)
	m.TSDocDBCPU = collector.NewTimeSeries(cap)
	m.TSRdsCPU = collector.NewTimeSeries(cap)
	m.TSAlbRT = collector.NewTimeSeries(cap)
	m.TSRedisInsertOK = collector.NewTimeSeries(cap)
	m.TSMongoInsertOK = collector.NewTimeSeries(cap)
	m.TSUserLogin = collector.NewTimeSeries(cap)
	m.TSGatewaySendRate = collector.NewTimeSeries(cap)
	m.TSLongTimePush = collector.NewTimeSeries(cap)
	m.TSPushInFlight = collector.NewTimeSeries(cap)
	m.TSKafkaLag = collector.NewTimeSeries(cap)
	m.TSMsgLagGrowth = collector.NewTimeSeries(cap)

	// Reset scroll
	m.ScrollPos = 0
	m.ScrollXPos = 0
	m.LastUpdated = time.Time{}

	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tea.WindowSize(),
		scheduleCollect("prometheus", 0),
		scheduleCollect("cloudwatch", 0),
		scheduleCollect("kubernetes", 0),
		scheduleCollect("locust", 0),
		scheduleCollect("logs", 0),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case TickMsg:
		if m.Paused {
			return m, nil
		}
		return m, m.collectCmd(msg.Source)

	case PrometheusMsg:
		snap := msg.Snapshot
		m.PromSnapshot = &snap
		m.PromStatus = connStatus(snap.Err)
		if snap.Err == nil {
			m.TSOnlineUsers.Push(snap.OnlineUsers)
			m.TSMsgs5Min.Push(snap.MsgsIn5Min)
			m.TSSendRate.Push(snap.SendRate)
			m.TSRedisInsertOK.Push(snap.RedisInsertOK)
			m.TSMongoInsertOK.Push(snap.MongoInsertOK)
			m.TSUserLogin.Push(snap.UserLogin)
			m.TSGatewaySendRate.Push(snap.GatewaySendRate)
			m.TSLongTimePush.Push(snap.LongTimePush)
			m.TSPushInFlight.Push(snap.PushMsgInFlight)
			m.TSMsgLagGrowth.Push(snap.MsgLagGrowthRate)
		}
		m.LastUpdated = time.Now()
		m.Exporter.OnUpdate(m.exportSnapshot())
		return m, tea.Batch(
			scheduleCollect("prometheus", m.Config.PrometheusInterval),
			m.evalAlerts(),
		)

	case CloudWatchMsg:
		snap := msg.Snapshot
		m.CWSnapshot = &snap
		m.CWStatus = connStatus(snap.Err)
		if snap.Err == nil {
			m.TSDocDBCPU.Push(snap.DocDB.CPUPercent)
			m.TSRdsCPU.Push(snap.RDS.CPUPercent)
			m.TSAlbRT.Push(snap.ALB.ResponseTimeP99 * 1000)
			m.TSKafkaLag.Push(snap.MSK.TotalLag)
		}
		m.LastUpdated = time.Now()
		m.Exporter.OnUpdate(m.exportSnapshot())
		return m, tea.Batch(
			scheduleCollect("cloudwatch", m.Config.CloudWatchInterval),
			m.evalAlerts(),
		)

	case KubernetesMsg:
		snap := msg.Snapshot
		m.K8sSnapshot = &snap
		m.K8sStatus = connStatus(snap.Err)
		m.LastUpdated = time.Now()
		m.Exporter.OnUpdate(m.exportSnapshot())
		return m, tea.Batch(
			scheduleCollect("kubernetes", m.Config.KubernetesInterval),
			m.evalAlerts(),
		)

	case LocustMsg:
		snap := msg.Snapshot
		m.LocustSnapshot = &snap
		if !snap.Available {
			m.LocustStatus = "off"
		} else {
			m.LocustStatus = connStatus(snap.Err)
		}
		if snap.Available && snap.Err == nil {
			m.TSLocustRPS.Push(snap.TotalRPS)
			m.TSLocustFail.Push(snap.FailRatio * 100)
		}
		m.LastUpdated = time.Now()
		m.Exporter.OnUpdate(m.exportSnapshot())
		return m, tea.Batch(
			scheduleCollect("locust", m.Config.LocustInterval),
			m.evalAlerts(),
		)

	case LogMsg:
		snap := msg.Snapshot
		m.LogSnapshot = &snap
		m.LogStatus = connStatus(snap.Err)
		m.LastUpdated = time.Now()
		return m, scheduleCollect("logs", m.Config.LogInterval)

	case AlertMsg:
		// Alerts are stored in evaluator, no extra action needed
		return m, nil
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, Keys.Quit):
		m.Exporter.Close()
		return m, tea.Quit

	case key.Matches(msg, Keys.Tab1):
		m.ActiveTab = TabOverview
		m.ScrollPos, m.ScrollXPos = 0, 0
	case key.Matches(msg, Keys.Tab2):
		m.ActiveTab = TabApp
		m.ScrollPos, m.ScrollXPos = 0, 0
	case key.Matches(msg, Keys.Tab3):
		m.ActiveTab = TabInfra
		m.ScrollPos, m.ScrollXPos = 0, 0
	case key.Matches(msg, Keys.Tab4):
		m.ActiveTab = TabKubernetes
		m.ScrollPos, m.ScrollXPos = 0, 0
	case key.Matches(msg, Keys.Tab5):
		m.ActiveTab = TabLocust
		m.ScrollPos, m.ScrollXPos = 0, 0
	case key.Matches(msg, Keys.Tab6):
		m.ActiveTab = TabAlerts
		m.ScrollPos, m.ScrollXPos = 0, 0
	case key.Matches(msg, Keys.Tab7):
		m.ActiveTab = TabLogs
		m.ScrollPos, m.ScrollXPos = 0, 0
	case key.Matches(msg, Keys.Tab8):
		m.ActiveTab = TabSystemMap
		m.ScrollPos, m.ScrollXPos = 0, 0

	case key.Matches(msg, Keys.NextTab):
		m.ActiveTab = (m.ActiveTab + 1) % TabCount
		m.ScrollPos, m.ScrollXPos = 0, 0
	case key.Matches(msg, Keys.PrevTab):
		m.ActiveTab = (m.ActiveTab - 1 + TabCount) % TabCount
		m.ScrollPos, m.ScrollXPos = 0, 0

	case key.Matches(msg, Keys.Up):
		if m.ScrollPos > 0 {
			m.ScrollPos--
		}
	case key.Matches(msg, Keys.Down):
		m.ScrollPos++

	case key.Matches(msg, Keys.Left):
		if m.ScrollXPos > 0 {
			m.ScrollXPos -= 10
			if m.ScrollXPos < 0 {
				m.ScrollXPos = 0
			}
		}
	case key.Matches(msg, Keys.Right):
		m.ScrollXPos += 10

	case key.Matches(msg, Keys.Help):
		m.ShowHelp = !m.ShowHelp

	case key.Matches(msg, Keys.Refresh):
		return m, tea.Batch(
			m.collectCmd("prometheus"),
			m.collectCmd("cloudwatch"),
			m.collectCmd("kubernetes"),
			m.collectCmd("locust"),
			m.collectCmd("logs"),
		)

	case key.Matches(msg, Keys.Pause):
		m.Paused = !m.Paused

	case key.Matches(msg, Keys.EnvNext):
		if len(m.Envs) > 1 {
			next := (m.EnvIndex + 1) % len(m.Envs)
			m = m.switchEnv(next)
			return m, tea.Batch(
				m.collectCmd("prometheus"),
				m.collectCmd("cloudwatch"),
				m.collectCmd("kubernetes"),
				m.collectCmd("locust"),
				m.collectCmd("logs"),
			)
		}
	}

	return m, nil
}

// exportSnapshot builds a SnapshotRecord from the current model state.
func (m Model) exportSnapshot() export.SnapshotRecord {
	snap := export.SnapshotRecord{
		Sources: export.SourceStatus{
			Prometheus: statusOrPending(m.PromStatus),
			CloudWatch: statusOrPending(m.CWStatus),
			Kubernetes: statusOrPending(m.K8sStatus),
			Locust:     statusOrPending(m.LocustStatus),
			Logs:       statusOrPending(m.LogStatus),
		},
	}

	if p := m.PromSnapshot; p != nil && p.Err == nil {
		snap.App = &export.AppMetrics{
			OnlineUsers:     p.OnlineUsers,
			Msgs5Min:        p.MsgsIn5Min,
			SendRate:        p.SendRate,
			SingleChatOK:    p.SingleChatOK,
			SingleChatFail:  p.SingleChatFail,
			GroupChatOK:     p.GroupChatOK,
			GroupChatFail:   p.GroupChatFail,
			PodCount:        len(p.PodMetrics),
			RedisInsertOK:   p.RedisInsertOK,
			RedisInsertFail: p.RedisInsertFail,
			MongoInsertOK:   p.MongoInsertOK,
			MongoInsertFail: p.MongoInsertFail,
			SeqSetFail:      p.SeqSetFail,
			PushFail:        p.PushFail,
			LongTimePush:    p.LongTimePush,
			UserLogin:       p.UserLogin,
			UserRegister:    p.UserRegister,
			API5XX:          p.API5XX,
			ChatAPI5XX:      p.ChatAPI5XX,
			OpenIMAPI5XX:    p.OpenIMAPI5XX,
			GatewaySendRate:     p.GatewaySendRate,
			PushMsgInFlight:     p.PushMsgInFlight,
			PushProcessingP95:   p.PushProcessingP95,
			PushGrpcDeliveryP95: p.PushGrpcDeliveryP95,
			GatewayWsQueueP95:   p.GatewayWsQueueP95,
		}
	}

	if cw := m.CWSnapshot; cw != nil && cw.Err == nil {
		infra := &export.InfraMetrics{
			DocDBCPUPct:     cw.DocDB.CPUPercent,
			DocDBConns:      cw.DocDB.Connections,
			DocDBCursors:    cw.DocDB.CursorsTimedOut,
			RDSCPUPct:       cw.RDS.CPUPercent,
			RDSConns:        cw.RDS.Connections,
			RDSFreeMemBytes: cw.RDS.FreeMemory,
			RDSReadIOPS:     cw.RDS.ReadIOPS,
			RDSWriteIOPS:    cw.RDS.WriteIOPS,
			ALBP99Ms:        cw.ALB.ResponseTimeP99 * 1000,
			ALB5XX:          cw.ALB.Count5XX,
			ALBActiveConns:  cw.ALB.ActiveConns,
			ALBRequestCount: cw.ALB.RequestCount,
			KafkaTotalLag:   cw.MSK.TotalLag,
		}
		for _, cl := range cw.MSK.ConsumerLag {
			infra.KafkaLagByGroup = append(infra.KafkaLagByGroup, export.KafkaLagRecord{
				Group: cl.Group,
				Topic: cl.Topic,
				Lag:   cl.Lag,
			})
		}
		snap.Infra = infra
	}

	if k := m.K8sSnapshot; k != nil && k.Err == nil {
		km := &export.K8sMetrics{
			TotalPods:     len(k.Pods),
			WarningEvents: len(k.Events),
		}
		for _, pod := range k.Pods {
			if pod.Status == "Running" {
				km.RunningPods++
			}
			km.TotalRestarts += pod.Restarts
		}
		for _, hpa := range k.HPAs {
			km.HPAs = append(km.HPAs, export.HPASnapshot{
				Name:    hpa.Name,
				Current: hpa.Current,
				Min:     hpa.MinReplicas,
				Max:     hpa.MaxReplicas,
			})
		}
		snap.K8s = km
	}

	if l := m.LocustSnapshot; l != nil && l.Available && l.Err == nil {
		snap.Locust = &export.LocustMetrics{
			State:     l.State,
			Users:     l.UserCount,
			RPS:       l.TotalRPS,
			FailRatio: l.FailRatio * 100,
		}
	}

	if ls := m.LogSnapshot; ls != nil && ls.Err == nil && len(ls.Services) > 0 {
		logSummary := &export.LogSummary{}
		for _, svc := range ls.Services {
			logSummary.Services = append(logSummary.Services, export.ServiceLogRecord{
				Name:     svc.Name,
				Errors:   svc.Errors,
				Fails:    svc.Fails,
				Timeouts: svc.Timeouts,
				Panics:   svc.Panics,
			})
		}
		snap.Logs = logSummary
	}

	// Active alerts
	if m.Evaluator != nil {
		active := m.Evaluator.Active()
		snap.Alerts.ActiveCount = len(active)
		for _, a := range active {
			snap.Alerts.Active = append(snap.Alerts.Active, export.AlertRecord{
				Level:   string(a.Level),
				Metric:  a.Metric,
				Value:   a.Value,
				Message: a.Message,
			})
		}
	}

	return snap
}

func statusOrPending(s string) string {
	if s == "" {
		return "pending"
	}
	return s
}

func (m Model) collectCmd(source string) tea.Cmd {
	switch source {
	case "prometheus":
		if m.PromCollector == nil {
			return nil
		}
		c := m.PromCollector
		ns := m.Config.Namespace
		return func() tea.Msg {
			snap := c.Collect(ns)
			return PrometheusMsg{Snapshot: snap}
		}
	case "cloudwatch":
		if m.CWCollector == nil {
			return nil
		}
		c := m.CWCollector
		return func() tea.Msg {
			snap := c.Collect()
			return CloudWatchMsg{Snapshot: snap}
		}
	case "kubernetes":
		if m.K8sCollector == nil {
			return nil
		}
		c := m.K8sCollector
		return func() tea.Msg {
			snap := c.Collect()
			return KubernetesMsg{Snapshot: snap}
		}
	case "locust":
		if m.LocustCollector == nil {
			return nil
		}
		c := m.LocustCollector
		return func() tea.Msg {
			snap := c.Collect()
			return LocustMsg{Snapshot: snap}
		}
	case "logs":
		if m.LogCollector == nil {
			return nil
		}
		c := m.LogCollector
		return func() tea.Msg {
			snap := c.Collect()
			return LogMsg{Snapshot: snap}
		}
	}
	return nil
}

func (m Model) evalAlerts() tea.Cmd {
	if m.Evaluator == nil {
		return nil
	}
	ev := m.Evaluator
	prom := m.PromSnapshot
	cw := m.CWSnapshot
	k8s := m.K8sSnapshot
	locust := m.LocustSnapshot
	return func() tea.Msg {
		alerts := ev.Evaluate(prom, cw, k8s, locust)
		var out []Alert
		for _, a := range alerts {
			out = append(out, Alert{
				Level:   string(a.Level),
				Metric:  a.Metric,
				Value:   a.Value,
				Message: a.Message,
			})
		}
		return AlertMsg{Alerts: out}
	}
}

func scheduleCollect(source string, delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(t time.Time) tea.Msg {
		return TickMsg{Source: source}
	})
}

// View dispatches to the appropriate tab renderer.
// This is defined here as a stub - the actual rendering is in view package,
// called from main.go through a wrapper.
func (m Model) View() string {
	// This will be overridden by the view wrapper in main.go
	return ""
}
