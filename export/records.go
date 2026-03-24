package export

import "time"

// Record type constants used in the "type" JSON field.
const (
	TypeSessionStart = "session_start"
	TypeSnapshot     = "snapshot"
	TypeSessionEnd   = "session_end"
)

// SessionStartRecord is written once when the TUI starts.
type SessionStartRecord struct {
	Type        string            `json:"type"`
	Timestamp   time.Time         `json:"ts"`
	Environment string            `json:"environment"`
	Namespace   string            `json:"namespace"`
	Collectors  CollectorStatus   `json:"collectors"`
	Intervals   IntervalConfig    `json:"intervals"`
	Thresholds  ThresholdConfig   `json:"thresholds"`
}

type CollectorStatus struct {
	Prometheus bool `json:"prometheus"`
	CloudWatch bool `json:"cloudwatch"`
	Kubernetes bool `json:"kubernetes"`
	Locust     bool `json:"locust"`
}

type IntervalConfig struct {
	PrometheusSec int `json:"prometheus_s"`
	CloudWatchSec int `json:"cloudwatch_s"`
	KubernetesSec int `json:"kubernetes_s"`
	LocustSec     int `json:"locust_s"`
	ExportSec     int `json:"export_s"`
}

type ThresholdConfig struct {
	CPUWarn          float64 `json:"cpu_warn"`
	CPUCrit          float64 `json:"cpu_crit"`
	MemoryWarn       float64 `json:"memory_warn"`
	Error5XXWarn     int     `json:"error_5xx_warn"`
	PodRestartCrit   int     `json:"pod_restart_crit"`
	LocustFailWarn   float64 `json:"locust_fail_warn"`
	ResponseTimeWarn int     `json:"response_time_warn_ms"`
}

// SnapshotRecord is written every export interval with consolidated metrics.
type SnapshotRecord struct {
	Type      string         `json:"type"`
	Timestamp time.Time      `json:"ts"`
	Seq       int            `json:"seq"`
	Sources   SourceStatus   `json:"sources"`
	App       *AppMetrics    `json:"app,omitempty"`
	Infra     *InfraMetrics  `json:"infra,omitempty"`
	K8s       *K8sMetrics    `json:"k8s,omitempty"`
	Locust    *LocustMetrics `json:"locust,omitempty"`
	Logs      *LogSummary    `json:"logs,omitempty"`
	Alerts    AlertMetrics   `json:"alerts"`
}

type SourceStatus struct {
	Prometheus string `json:"prometheus"`
	CloudWatch string `json:"cloudwatch"`
	Kubernetes string `json:"kubernetes"`
	Locust     string `json:"locust"`
	Logs       string `json:"logs"`
}

// LogSummary captures per-service error counts for export.
type LogSummary struct {
	Services []ServiceLogRecord `json:"services"`
}

// ServiceLogRecord holds error counts for a single service.
type ServiceLogRecord struct {
	Name     string `json:"name"`
	Errors   int    `json:"errors"`
	Fails    int    `json:"fails"`
	Timeouts int    `json:"timeouts"`
	Panics   int    `json:"panics"`
}

type AppMetrics struct {
	OnlineUsers    float64 `json:"online_users"`
	Msgs5Min       float64 `json:"msgs_5min"`
	SendRate       float64 `json:"send_rate"`
	SingleChatOK   float64 `json:"single_chat_ok"`
	SingleChatFail float64 `json:"single_chat_fail"`
	GroupChatOK    float64 `json:"group_chat_ok"`
	GroupChatFail  float64 `json:"group_chat_fail"`
	PodCount       int     `json:"pod_count"`

	// msg-transfer storage pipeline
	RedisInsertOK   float64 `json:"redis_insert_ok"`
	RedisInsertFail float64 `json:"redis_insert_fail"`
	MongoInsertOK   float64 `json:"mongo_insert_ok"`
	MongoInsertFail float64 `json:"mongo_insert_fail"`
	SeqSetFail      float64 `json:"seq_set_fail"`

	// Push + activity
	PushFail        float64 `json:"push_fail"`
	LongTimePush    float64 `json:"long_time_push"`
	UserLogin       float64 `json:"user_login"`
	UserRegister    float64 `json:"user_register"`
	API5XX          float64 `json:"api_5xx"`
	ChatAPI5XX      float64 `json:"chat_api_5xx"`
	OpenIMAPI5XX    float64 `json:"openim_api_5xx"`
	GatewaySendRate float64 `json:"gateway_send_rate"`

	// Push pipeline (invisible queue visibility)
	PushMsgInFlight     float64 `json:"push_in_flight"`
	PushProcessingP95   float64 `json:"push_processing_p95_s"`
	PushGrpcDeliveryP95 float64 `json:"push_grpc_delivery_p95_s"`
	GatewayWsQueueP95   float64 `json:"gateway_ws_queue_p95"`

	// Pipeline latency P95 (upgrade version metrics)
	KafkaProduceP95       float64 `json:"kafka_produce_p95_s,omitempty"`
	TransferBatchP95      float64 `json:"transfer_batch_p95_s,omitempty"`
	TransferRedisCacheP95 float64 `json:"transfer_redis_cache_p95_s,omitempty"`
	TransferMongoWriteP95 float64 `json:"transfer_mongo_write_p95_s,omitempty"`
	PushGroupMemberP95    float64 `json:"push_group_member_p95,omitempty"`
	GatewayEncodeP95      float64 `json:"gateway_encode_p95_s,omitempty"`
	E2EDeliveryGroupP95   float64 `json:"e2e_delivery_group_p95_s,omitempty"`
	E2EDeliverySingleP95  float64 `json:"e2e_delivery_single_p95_s,omitempty"`
	GatewayBatchPushP95   float64 `json:"gateway_batch_push_p95_s,omitempty"`
	GatewayBatchPushSizeP95 float64 `json:"gateway_batch_push_size_p95,omitempty"`

	// Gateway dead connection health (per gateway pod)
	GatewayHealth []GatewayHealthRecord `json:"gateway_health,omitempty"`
}

