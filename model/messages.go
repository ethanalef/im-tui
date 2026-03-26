package model

import "im-tui/collector"

// Tea messages returned by collectors.

type PrometheusMsg struct {
	Snapshot collector.PrometheusSnapshot
}

type CloudWatchMsg struct {
	Snapshot collector.CloudWatchSnapshot
}

type KubernetesMsg struct {
	Snapshot collector.KubernetesSnapshot
}

type LocustMsg struct {
	Snapshot collector.LocustSnapshot
}

type LogMsg struct {
	Snapshot collector.LogSnapshot
}

type ChatAPIMsg struct {
	Snapshot collector.ChatAPISnapshot
}

// TickMsg triggers periodic collection.
type TickMsg struct {
	Source string // "prometheus", "cloudwatch", "kubernetes", "locust"
}

// AlertMsg carries new alerts from the evaluator.
type AlertMsg struct {
	Alerts []Alert
}

type Alert struct {
	Level   string // "warning", "critical"
	Metric  string
	Value   string
	Message string
}

// WindowSizeMsg is sent on terminal resize.
type WindowSizeMsg struct {
	Width  int
	Height int
}
