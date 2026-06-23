package alert

import (
	"testing"
	"time"

	"im-tui/collector"
)

// thresholds that make a single persistent condition (DocDB connections) fire.
// CPU thresholds are set non-zero so zero-valued CPU metrics don't trip them.
func docdbConnThresholds() Thresholds {
	return Thresholds{CPUWarn: 50, CPUCrit: 80, DocDBConnWarn: 80, DocDBConnCrit: 100}
}

func docdbConnSnapshot(conns float64) *collector.CloudWatchSnapshot {
	return &collector.CloudWatchSnapshot{DocDB: collector.DocDBMetrics{Connections: conns}}
}

func findAlert(alerts []Alert, metric string) (Alert, bool) {
	for _, a := range alerts {
		if a.Metric == metric {
			return a, true
		}
	}
	return Alert{}, false
}

func assertAlert(t *testing.T, alerts []Alert, level Level, metric string) {
	t.Helper()
	for _, a := range alerts {
		if a.Level == level && a.Metric == metric {
			return
		}
	}
	t.Fatalf("expected %s alert for %q, got %+v", level, metric, alerts)
}

func assertNoAlert(t *testing.T, alerts []Alert, level Level, metric string) {
	t.Helper()
	for _, a := range alerts {
		if a.Level == level && a.Metric == metric {
			t.Fatalf("did not expect %s alert for %q, got %+v", level, metric, alerts)
		}
	}
}

// --- Push-quality thresholds (warn + crit per-second) ---

func TestPushRateThresholdsSuppressBackgroundRates(t *testing.T) {
	e := NewEvaluator(Thresholds{
		PushFailWarnPerSec:     5,
		PushFailCritPerSec:     20,
		LongTimePushWarnPerSec: 1,
		LongTimePushCritPerSec: 5,
	})

	alerts := e.Evaluate(&collector.PrometheusSnapshot{
		PushFail:     1.82,
		LongTimePush: 0.33,
	}, nil, nil, nil, nil)
	if len(alerts) != 0 {
		t.Fatalf("expected background push rates below thresholds to be silent, got %+v", alerts)
	}
}

func TestPushRateThresholdsWarnAndCrit(t *testing.T) {
	e := NewEvaluator(Thresholds{
		PushFailWarnPerSec:     5,
		PushFailCritPerSec:     20,
		LongTimePushWarnPerSec: 1,
		LongTimePushCritPerSec: 5,
	})

	alerts := e.Evaluate(&collector.PrometheusSnapshot{
		PushFail:     6,
		LongTimePush: 2,
	}, nil, nil, nil, nil)
	assertAlert(t, alerts, LevelWarning, "Push Fail")
	assertAlert(t, alerts, LevelWarning, "Push Slow >10s")

	alerts = e.Evaluate(&collector.PrometheusSnapshot{
		PushFail:     20,
		LongTimePush: 5,
	}, nil, nil, nil, nil)
	assertAlert(t, alerts, LevelCritical, "Push Fail")
	assertAlert(t, alerts, LevelCritical, "Push Slow >10s")
}

func TestPushRateThresholdsDefaultToAnyPositive(t *testing.T) {
	e := NewEvaluator(Thresholds{})

	alerts := e.Evaluate(&collector.PrometheusSnapshot{PushFail: 0.1}, nil, nil, nil, nil)
	assertAlert(t, alerts, LevelWarning, "Push Fail")
}

// --- SMS verification-code provider failures ---

func TestSMSProviderFailuresUseReasonSpecificSeverity(t *testing.T) {
	e := NewEvaluator(Thresholds{})

	alerts := e.Evaluate(&collector.PrometheusSnapshot{
		SMSAliBusinessStopped:         0.2,
		SMSTencentInsufficientBalance: 0.3,
		SMSNoProviderSuccess:          0.4,
		SMSTencentPhoneFormat:         0.5,
	}, nil, nil, nil, nil)

	assertAlert(t, alerts, LevelCritical, "SMS Aliyun Stopped")
	assertAlert(t, alerts, LevelCritical, "SMS Tencent Balance")
	assertAlert(t, alerts, LevelCritical, "SMS No Provider")
	assertAlert(t, alerts, LevelWarning, "SMS Tencent Format")
}

