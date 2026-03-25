package collector

import "sync"

// TimeSeries is a thread-safe ring buffer for sparkline data.
type TimeSeries struct {
	mu   sync.Mutex
	data []float64
	cap  int
}

func NewTimeSeries(capacity int) *TimeSeries {
	return &TimeSeries{
		data: make([]float64, 0, capacity),
		cap:  capacity,
	}
}

func (ts *TimeSeries) Push(v float64) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if len(ts.data) >= ts.cap {
		copy(ts.data, ts.data[1:])
		ts.data = ts.data[:ts.cap-1]
	}
	ts.data = append(ts.data, v)
}

func (ts *TimeSeries) Values() []float64 {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	out := make([]float64, len(ts.data))
	copy(out, ts.data)
	return out
}

func (ts *TimeSeries) Last() float64 {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if len(ts.data) == 0 {
		return 0
	}
	return ts.data[len(ts.data)-1]
}

func (ts *TimeSeries) Len() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return len(ts.data)
}

// PrometheusSnapshot holds all Prometheus metrics at a point in time.
type PrometheusSnapshot struct {
	OnlineUsers     float64
	MsgsIn5Min      float64
	SendRate        float64
	SingleChatOK    float64
	SingleChatFail  float64
	GroupChatOK     float64
	GroupChatFail   float64

	// Tier 1: msg-transfer storage pipeline (any failure = data loss risk)
	RedisInsertOK   float64 // rate(msg_insert_redis_success_total[1m])
	RedisInsertFail float64 // rate(msg_insert_redis_failed_total[1m])
	MongoInsertOK   float64 // rate(msg_insert_mongo_success_total[1m])
	MongoInsertFail float64 // rate(msg_insert_mongo_failed_total[1m])
	SeqSetFail      float64 // rate(seq_set_failed_total[1m])

	// Tier 1: push failures
	PushFail        float64 // rate(msg_offline_push_failed_total[1m])

	// Tier 2: push quality + activity
	LongTimePush    float64 // rate(msg_long_time_push_total[1m])
	UserLogin       float64 // rate(user_login_total[1m])
	UserRegister    float64 // rate(user_register_total[1m])
	API5XX          float64 // sum(rate(http_count{status=~"5.."}[1m]))
	ChatAPI5XX      float64 // sum(rate(http_count{job=~".*chat-api.*",status=~"5.."}[1m]))
	OpenIMAPI5XX    float64 // sum(rate(http_count{job=~".*openim-api.*",status=~"5.."}[1m]))

	// Tier 3: gateway-level counter (now available via ServiceMonitor)
	GatewaySendRate float64 // rate(msg_gateway_send_msg_total[1m])

	// Push pipeline metrics (invisible queue visibility)
	PushMsgInFlight     float64 // push_msg_in_flight gauge
	PushProcessingP95   float64 // p95 of push_msg_processing_duration_seconds (seconds)
	PushGrpcDeliveryP95 float64 // p95 of push_grpc_delivery_duration_seconds (seconds)
	GatewayWsQueueP95   float64 // p95 of gateway_ws_write_queue_len (queue depth)
	GatewayWsWriteP95   float64 // p95 of gateway_ws_write_duration_seconds (seconds)

	// Pipeline latency histograms (NEW — requires upgrade version deployed)
	KafkaProduceP95       float64 // p95 of kafka_produce_duration_seconds (seconds)
	TransferBatchP95      float64 // p95 of msg_transfer_batch_duration_seconds (seconds)
	TransferRedisCacheP95 float64 // p95 of msg_transfer_redis_cache_duration_seconds (seconds)
	TransferMongoWriteP95 float64 // p95 of msg_transfer_mongo_write_duration_seconds (seconds)
	PushGroupMemberP95    float64 // p95 of push_group_member_count (member count)
	GatewayEncodeP95      float64 // p95 of gateway_msg_encode_duration_seconds (seconds)
	E2EDeliveryGroupP95   float64 // p95 of message_e2e_delivery_seconds{session_type="group"} (seconds)
	E2EDeliverySingleP95  float64 // p95 of message_e2e_delivery_seconds{session_type="single"} (seconds)
	GatewayBatchPushP95   float64 // p95 of gateway_batch_push_duration_seconds (seconds)
	GatewayBatchPushSizeP95 float64 // p95 of gateway_batch_push_user_count (user count)

	// msg-transfer health: production vs consumption rate delta
	// Positive = lag growing, negative = catching up, zero = keeping pace
	MsgLagGrowthRate float64

	PodMetrics      []PodMetric
	Err             error
}