// GatewayHealthRecord captures per-gateway-pod health for dead connection leak detection.
type GatewayHealthRecord struct {
	Pod          string  `json:"pod"`
	Status       string  `json:"status"` // OK, WARN, DEGRADED
	Goroutines   float64 `json:"goroutines"`
	HeapInUseMB  float64 `json:"heap_inuse_mb"`
	HeapReleasedMB float64 `json:"heap_released_mb"`
	MemAllocMB   float64 `json:"mem_alloc_mb"`
}

type InfraMetrics struct {
	DocDBCPUPct     float64 `json:"docdb_cpu_pct"`
	DocDBConns      float64 `json:"docdb_conns"`
	DocDBCursors    float64 `json:"docdb_cursors_timed_out"`
	DocDBReadIOPS   float64 `json:"docdb_read_iops"`
	DocDBWriteIOPS  float64 `json:"docdb_write_iops"`
	RDSCPUPct       float64 `json:"rds_cpu_pct"`
	RDSConns        float64 `json:"rds_conns"`
	RDSFreeMemBytes float64 `json:"rds_free_mem_bytes"`
	RDSReadIOPS     float64 `json:"rds_read_iops"`
	RDSWriteIOPS    float64 `json:"rds_write_iops"`
	ALBP99Ms        float64 `json:"alb_p99_ms"`
	ALB5XX          float64 `json:"alb_5xx"`
	ALBActiveConns  float64           `json:"alb_active_conns"`
	ALBRequestCount float64           `json:"alb_request_count"`
	KafkaTotalLag   float64           `json:"kafka_total_lag"`
	KafkaLagByGroup []KafkaLagRecord  `json:"kafka_lag_by_group,omitempty"`
}

type KafkaLagRecord struct {
	Group string  `json:"group"`
	Topic string  `json:"topic"`
	Lag   float64 `json:"lag"`
}

type K8sMetrics struct {
	TotalPods     int           `json:"total_pods"`
	RunningPods   int           `json:"running_pods"`
	TotalRestarts int           `json:"total_restarts"`
	WarningEvents int           `json:"warning_events"`
	HPAs          []HPASnapshot `json:"hpas,omitempty"`
}

type HPASnapshot struct {
	Name    string `json:"name"`
	Current int    `json:"current"`
	Min     int    `json:"min"`
	Max     int    `json:"max"`
}

type LocustMetrics struct {
	State     string  `json:"state"`
	Users     int     `json:"users"`
	RPS       float64 `json:"rps"`
	FailRatio float64 `json:"fail_ratio_pct"`
}

type AlertMetrics struct {
	ActiveCount int           `json:"active_count"`
	Active      []AlertRecord `json:"active,omitempty"`
}

// AlertRecord is a single alert included in snapshot data.
type AlertRecord struct {
	Level   string `json:"level"`
	Metric  string `json:"metric"`
	Value   string `json:"value"`
	Message string `json:"message"`
}

// SessionEndRecord is written on graceful exit.
type SessionEndRecord struct {
	Type          string         `json:"type"`
	Timestamp     time.Time      `json:"ts"`
	Duration      string         `json:"duration"`
	DurationSec   float64        `json:"duration_s"`
	SnapshotCount int            `json:"snapshot_count"`
	Summary       SessionSummary `json:"summary"`
}

type SessionSummary struct {
	App    AppSummary    `json:"app"`
	Infra  InfraSummary  `json:"infra"`
	K8s    K8sSummary    `json:"k8s"`
	Alerts AlertSummary  `json:"alerts"`
}

type AppSummary struct {
	OnlineUsers MinMaxAvg `json:"online_users"`
	Msgs5Min    MinMaxAvg `json:"msgs_5min"`
	SendRate    MinMaxAvg `json:"send_rate"`
}

type InfraSummary struct {
	DocDBCPUPct    MinMaxAvg `json:"docdb_cpu_pct"`
	DocDBReadIOPS  MinMaxAvg `json:"docdb_read_iops"`
	DocDBWriteIOPS MinMaxAvg `json:"docdb_write_iops"`
	RDSCPUPct      MinMaxAvg `json:"rds_cpu_pct"`
	RDSReadIOPS    MinMaxAvg `json:"rds_read_iops"`
	RDSWriteIOPS   MinMaxAvg `json:"rds_write_iops"`
	ALBP99Ms       MinMaxAvg `json:"alb_p99_ms"`
	ALB5XXTotal    float64   `json:"alb_5xx_total"`
}

type K8sSummary struct {
	PeakPods      int              `json:"peak_pods"`
	PeakRestarts  int              `json:"peak_restarts"`
	ScalingEvents []HPAScaleEvent  `json:"scaling_events,omitempty"`
}

type HPAScaleEvent struct {
	Name      string    `json:"name"`
	From      int       `json:"from"`
	To        int       `json:"to"`
	Timestamp time.Time `json:"ts"`
}

type AlertSummary struct {
	TotalFired int            `json:"total_fired"`
	BySeverity map[string]int `json:"by_severity"`
	ByMetric   map[string]int `json:"by_metric"`
}

// MinMaxAvg holds running statistics for a single metric.
type MinMaxAvg struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
	Avg float64 `json:"avg"`
}