func TestSMSOtherFailureThresholds(t *testing.T) {
	e := NewEvaluator(Thresholds{
		SMSFailWarnPerSec: 5,
		SMSFailCritPerSec: 20,
	})

	alerts := e.Evaluate(&collector.PrometheusSnapshot{SMSOtherFailure: 4}, nil, nil, nil, nil)
	if len(alerts) != 0 {
		t.Fatalf("expected SMS failures below threshold to be silent, got %+v", alerts)
	}

	alerts = e.Evaluate(&collector.PrometheusSnapshot{SMSOtherFailure: 5}, nil, nil, nil, nil)
	assertAlert(t, alerts, LevelWarning, "SMS Other Fail")

	alerts = e.Evaluate(&collector.PrometheusSnapshot{SMSOtherFailure: 20}, nil, nil, nil, nil)
	assertAlert(t, alerts, LevelCritical, "SMS Other Fail")
}

func TestSMSOtherFailureDefaultsToAnyPositive(t *testing.T) {
	e := NewEvaluator(Thresholds{})

	alerts := e.Evaluate(&collector.PrometheusSnapshot{SMSOtherFailure: 0.1}, nil, nil, nil, nil)
	assertAlert(t, alerts, LevelWarning, "SMS Other Fail")
}

// --- ALB 5XX configurable critical threshold ---

func TestALB5XXUsesConfigurableCriticalThreshold(t *testing.T) {
	e := NewEvaluator(Thresholds{
		CPUWarn:      60,
		CPUCrit:      85,
		Error5XXWarn: 50,
		Error5XXCrit: 200,
	})

	alerts := e.Evaluate(nil, &collector.CloudWatchSnapshot{
		ALB: collector.ALBMetrics{Count5XX: 53},
	}, nil, nil, nil)
	assertAlert(t, alerts, LevelWarning, "ALB 5XX")
	assertNoAlert(t, alerts, LevelCritical, "ALB 5XX")
}

func TestALB5XXDefaultCriticalThreshold(t *testing.T) {
	e := NewEvaluator(Thresholds{
		CPUWarn:      60,
		CPUCrit:      85,
		Error5XXWarn: 1,
	})

	alerts := e.Evaluate(nil, &collector.CloudWatchSnapshot{
		ALB: collector.ALBMetrics{Count5XX: 53},
	}, nil, nil, nil)
	assertAlert(t, alerts, LevelCritical, "ALB 5XX")
}

// --- Edge-triggered alert history (record onset once) ---

func TestHistoryRecordsOnsetOnceForPersistentAlert(t *testing.T) {
	// Arrange
	e := NewEvaluator(docdbConnThresholds())
	cw := docdbConnSnapshot(4989) // critical, unchanged across polls

	// Act: same condition active across three consecutive polls
	for range 3 {
		e.Evaluate(nil, cw, nil, nil, nil)
	}

	// Assert: recorded once at onset, not once per poll
	if got := len(e.History()); got != 1 {
		t.Fatalf("expected 1 history entry for a persistent alert, got %d", got)
	}
	if got := len(e.Active()); got != 1 {
		t.Fatalf("expected 1 active alert, got %d", got)
	}
}

func TestHistoryReRecordsAfterClearAndRefire(t *testing.T) {
	// Arrange
	e := NewEvaluator(docdbConnThresholds())

	// Act: fire, clear, fire again
	e.Evaluate(nil, docdbConnSnapshot(4989), nil, nil, nil) // fire
	e.Evaluate(nil, docdbConnSnapshot(10), nil, nil, nil)   // clears (below warn)
	e.Evaluate(nil, docdbConnSnapshot(4989), nil, nil, nil) // fires again

	// Assert: two distinct onset events recorded
	if got := len(e.History()); got != 2 {
		t.Fatalf("expected 2 history entries after clear+refire, got %d", got)
	}
	if got := len(e.Active()); got != 1 {
		t.Fatalf("expected 1 active alert after refire, got %d", got)
	}
}

func TestHistoryRecordsOnValueChange(t *testing.T) {
	// Arrange: warn at 80, crit at 100 — different values produce distinct keys
	e := NewEvaluator(docdbConnThresholds())

	// Act: warning level, then escalates to critical (different value + message)
	e.Evaluate(nil, docdbConnSnapshot(85), nil, nil, nil)  // warning
	e.Evaluate(nil, docdbConnSnapshot(85), nil, nil, nil)  // unchanged → skip
	e.Evaluate(nil, docdbConnSnapshot(120), nil, nil, nil) // critical → new entry

	// Assert
	if got := len(e.History()); got != 2 {
		t.Fatalf("expected 2 history entries across warn→crit change, got %d", got)
	}
}

