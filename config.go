package main

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Environment  string           `yaml:"environment"`
	Namespace    string           `yaml:"namespace"`
	Kubeconfig   string           `yaml:"kubeconfig"`
	Prometheus   PrometheusConfig `yaml:"prometheus"`
	CloudWatch   CloudWatchConfig `yaml:"cloudwatch"`
	Kubernetes   KubernetesConfig `yaml:"kubernetes"`
	Locust       LocustConfig     `yaml:"locust"`
	Logs         LogConfig        `yaml:"logs"`
	AWS          AWSConfig        `yaml:"aws"`
	Thresholds   ThresholdConfig  `yaml:"thresholds"`
	Capacity     CapacityConfig   `yaml:"capacity"`
	Export       ExportConfig     `yaml:"export"`
	SparklineCap int              `yaml:"sparkline_capacity"`
}

type CapacityConfig struct {
	MaxOnlineUsers            float64 `yaml:"max_online_users"`
	MaxInboundMsgPerSec       float64 `yaml:"max_inbound_msg_per_sec"`
	MaxBackendFanoutMsgPerSec float64 `yaml:"max_backend_fanout_msg_per_sec"`
}

type LogConfig struct {
	Interval time.Duration `yaml:"interval"`
	Services []string      `yaml:"services"`
	SinceSec int           `yaml:"since_seconds"`
}

type ExportConfig struct {
	Enabled  bool          `yaml:"enabled"`
	Path     string        `yaml:"path"`
	Interval time.Duration `yaml:"interval"`
}

type PrometheusConfig struct {
	URL       string        `yaml:"url"`       // direct URL (overrides service-based port-forward)
	Namespace string        `yaml:"namespace"` // k8s namespace of prometheus
	Service   string        `yaml:"service"`   // k8s service name (e.g. svc/kube-prometheus-stack-prometheus)
	Port      int           `yaml:"port"`      // remote port
	Interval  time.Duration `yaml:"interval"`
}

type CloudWatchConfig struct {
	Region   string        `yaml:"region"`
	Interval time.Duration `yaml:"interval"`
}

type KubernetesConfig struct {
	Interval       time.Duration `yaml:"interval"`
	IgnorePrefixes []string      `yaml:"ignore_prefixes"`
}

type LocustConfig struct {
	URL      string        `yaml:"url"`
	Interval time.Duration `yaml:"interval"`
}

type AWSConfig struct {
	DocDB       DocDBConfig       `yaml:"docdb"`
	RDS         RDSConfig         `yaml:"rds"`
	ElastiCache ElastiCacheConfig `yaml:"elasticache"`
	ALB         ALBConfig         `yaml:"alb"`
	MSK         MSKConfig         `yaml:"msk"`
}

type MSKConfig struct {
	ClusterName    string             `yaml:"cluster_name"`
	AWSProfile     string             `yaml:"aws_profile"`
	ConsumerGroups []MSKConsumerGroup `yaml:"consumer_groups"`
}

type MSKConsumerGroup struct {
	Group string `yaml:"group"`
	Topic string `yaml:"topic"`
}

type DocDBConfig struct {
	ClusterID          string `yaml:"cluster_id"`
	ClusterName        string `yaml:"cluster_name"`
	ShardCount         int32  `yaml:"shard_count"`
	ShardInstanceCount int32  `yaml:"shard_instance_count"`
	ShardCapacity      int32  `yaml:"shard_capacity"` // vCPUs per shard
}

type RDSConfig struct {
	InstanceID string `yaml:"instance_id"`
}

type ElastiCacheConfig struct {
	// ReplicationGroupID, when set, discovers member nodes + roles live via
	// DescribeReplicationGroups (adapts to 1P4R / 1P5R automatically).
	// Nodes is used as a static fallback when discovery is empty or fails.
	ReplicationGroupID string   `yaml:"replication_group_id"`
	Nodes              []string `yaml:"nodes"`
}

type ALBConfig struct {
	LoadBalancers []string `yaml:"load_balancers"`
}

