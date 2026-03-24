package alert

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"im-tui/collector"
)

type Level string

const (
	LevelWarning  Level = "warning"
	LevelCritical Level = "critical"
)

type Alert struct {
	Level   Level
	Metric  string
	Value   string
	Message string
	Time    time.Time
}

type Thresholds struct {
	CPUWarn          float64
	CPUCrit          float64
	MemoryWarn       float64
	Error5XXWarn     int
	PodRestartCrit   int
	LocustFailWarn   float64
	ResponseTimeWarn int // ms

	DocDBConnWarn    float64
	DocDBConnCrit    float64
	RDSLatencyWarnMs float64
	RDSLatencyCritMs float64
	RDSDiskQueueWarn float64
	RDSDiskQueueCrit float64
	RedisEvictWarn   float64
	RedisEvictCrit   float64
	GoroutineWarn    float64
	GoroutineCrit    float64
	KafkaLagWarn     float64
	KafkaLagCrit     float64

	// Pipeline latency P95 thresholds (upgrade version metrics)
	E2EGroupWarnS      float64
	E2EGroupCritS      float64
	E2ESingleWarnS     float64
	E2ESingleCritS     float64
	GatewayEncodeWarnS float64
	GatewayEncodeCritS float64
	TransferBatchWarnS float64
	TransferBatchCritS float64

	// Spike detection
	SpikeRisePct    float64 // % increase over baseline → warning (2x → critical)
	SpikeMinSamples int     // min data points before detection activates
}

// SpikeInput provides a named time series for spike detection.
type SpikeInput struct {
	Name   string
	Values []float64 // chronological, latest last
}

type Evaluator struct {
	mu         sync.Mutex
	thresholds Thresholds
	history    []Alert
	active     []Alert
}

func NewEvaluator(t Thresholds) *Evaluator {
	return &Evaluator{thresholds: t}
}