// --- Pod-restart alerting (real restart time, age-out, eviction filter) ---

func TestPodRestartUsesActualRestartTime(t *testing.T) {
	// Arrange: a pod whose last restart happened well before detection
	e := NewEvaluator(Thresholds{PodRestartCrit: 1})
	restartedAt := time.Date(2026, 6, 21, 1, 23, 45, 0, time.Local)
	k8s := &collector.KubernetesSnapshot{Pods: []collector.PodInfo{
		{Name: "chat-rpc-aaa", Status: "Running", Restarts: 1, LastRestart: restartedAt},
	}}

	// Act
	alerts := e.Evaluate(nil, nil, k8s, nil, nil)

	// Assert: alert timestamp reflects the real restart time, not detection time
	a, ok := findAlert(alerts, "Pod Restarts")
	if !ok {
		t.Fatalf("expected Pod Restarts alert")
	}
	if !a.Time.Equal(restartedAt) {
		t.Fatalf("expected alert time %v, got %v", restartedAt, a.Time)
	}
}

func TestStaleRestartsAreAgedOut(t *testing.T) {
	// Arrange: 1h age-out window; one old restart (recovered OOMKill) and one fresh.
	e := NewEvaluator(Thresholds{PodRestartCrit: 1, PodRestartRecentWindow: time.Hour})
	now := time.Now()
	k8s := &collector.KubernetesSnapshot{Pods: []collector.PodInfo{
		{Name: "old-oomkill", Status: "Running", Restarts: 1, LastRestart: now.Add(-2 * time.Hour)},
		{Name: "fresh-restart", Status: "Running", Restarts: 1, LastRestart: now.Add(-5 * time.Minute)},
	}}

	// Act
	alerts := e.Evaluate(nil, nil, k8s, nil, nil)

	// Assert: only the recent restart is alerted
	count := 0
	for _, a := range alerts {
		if a.Metric == "Pod Restarts" {
			count++
			if a.Value != "fresh-restart: 1" {
				t.Fatalf("unexpected aged-out alert: %q", a.Value)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 recent restart alert, got %d", count)
	}
}

func TestEvictedAndFailedPodsAreNotAlerted(t *testing.T) {
	// Arrange: tombstone pods evicted off a node (phase Failed) keep a
	// restartCount of 1 but are not actively restarting.
	e := NewEvaluator(Thresholds{PodRestartCrit: 1})
	k8s := &collector.KubernetesSnapshot{Pods: []collector.PodInfo{
		{Name: "chat-rpc-evicted", Status: "Failed", Restarts: 1},
		{Name: "msg-transfer-unknown", Status: "Failed", Restarts: 1},
		{Name: "chat-rpc-pending", Status: "Pending", Restarts: 1},
		{Name: "chat-rpc-live", Status: "Running", Restarts: 2}, // the only real one
	}}

	// Act
	alerts := e.Evaluate(nil, nil, k8s, nil, nil)

	// Assert: only the Running pod is alerted
	count := 0
	for _, a := range alerts {
		if a.Metric == "Pod Restarts" {
			count++
			if a.Value != "chat-rpc-live: 2" {
				t.Fatalf("unexpected pod-restart alert for %q", a.Value)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 pod-restart alert (Running only), got %d", count)
	}
}

func TestDistinctEntitiesSharingMetricStaySeparate(t *testing.T) {
	// Arrange: two pods both restarted — same Metric/Message, different Value.
	e := NewEvaluator(Thresholds{PodRestartCrit: 1})
	k8s := &collector.KubernetesSnapshot{Pods: []collector.PodInfo{
		{Name: "chat-rpc-aaa", Status: "Running", Restarts: 1},
		{Name: "msg-transfer-bbb", Status: "Running", Restarts: 1},
	}}

	// Act: same two restarts seen across two polls
	e.Evaluate(nil, nil, k8s, nil, nil)
	e.Evaluate(nil, nil, k8s, nil, nil)

	// Assert: both pods recorded once each, not collapsed and not re-logged
	if got := len(e.History()); got != 2 {
		t.Fatalf("expected 2 history entries (one per pod), got %d", got)
	}
}