type ThresholdConfig struct {
	CPUWarn          float64 `yaml:"cpu_warn"`
	CPUCrit          float64 `yaml:"cpu_crit"`
	MemoryWarn       float64 `yaml:"memory_warn"`
	Error5XXWarn     int     `yaml:"error_5xx_warn"`
	Error5XXCrit     int     `yaml:"error_5xx_crit"`
	PodRestartCrit   int     `yaml:"pod_restart_crit"`
	LocustFailWarn   float64 `yaml:"locust_fail_warn"`
	ResponseTimeWarn int     `yaml:"response_time_warn_ms"`
	// PodRestartWindowMin ignores restarts older than N minutes (default 60; negative disables).
	PodRestartWindowMin int `yaml:"pod_restart_window_min"`

	// DocDB Elastic
	DocDBConnWarn float64 `yaml:"docdb_conn_warn"`
	DocDBConnCrit float64 `yaml:"docdb_conn_crit"`

	// RDS MySQL
	RDSLatencyWarnMs float64 `yaml:"rds_latency_warn_ms"`
	RDSLatencyCritMs float64 `yaml:"rds_latency_crit_ms"`
	RDSDiskQueueWarn float64 `yaml:"rds_disk_queue_warn"`
	RDSDiskQueueCrit float64 `yaml:"rds_disk_queue_crit"`

	// ElastiCache Redis
	RedisEvictWarn float64 `yaml:"redis_evict_warn"`
	RedisEvictCrit float64 `yaml:"redis_evict_crit"`
	RedisCPUWarn   float64 `yaml:"redis_cpu_warn"` // EngineCPU% warning (default 70)
	RedisCPUCrit   float64 `yaml:"redis_cpu_crit"` // EngineCPU% critical (default 85)

	// Goroutines
	GoroutineWarn float64 `yaml:"goroutine_warn"`
	GoroutineCrit float64 `yaml:"goroutine_crit"`

	// Kafka consumer lag
	KafkaLagWarn float64 `yaml:"kafka_lag_warn"`
	KafkaLagCrit float64 `yaml:"kafka_lag_crit"`

	// Push quality rates
	PushFailWarnPerSec     float64 `yaml:"push_fail_warn_per_sec"`
	PushFailCritPerSec     float64 `yaml:"push_fail_crit_per_sec"`
	LongTimePushWarnPerSec float64 `yaml:"long_time_push_warn_per_sec"`
	LongTimePushCritPerSec float64 `yaml:"long_time_push_crit_per_sec"`

	// Pipeline latency P95 thresholds (upgrade version metrics)
	E2EGroupWarnS      float64 `yaml:"e2e_group_warn_s"`      // group delivery P95 warning (seconds)
	E2EGroupCritS      float64 `yaml:"e2e_group_crit_s"`      // group delivery P95 critical (seconds)
	E2ESingleWarnS     float64 `yaml:"e2e_single_warn_s"`     // single delivery P95 warning (seconds)
	E2ESingleCritS     float64 `yaml:"e2e_single_crit_s"`     // single delivery P95 critical (seconds)
	GatewayEncodeWarnS float64 `yaml:"gw_encode_warn_s"`      // per-msg encode P95 warning (seconds)
	GatewayEncodeCritS float64 `yaml:"gw_encode_crit_s"`      // per-msg encode P95 critical (seconds)
	TransferBatchWarnS float64 `yaml:"transfer_batch_warn_s"` // msg-transfer batch P95 warning (seconds)
	TransferBatchCritS float64 `yaml:"transfer_batch_crit_s"` // msg-transfer batch P95 critical (seconds)

	// Spike detection — alerts on sudden rapid rises
	SpikeRisePct    float64 `yaml:"spike_rise_pct"`    // % increase over baseline → warning (2x → critical, 0 = disabled)
	SpikeMinSamples int     `yaml:"spike_min_samples"` // min data points before detection activates
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	cfg := &Config{
		SparklineCap: 60,
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Defaults
	if cfg.Namespace == "" {
		cfg.Namespace = "im-sit"
	}
	if cfg.Prometheus.Interval == 0 {
		cfg.Prometheus.Interval = 3 * time.Second
	}
	if cfg.CloudWatch.Interval == 0 {
		cfg.CloudWatch.Interval = 60 * time.Second
	}
	if cfg.Kubernetes.Interval == 0 {
		cfg.Kubernetes.Interval = 3 * time.Second
	}
	if cfg.Locust.Interval == 0 {
		cfg.Locust.Interval = 5 * time.Second
	}
	if cfg.Logs.Interval == 0 {
		cfg.Logs.Interval = 5 * time.Second
	}
	if cfg.Logs.SinceSec == 0 {
		cfg.Logs.SinceSec = 60
	}
	if len(cfg.Logs.Services) == 0 {
		cfg.Logs.Services = []string{
			"msg-gateway", "msg-transfer", "openim-push",
			"openim-auth", "openim-conversation", "openim-msg", "chat-api",
		}
	}
	if cfg.SparklineCap == 0 {
		cfg.SparklineCap = 60
	}
	if cfg.Thresholds.Error5XXCrit == 0 {
		cfg.Thresholds.Error5XXCrit = 10
	}

	// Pod-restart age-out window (minutes). 0 = unset → default 60; set negative to disable.
	if cfg.Thresholds.PodRestartWindowMin == 0 {
		cfg.Thresholds.PodRestartWindowMin = 60
	}

	// ElastiCache: discovery is opt-in per environment via replication_group_id.
	// (No global default — each env points at a different cache cluster. PROD sets
	//  replication_group_id: im-cache-prod-fallback-20260330 in config-prod.yaml.)
	// When unset, the static `nodes` list is used as-is.

	// Redis EngineCPU thresholds (percent)
	if cfg.Thresholds.RedisCPUWarn == 0 {
		cfg.Thresholds.RedisCPUWarn = 70
	}
	if cfg.Thresholds.RedisCPUCrit == 0 {
		cfg.Thresholds.RedisCPUCrit = 85
	}

	// Export defaults
	if cfg.Export.Path == "" {
		cfg.Export.Path = "im-tui-export.jsonl"
	}
	if cfg.Export.Interval == 0 {
		cfg.Export.Interval = 10 * time.Second
	}

	return cfg, nil
}