func (e *Evaluator) Evaluate(
	prom *collector.PrometheusSnapshot,
	cw *collector.CloudWatchSnapshot,
	k8s *collector.KubernetesSnapshot,
	locust *collector.LocustSnapshot,
	spikes []SpikeInput,
) []Alert {
	var alerts []Alert
	now := time.Now()

	// Tier 1: Prometheus storage pipeline failures (any > 0 = data loss risk)
	if prom != nil && prom.Err == nil {
		if prom.RedisInsertFail > 0 {
			alerts = append(alerts, Alert{LevelCritical, "Redis Insert Fail", fmt.Sprintf("%.2f/s", prom.RedisInsertFail), "Messages failing to insert into Redis", now})
		}
		if prom.MongoInsertFail > 0 {
			alerts = append(alerts, Alert{LevelCritical, "Mongo Insert Fail", fmt.Sprintf("%.2f/s", prom.MongoInsertFail), "Messages failing to persist to MongoDB", now})
		}
		if prom.SeqSetFail > 0 {
			alerts = append(alerts, Alert{LevelCritical, "Seq Set Fail", fmt.Sprintf("%.2f/s", prom.SeqSetFail), "Message sequence assignment failing", now})
		}
		if prom.PushFail > 0 {
			alerts = append(alerts, Alert{LevelWarning, "Push Fail", fmt.Sprintf("%.2f/s", prom.PushFail), "Offline push notifications failing", now})
		}
		if prom.LongTimePush > 0 {
			alerts = append(alerts, Alert{LevelWarning, "Push Slow >10s", fmt.Sprintf("%.2f/s", prom.LongTimePush), "Messages taking >10s from send to push delivery — push pipeline backlogged", now})
		}
		if prom.API5XX > 0 {
			alerts = append(alerts, Alert{LevelWarning, "API 5XX", fmt.Sprintf("%.2f/s", prom.API5XX), "API server returning 5XX errors", now})
		}
	}

	// CloudWatch infrastructure checks
	if cw != nil && cw.Err == nil {
		// DocDB CPU
		if cw.DocDB.CPUPercent >= e.thresholds.CPUCrit {
			alerts = append(alerts, Alert{LevelCritical, "DocDB CPU", fmt.Sprintf("%.1f%%", cw.DocDB.CPUPercent), "DocDB CPU critical", now})
		} else if cw.DocDB.CPUPercent >= e.thresholds.CPUWarn {
			alerts = append(alerts, Alert{LevelWarning, "DocDB CPU", fmt.Sprintf("%.1f%%", cw.DocDB.CPUPercent), "DocDB CPU elevated", now})
		}

		// DocDB Connections
		if e.thresholds.DocDBConnCrit > 0 && cw.DocDB.Connections >= e.thresholds.DocDBConnCrit {
			alerts = append(alerts, Alert{LevelCritical, "DocDB Conns", fmt.Sprintf("%.0f", cw.DocDB.Connections), "DocDB connections critical", now})
		} else if e.thresholds.DocDBConnWarn > 0 && cw.DocDB.Connections >= e.thresholds.DocDBConnWarn {
			alerts = append(alerts, Alert{LevelWarning, "DocDB Conns", fmt.Sprintf("%.0f", cw.DocDB.Connections), "DocDB connections elevated", now})
		}

		// DocDB Cursors Timed Out
		if cw.DocDB.CursorsTimedOut > 0 {
			alerts = append(alerts, Alert{LevelWarning, "DocDB Cursors", fmt.Sprintf("%.0f", cw.DocDB.CursorsTimedOut), "DocDB cursors timing out", now})
		}

		// RDS CPU
		if cw.RDS.CPUPercent >= e.thresholds.CPUCrit {
			alerts = append(alerts, Alert{LevelCritical, "RDS CPU", fmt.Sprintf("%.1f%%", cw.RDS.CPUPercent), "RDS CPU critical", now})
		} else if cw.RDS.CPUPercent >= e.thresholds.CPUWarn {
			alerts = append(alerts, Alert{LevelWarning, "RDS CPU", fmt.Sprintf("%.1f%%", cw.RDS.CPUPercent), "RDS CPU elevated", now})
		}

		// RDS Read Latency (value is in seconds, threshold is in ms)
		rdsReadMs := cw.RDS.ReadLatency * 1000
		if e.thresholds.RDSLatencyCritMs > 0 && rdsReadMs >= e.thresholds.RDSLatencyCritMs {
			alerts = append(alerts, Alert{LevelCritical, "RDS Read Lat", fmt.Sprintf("%.1fms", rdsReadMs), "RDS read latency critical", now})
		} else if e.thresholds.RDSLatencyWarnMs > 0 && rdsReadMs >= e.thresholds.RDSLatencyWarnMs {
			alerts = append(alerts, Alert{LevelWarning, "RDS Read Lat", fmt.Sprintf("%.1fms", rdsReadMs), "RDS read latency elevated", now})
		}

		// RDS Write Latency
		rdsWriteMs := cw.RDS.WriteLatency * 1000
		if e.thresholds.RDSLatencyCritMs > 0 && rdsWriteMs >= e.thresholds.RDSLatencyCritMs {
			alerts = append(alerts, Alert{LevelCritical, "RDS Write Lat", fmt.Sprintf("%.1fms", rdsWriteMs), "RDS write latency critical", now})
		} else if e.thresholds.RDSLatencyWarnMs > 0 && rdsWriteMs >= e.thresholds.RDSLatencyWarnMs {
			alerts = append(alerts, Alert{LevelWarning, "RDS Write Lat", fmt.Sprintf("%.1fms", rdsWriteMs), "RDS write latency elevated", now})
		}

		// RDS Disk Queue Depth
		if e.thresholds.RDSDiskQueueCrit > 0 && cw.RDS.DiskQueue >= e.thresholds.RDSDiskQueueCrit {
			alerts = append(alerts, Alert{LevelCritical, "RDS Disk Queue", fmt.Sprintf("%.1f", cw.RDS.DiskQueue), "RDS disk queue depth critical", now})
		} else if e.thresholds.RDSDiskQueueWarn > 0 && cw.RDS.DiskQueue >= e.thresholds.RDSDiskQueueWarn {
			alerts = append(alerts, Alert{LevelWarning, "RDS Disk Queue", fmt.Sprintf("%.1f", cw.RDS.DiskQueue), "RDS disk queue depth elevated", now})
		}

		// Redis per-node
		for _, r := range cw.Redis {
			if r.CPUPercent >= e.thresholds.CPUCrit {
				alerts = append(alerts, Alert{LevelCritical, "Redis CPU " + r.NodeID, fmt.Sprintf("%.1f%%", r.CPUPercent), "Redis CPU critical", now})
			} else if r.CPUPercent >= e.thresholds.CPUWarn {
				alerts = append(alerts, Alert{LevelWarning, "Redis CPU " + r.NodeID, fmt.Sprintf("%.1f%%", r.CPUPercent), "Redis CPU elevated", now})
			}

			// Redis memory
			if r.MemoryPercent >= e.thresholds.MemoryWarn {
				alerts = append(alerts, Alert{LevelWarning, "Redis Memory " + r.NodeID, fmt.Sprintf("%.1f%%", r.MemoryPercent), "Redis memory high", now})
			}

			// Redis evictions
			if e.thresholds.RedisEvictCrit > 0 && r.Evictions >= e.thresholds.RedisEvictCrit {
				alerts = append(alerts, Alert{LevelCritical, "Redis Evict " + r.NodeID, fmt.Sprintf("%.0f", r.Evictions), "Redis evictions critical — cache pressure", now})
			} else if e.thresholds.RedisEvictWarn > 0 && r.Evictions >= e.thresholds.RedisEvictWarn {
				alerts = append(alerts, Alert{LevelWarning, "Redis Evict " + r.NodeID, fmt.Sprintf("%.0f", r.Evictions), "Redis evictions detected", now})
			}
		}

		// MSK Kafka consumer lag
		if cw.MSK.TotalLag > 0 {
			if e.thresholds.KafkaLagCrit > 0 && cw.MSK.TotalLag >= e.thresholds.KafkaLagCrit {
				alerts = append(alerts, Alert{LevelCritical, "Kafka Lag", fmt.Sprintf("%.0f", cw.MSK.TotalLag), "Kafka consumer lag critical — messages backlogged", now})
			} else if e.thresholds.KafkaLagWarn > 0 && cw.MSK.TotalLag >= e.thresholds.KafkaLagWarn {
				alerts = append(alerts, Alert{LevelWarning, "Kafka Lag", fmt.Sprintf("%.0f", cw.MSK.TotalLag), "Kafka consumer lag elevated", now})
			}
			// Per-group alerts for critical lag
			for _, cl := range cw.MSK.ConsumerLag {
				if e.thresholds.KafkaLagCrit > 0 && cl.Lag >= e.thresholds.KafkaLagCrit {
					alerts = append(alerts, Alert{LevelCritical, "Kafka Lag " + cl.Group, fmt.Sprintf("%.0f", cl.Lag), "Consumer group " + cl.Group + " severely behind", now})
				}
			}
		}

		// Prometheus: lag growth rate (production > consumption = backlog growing)
		if prom != nil && prom.Err == nil && prom.MsgLagGrowthRate > 0.5 {
			alerts = append(alerts, Alert{LevelWarning, "Msg Lag Growth", fmt.Sprintf("%.2f/s", prom.MsgLagGrowthRate), "msg-transfer consuming slower than production", now})
		}

		// ALB 5XX
		if int(cw.ALB.Count5XX) > 0 {
			if int(cw.ALB.Count5XX) >= 10 {
				alerts = append(alerts, Alert{LevelCritical, "ALB 5XX", fmt.Sprintf("%.0f", cw.ALB.Count5XX), "High 5XX error count", now})
			} else if int(cw.ALB.Count5XX) >= e.thresholds.Error5XXWarn {
				alerts = append(alerts, Alert{LevelWarning, "ALB 5XX", fmt.Sprintf("%.0f", cw.ALB.Count5XX), "5XX errors detected", now})
			}
		}
	}

	// Prometheus: per-service 5XX (more specific than aggregate)
	if prom != nil && prom.Err == nil {
		if prom.ChatAPI5XX > 0 {
			alerts = append(alerts, Alert{LevelWarning, "chat-api 5XX", fmt.Sprintf("%.2f/s", prom.ChatAPI5XX), "chat-api returning 5XX errors", now})
		}
		if prom.OpenIMAPI5XX > 0 {
			alerts = append(alerts, Alert{LevelWarning, "openim-api 5XX", fmt.Sprintf("%.2f/s", prom.OpenIMAPI5XX), "openim-api returning 5XX errors", now})
		}

		// Goroutine checks per pod — gateway pods get specific dead connection leak messaging
		for _, pm := range prom.PodMetrics {
			isGW := strings.Contains(pm.Pod, "msg-gateway") || strings.Contains(pm.Pod, "gateway")
			if e.thresholds.GoroutineCrit > 0 && pm.Goroutines >= e.thresholds.GoroutineCrit {
				if isGW {
					heapMB := pm.HeapInUse / (1 << 20)
					alerts = append(alerts, Alert{LevelCritical,
						"GW Dead Conns " + pm.Pod,
						fmt.Sprintf("%.0f goroutines, %.0f MiB heap", pm.Goroutines, heapMB),
						"DEAD CONNECTION LEAK: zombie goroutines from unclean WS disconnects. " +
							"All messaging degraded — redeploy msg-gateway immediately",
						now})
				} else {
					alerts = append(alerts, Alert{LevelCritical, "Goroutines " + pm.Pod, fmt.Sprintf("%.0f", pm.Goroutines), "Goroutine leak suspected", now})
				}
			} else if e.thresholds.GoroutineWarn > 0 && pm.Goroutines >= e.thresholds.GoroutineWarn {
				if isGW {
					alerts = append(alerts, Alert{LevelWarning,
						"GW Dead Conns " + pm.Pod,
						fmt.Sprintf("%.0f goroutines", pm.Goroutines),
						"Dead connections accumulating — monitor for further growth, redeploy if degraded",
						now})
				} else {
					alerts = append(alerts, Alert{LevelWarning, "Goroutines " + pm.Pod, fmt.Sprintf("%.0f", pm.Goroutines), "Goroutine count elevated", now})
				}
			} else if isGW && pm.OnlineUsers > 0 {
				// Ratio-based gateway check: expected = users*3 + 2200 (MemoryQueue workers + infra)
				expected := pm.OnlineUsers*3 + 2200
				excess := pm.Goroutines - expected
				if excess >= 5000 {
					alerts = append(alerts, Alert{LevelCritical,
						"GW Dead Conns " + pm.Pod,
						fmt.Sprintf("%.0f goroutines vs %.0f expected (%.0f users)", pm.Goroutines, expected, pm.OnlineUsers),
						"Zombie goroutine leak — excess goroutines far above online user count",
						now})
				} else if excess >= 2000 {
					alerts = append(alerts, Alert{LevelWarning,
						"GW Goroutines " + pm.Pod,
						fmt.Sprintf("%.0f goroutines vs %.0f expected (%.0f users)", pm.Goroutines, expected, pm.OnlineUsers),
						"Gateway goroutines elevated above expected — possible dead connections",
						now})
				}
			}
		}
	}

	// Kubernetes pod restarts
	if k8s != nil && k8s.Err == nil {
		for _, pod := range k8s.Pods {
			if pod.Restarts >= e.thresholds.PodRestartCrit {
				alerts = append(alerts, Alert{LevelCritical, "Pod Restarts", fmt.Sprintf("%s: %d", pod.Name, pod.Restarts), "Pod has restarted", now})
			}
		}
	}

	// Locust fail rate
	if locust != nil && locust.Available && locust.Err == nil {
		failPct := locust.FailRatio * 100
		if failPct >= 5.0 {
			alerts = append(alerts, Alert{LevelCritical, "Locust Fail Rate", fmt.Sprintf("%.1f%%", failPct), "Load test high failure rate", now})
		} else if failPct >= e.thresholds.LocustFailWarn {
			alerts = append(alerts, Alert{LevelWarning, "Locust Fail Rate", fmt.Sprintf("%.1f%%", failPct), "Load test failure rate elevated", now})
		}
	}

	// Pipeline latency P95 alerts (upgrade version metrics — skip if NaN/0)
	if prom != nil && prom.Err == nil {
		type latencyCheck struct {
			name     string
			value    float64
			warn, crit float64
		}
		for _, lc := range []latencyCheck{
			{"E2E Group Delivery", prom.E2EDeliveryGroupP95, e.thresholds.E2EGroupWarnS, e.thresholds.E2EGroupCritS},
			{"E2E Single Delivery", prom.E2EDeliverySingleP95, e.thresholds.E2ESingleWarnS, e.thresholds.E2ESingleCritS},
			{"GW Encode P95", prom.GatewayEncodeP95, e.thresholds.GatewayEncodeWarnS, e.thresholds.GatewayEncodeCritS},
			{"Transfer Batch P95", prom.TransferBatchP95, e.thresholds.TransferBatchWarnS, e.thresholds.TransferBatchCritS},
		} {
			if lc.value == 0 || lc.value != lc.value { // skip 0 or NaN
				continue
			}
			ms := lc.value * 1000
			if lc.crit > 0 && lc.value >= lc.crit {
				alerts = append(alerts, Alert{LevelCritical, lc.name, fmt.Sprintf("%.0fms", ms), "Pipeline latency critical", now})
			} else if lc.warn > 0 && lc.value >= lc.warn {
				alerts = append(alerts, Alert{LevelWarning, lc.name, fmt.Sprintf("%.0fms", ms), "Pipeline latency elevated", now})
			}
		}
	}

	// Spike detection: sudden rapid rises in infrastructure metrics
	alerts = append(alerts, e.detectSpikes(spikes, now)...)

	// Update active/history under lock
	e.mu.Lock()
	e.active = alerts
	if len(alerts) > 0 {
		e.history = append(alerts, e.history...)
		// Cap history at 200
		if len(e.history) > 200 {
			e.history = e.history[:200]
		}
	}
	e.mu.Unlock()

	return alerts
}

