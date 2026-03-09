package alert

import (
	"fmt"
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

		// Goroutine checks per pod
		for _, pm := range prom.PodMetrics {
			if e.thresholds.GoroutineCrit > 0 && pm.Goroutines >= e.thresholds.GoroutineCrit {
				alerts = append(alerts, Alert{LevelCritical, "Goroutines " + pm.Pod, fmt.Sprintf("%.0f", pm.Goroutines), "Goroutine leak suspected", now})
			} else if e.thresholds.GoroutineWarn > 0 && pm.Goroutines >= e.thresholds.GoroutineWarn {
				alerts = append(alerts, Alert{LevelWarning, "Goroutines " + pm.Pod, fmt.Sprintf("%.0f", pm.Goroutines), "Goroutine count elevated", now})
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
