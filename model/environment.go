package model

import (
	"im-tui/alert"
	"im-tui/collector"
	"im-tui/export"
)

// EnvBundle holds all collectors and config for one environment.
type EnvBundle struct {
	Config          Config
	PromCollector   *collector.PrometheusCollector
	CWCollector     *collector.CloudWatchCollector
	K8sCollector    *collector.KubernetesCollector
	LocustCollector *collector.LocustCollector
	LogCollector    *collector.LogCollector
	Evaluator       *alert.Evaluator
	Exporter        *export.Exporter
}