// detectSpikes checks each SpikeInput for sudden rapid rises.
// Compares the latest value against the mean of all prior values (baseline).
// Warning at SpikeRisePct increase, Critical at 2x SpikeRisePct.
func (e *Evaluator) detectSpikes(inputs []SpikeInput, now time.Time) []Alert {
	pct := e.thresholds.SpikeRisePct
	if pct <= 0 {
		return nil
	}
	minSamples := e.thresholds.SpikeMinSamples
	if minSamples < 2 {
		minSamples = 3
	}

	var alerts []Alert
	for _, inp := range inputs {
		n := len(inp.Values)
		if n < minSamples {
			continue
		}

		current := inp.Values[n-1]
		// Baseline = mean of all values except the last
		var sum float64
		for _, v := range inp.Values[:n-1] {
			sum += v
		}
		baseline := sum / float64(n-1)

		// Skip if baseline is too low (avoid noise on near-zero metrics)
		if baseline < 1.0 {
			continue
		}

		risePct := (current - baseline) / baseline * 100
		if risePct >= pct*2 {
			alerts = append(alerts, Alert{
				LevelCritical, inp.Name + " Spike",
				fmt.Sprintf("%.1f (%.0f%% rise)", current, risePct),
				fmt.Sprintf("%s surged from baseline %.1f — rapid rise detected", inp.Name, baseline),
				now,
			})
		} else if risePct >= pct {
			alerts = append(alerts, Alert{
				LevelWarning, inp.Name + " Spike",
				fmt.Sprintf("%.1f (%.0f%% rise)", current, risePct),
				fmt.Sprintf("%s rising from baseline %.1f — monitor for further increase", inp.Name, baseline),
				now,
			})
		}
	}
	return alerts
}

// Active returns a copy of the current active alerts.
func (e *Evaluator) Active() []Alert {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Alert, len(e.active))
	copy(out, e.active)
	return out
}

// History returns a copy of the alert history.
func (e *Evaluator) History() []Alert {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Alert, len(e.history))
	copy(out, e.history)
	return out
}
