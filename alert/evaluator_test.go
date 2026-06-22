package alert

import (
	"testing"

	"im-tui/collector"
)

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
