package export

import (
	"math"
	"time"
)

// stat tracks min/max/sum/count in O(1) per ingest.
type stat struct {
	min   float64
	max   float64
	sum   float64
	count int
}

func newStat() stat {
	return stat{min: math.MaxFloat64, max: -math.MaxFloat64}
}

func (s *stat) add(v float64) {
	if v < s.min {
		s.min = v
	}
	if v > s.max {
		s.max = v
	}
	s.sum += v
	s.count++
}

func (s *stat) result() MinMaxAvg {
	if s.count == 0 {
		return MinMaxAvg{}
	}
	return MinMaxAvg{
		Min: s.min,
		Max: s.max,
		Avg: s.sum / float64(s.count),
	}
}

// hpaState tracks the last known replica count for scaling event detection.
type hpaState struct {
	lastReplicas int
	seen         bool
}

// aggregator accumulates running statistics across snapshots.
type aggregator struct {
	startTime time.Time

	// App metrics
	onlineUsers stat
	msgs5Min    stat
	sendRate    stat

	// Infra metrics
	docdbCPU     stat
	rdsCPU       stat
	rdsReadIOPS  stat
	rdsWriteIOPS stat
	albP99       stat
	alb5XXTotal  float64

	// K8s peaks
	peakPods     int
	peakRestarts int

	// HPA scaling event tracking
	hpaStates     map[string]*hpaState
	scalingEvents []HPAScaleEvent

	// Alert tracking
	totalAlertsFired int
	alertsBySeverity map[string]int
	alertsByMetric   map[string]int

	snapshotCount int
}

func newAggregator() *aggregator {
	return &aggregator{
		startTime:        time.Now(),
		onlineUsers:      newStat(),
		msgs5Min:         newStat(),
		sendRate:         newStat(),
		docdbCPU:         newStat(),
		rdsCPU:           newStat(),
		rdsReadIOPS:      newStat(),
		rdsWriteIOPS:     newStat(),
		albP99:           newStat(),
		hpaStates:        make(map[string]*hpaState),
		alertsBySeverity: make(map[string]int),
		alertsByMetric:   make(map[string]int),
	}
}

// ingest processes a snapshot and updates running stats.
func (a *aggregator) ingest(snap *SnapshotRecord) {
	a.snapshotCount++

	if snap.App != nil {
		a.onlineUsers.add(snap.App.OnlineUsers)
		a.msgs5Min.add(snap.App.Msgs5Min)
		a.sendRate.add(snap.App.SendRate)
	}

	if snap.Infra != nil {
		a.docdbCPU.add(snap.Infra.DocDBCPUPct)
		a.rdsCPU.add(snap.Infra.RDSCPUPct)
		a.rdsReadIOPS.add(snap.Infra.RDSReadIOPS)
		a.rdsWriteIOPS.add(snap.Infra.RDSWriteIOPS)
		a.albP99.add(snap.Infra.ALBP99Ms)
		a.alb5XXTotal += snap.Infra.ALB5XX
	}

	if snap.K8s != nil {
		if snap.K8s.TotalPods > a.peakPods {
			a.peakPods = snap.K8s.TotalPods
		}
		if snap.K8s.TotalRestarts > a.peakRestarts {
			a.peakRestarts = snap.K8s.TotalRestarts
		}
		// Track HPA scaling events
		for _, hpa := range snap.K8s.HPAs {
			st, ok := a.hpaStates[hpa.Name]
			if !ok {
				a.hpaStates[hpa.Name] = &hpaState{lastReplicas: hpa.Current, seen: true}
				continue
			}
			if st.seen && st.lastReplicas != hpa.Current {
				a.scalingEvents = append(a.scalingEvents, HPAScaleEvent{
					Name:      hpa.Name,
					From:      st.lastReplicas,
					To:        hpa.Current,
					Timestamp: snap.Timestamp,
				})
			}
			st.lastReplicas = hpa.Current
			st.seen = true
		}
	}
}

// ingestAlerts tracks alert counts by severity and metric.
func (a *aggregator) ingestAlerts(alerts []AlertRecord) {
	for _, al := range alerts {
		a.totalAlertsFired++
		a.alertsBySeverity[al.Level]++
		a.alertsByMetric[al.Metric]++
	}
}

// summary produces the final SessionSummary.
func (a *aggregator) summary() SessionSummary {
	return SessionSummary{
		App: AppSummary{
			OnlineUsers: a.onlineUsers.result(),
			Msgs5Min:    a.msgs5Min.result(),
			SendRate:    a.sendRate.result(),
		},
		Infra: InfraSummary{
			DocDBCPUPct:  a.docdbCPU.result(),
			RDSCPUPct:    a.rdsCPU.result(),
			RDSReadIOPS:  a.rdsReadIOPS.result(),
			RDSWriteIOPS: a.rdsWriteIOPS.result(),
			ALBP99Ms:     a.albP99.result(),
			ALB5XXTotal:  a.alb5XXTotal,
		},
		K8s: K8sSummary{
			PeakPods:      a.peakPods,
			PeakRestarts:  a.peakRestarts,
			ScalingEvents: a.scalingEvents,
		},
		Alerts: AlertSummary{
			TotalFired: a.totalAlertsFired,
			BySeverity: a.alertsBySeverity,
			ByMetric:   a.alertsByMetric,
		},
	}
}