type PodMetric struct {
	Pod          string
	Goroutines   float64
	MemAlloc     float64 // go_memstats_alloc_bytes
	HeapInUse    float64 // go_memstats_heap_inuse_bytes (RSS leak indicator)
	HeapReleased float64 // go_memstats_heap_released_bytes (low = RSS not returned to OS)
	OnlineUsers  float64 // online_user_num gauge (msg-gateway only)
}

// CloudWatchSnapshot holds all AWS metrics.
type CloudWatchSnapshot struct {
	DocDB DocDBMetrics
	RDS   RDSMetrics
	Redis []RedisNodeMetrics
	ALB   ALBMetrics
	MSK   MSKMetrics
	Err   error
}

// MSKMetrics holds Kafka consumer group lag from CloudWatch.
type MSKMetrics struct {
	ConsumerLag []ConsumerGroupLag
	TotalLag    float64 // sum of all consumer group lags
}

type ConsumerGroupLag struct {
	Group string
	Topic string
	Lag   float64 // SumOffsetLag
}

type DocDBMetrics struct {
	CPUPercent      float64
	Connections     float64
	VolumeUsed      float64 // bytes
	InsertOps       float64
	QueryOps        float64
	UpdateOps       float64
	DeleteOps       float64
	CursorsTimedOut float64 // DatabaseCursorsTimedOut
	ReadIOPS        float64 // ReadIOPS
	WriteIOPS       float64 // WriteIOPS
}

type RDSMetrics struct {
	CPUPercent    float64
	Connections   float64
	FreeMemory    float64 // bytes
	ReadLatency   float64 // seconds
	WriteLatency  float64 // seconds
	DiskQueue     float64
	ReadIOPS      float64
	WriteIOPS     float64
}

type RedisNodeMetrics struct {
	NodeID        string
	CPUPercent    float64
	MemoryPercent float64
	HitRate       float64
	Evictions     float64
	Connections   float64
	GetTypeCmds   float64 // GetTypeCmds (read ops/sec)
	SetTypeCmds   float64 // SetTypeCmds (write ops/sec)
}

type ALBMetrics struct {
	ResponseTimeP99 float64 // seconds
	Count5XX        float64
	ActiveConns     float64
	RequestCount    float64
}

// KubernetesSnapshot holds all kubectl-sourced data.
type KubernetesSnapshot struct {
	Pods     []PodInfo
	HPAs     []HPAInfo
	Events   []EventInfo
	Err      error
}

type PodInfo struct {
	Name       string
	Status     string
	Ready      string
	Restarts   int
	Age        string
	CPUUsage   string  // from kubectl top (e.g. "3m")
	MemUsage   string  // from kubectl top (e.g. "256Mi")
	CPURequest string  // from pod spec resources.requests.cpu
	CPULimit   string  // from pod spec resources.limits.cpu
	MemRequest string  // from pod spec resources.requests.memory
	MemLimit   string  // from pod spec resources.limits.memory
	CPUPercent float64 // usage/limit * 100 (0 if unknown)
	MemPercent float64 // usage/limit * 100 (0 if unknown)
}

type HPAInfo struct {
	Name        string
	Targets     string
	MinReplicas int
	MaxReplicas int
	Current     int
}

type EventInfo struct {
	Type      string
	Reason    string
	Object    string
	Message   string
	Age       string
	Count     int
}

// InfraSpecs holds static infrastructure specifications fetched once at startup.
type InfraSpecs struct {
	DocDB DocDBSpec
	RDS   RDSSpec
	Redis []RedisNodeSpec
}

type DocDBSpec struct {
	ShardCount    int32
	ShardCapacity int32 // vCPUs per shard
}

type RDSSpec struct {
	InstanceClass    string
	Engine           string
	EngineVersion    string
	AllocatedStorage int32  // GiB
	MaxStorage       int32  // GiB (0 = autoscaling disabled)
	MultiAZ          bool
	StorageType      string // gp3, io1, etc.
}

type RedisNodeSpec struct {
	NodeID        string
	NodeType      string
	Engine        string
	EngineVersion string
}

// LocustSnapshot holds Locust load test data.
type LocustSnapshot struct {
	Available   bool
	State       string
	UserCount   int
	TotalRPS    float64
	FailRatio   float64
	Endpoints   []LocustEndpoint
	Failures    []LocustFailure
	Err         error
}

type LocustEndpoint struct {
	Method         string
	Name           string
	NumRequests    int
	NumFailures    int
	RPS            float64
	FailPercent    float64
	AvgResponseTime float64
	P50            float64
	P95            float64
	P99            float64
	MaxResponseTime float64
}

type LocustFailure struct {
	Method    string
	Name      string
	Error     string
	Occurrences int
}
