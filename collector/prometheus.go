package collector

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"time"
)

type PrometheusCollector struct {
	baseURL   string
	client    *http.Client
	recover   func() error
	recoverMu sync.Mutex
}

func NewPrometheusCollector(baseURL string, recoverers ...func() error) *PrometheusCollector {
	var recover func() error
	if len(recoverers) > 0 {
		recover = recoverers[0]
	}
	return &PrometheusCollector{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
		recover: recover,
	}
}

func (p *PrometheusCollector) Collect(namespace string) PrometheusSnapshot {
	snap := PrometheusSnapshot{}

	ns := namespace

	// Ordered queries — each entry maps a name to a PromQL expression.
	// Fork-specific metrics (sent_msg_count_in_5_min, msg_gateway_send_msg_total)
	// are replaced with computed equivalents from upstream v3.8.3 counters.
	type namedQuery struct {
		name     string
		query    string
		required bool
	}
	queries := []namedQuery{
		{"online_users", `sum(online_user_num{namespace="` + ns + `",job="msg-gateway"})`, true},
		{"online_conns", `sum(online_user_conn_num{namespace="` + ns + `",job="msg-gateway"})`, false},
		// Message processing counters — old fork omits _total suffix, upgrade has it.
		// Use __name__ regex to match both: "metric" and "metric_total".
		// NOTE: group_chat_msg_process_success is registered but NEVER incremented;
		// all group msgs use work_super_group_chat_msg_process_success instead.
		{"msgs_5min", `(sum(rate({__name__=~"single_chat_msg_process_success(_total)?",namespace="` + ns + `"}[5m])) or vector(0)) + (sum(rate({__name__=~"work_super_group_chat_msg_process_success(_total)?",namespace="` + ns + `"}[5m])) or vector(0))`, false},
		{"send_rate", `(sum(rate({__name__=~"single_chat_msg_process_success(_total)?",namespace="` + ns + `"}[1m])) or vector(0)) + (sum(rate({__name__=~"work_super_group_chat_msg_process_success(_total)?",namespace="` + ns + `"}[1m])) or vector(0))`, false},
		{"single_chat_ok", `sum(rate({__name__=~"single_chat_msg_process_success(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"single_chat_fail", `sum(rate({__name__=~"single_chat_msg_process_failed(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"group_chat_ok", `sum(rate({__name__=~"work_super_group_chat_msg_process_success(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"group_chat_fail", `sum(rate({__name__=~"work_super_group_chat_msg_process_failed(_total)?",namespace="` + ns + `"}[1m]))`, false},
		// Tier 1: msg-transfer storage pipeline
		{"redis_insert_ok", `sum(rate({__name__=~"msg_insert_redis_success(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"redis_insert_fail", `sum(rate({__name__=~"msg_insert_redis_failed(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"mongo_insert_ok", `sum(rate({__name__=~"msg_insert_mongo_success(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"mongo_insert_fail", `sum(rate({__name__=~"msg_insert_mongo_failed(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"seq_set_fail", `sum(rate({__name__=~"seq_set_failed(_total)?",namespace="` + ns + `"}[1m]))`, false},
		// Tier 1: push failures
		{"push_fail", `sum(rate({__name__=~"msg_offline_push_failed(_total)?",namespace="` + ns + `"}[1m]))`, false},
		// Tier 2: push quality + activity
		{"long_time_push", `sum(rate({__name__=~"msg_long_time_push(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"user_login", `sum(rate({__name__=~"user_login(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"user_register", `sum(rate({__name__=~"user_register(_total)?",namespace="` + ns + `"}[1m]))`, false},
		// NOTE: metric names are http_count and api_count (NO _total suffix)
		{"api_5xx", `sum(rate(http_count{namespace="` + ns + `",status=~"5.."}[1m]))`, false},
		{"chat_api_5xx", `sum(rate(http_count{namespace="` + ns + `",job=~".*chat-api.*",status=~"5.."}[1m]))`, false},
		{"openim_api_5xx", `sum(rate(http_count{namespace="` + ns + `",job=~".*openim-api.*",status=~"5.."}[1m]))`, false},
		// Gateway-level send counter (now available via ServiceMonitor)
		{"gateway_send_rate", `sum(rate(msg_gateway_send_msg_total{namespace="` + ns + `"}[1m]))`, false},
		// Push pipeline metrics (invisible queue visibility)
		{"push_in_flight", `sum(push_msg_in_flight{namespace="` + ns + `"})`, false},
		{"push_processing_p95", `histogram_quantile(0.95, sum(rate(push_msg_processing_duration_seconds_bucket{namespace="` + ns + `"}[1m])) by (le))`, false},
		{"push_grpc_p95", `histogram_quantile(0.95, sum(rate(push_grpc_delivery_duration_seconds_bucket{namespace="` + ns + `"}[1m])) by (le))`, false},
		{"gw_ws_queue_p95", `histogram_quantile(0.95, sum(rate(gateway_ws_write_queue_len_bucket{namespace="` + ns + `"}[1m])) by (le))`, false},
		{"push_zombie_candidates", `sum(rate(push_zombie_filter_candidates_total{namespace="` + ns + `"}[1m]))`, false},
		{"push_zombie_dropped", `sum(rate(push_zombie_filter_dropped_total{namespace="` + ns + `"}[1m]))`, false},
		{"push_zombie_kept", `sum(rate(push_zombie_filter_kept_total{namespace="` + ns + `"}[1m]))`, false},
		{"push_zombie_unknown", `sum(rate(push_zombie_filter_unknown_total{namespace="` + ns + `"}[1m]))`, false},
		{"push_zombie_fail_open", `sum(rate(push_zombie_filter_fail_open_total{namespace="` + ns + `"}[1m]))`, false},
		{"push_zombie_cache_hit", `sum(rate(push_zombie_filter_cache_hit_total{namespace="` + ns + `"}[1m]))`, false},
		{"push_zombie_cache_miss", `sum(rate(push_zombie_filter_cache_miss_total{namespace="` + ns + `"}[1m]))`, false},
		{"push_zombie_cache_error", `sum(rate(push_zombie_filter_cache_error_total{namespace="` + ns + `"}[1m]))`, false},
		{"push_zombie_db_lookup", `sum(rate(push_zombie_filter_db_lookup_total{namespace="` + ns + `"}[1m]))`, false},
		{"push_zombie_cache_write_failed", `sum(rate(push_zombie_filter_cache_write_total{namespace="` + ns + `",result="failed"}[1m]))`, false},
		// Pipeline latency histograms (upgrade version only — gracefully ignored if metrics absent)
		{"kafka_produce_p95", `histogram_quantile(0.95, sum(rate(kafka_produce_duration_seconds_bucket{namespace="` + ns + `"}[1m])) by (le))`, false},
		{"transfer_batch_p95", `histogram_quantile(0.95, sum(rate(msg_transfer_batch_duration_seconds_bucket{namespace="` + ns + `"}[1m])) by (le))`, false},
		{"transfer_redis_p95", `histogram_quantile(0.95, sum(rate(msg_transfer_redis_cache_duration_seconds_bucket{namespace="` + ns + `"}[1m])) by (le))`, false},
		{"transfer_mongo_p95", `histogram_quantile(0.95, sum(rate(msg_transfer_mongo_write_duration_seconds_bucket{namespace="` + ns + `"}[1m])) by (le))`, false},
		{"push_group_size_p95", `histogram_quantile(0.95, sum(rate(push_group_member_count_bucket{namespace="` + ns + `"}[1m])) by (le))`, false},
		{"gw_encode_p95", `histogram_quantile(0.95, sum(rate(gateway_msg_encode_duration_seconds_bucket{namespace="` + ns + `"}[1m])) by (le))`, false},
		{"e2e_group_p95", `histogram_quantile(0.95, sum(rate(message_e2e_delivery_seconds_bucket{namespace="` + ns + `",session_type="group"}[1m])) by (le))`, false},
		{"e2e_single_p95", `histogram_quantile(0.95, sum(rate(message_e2e_delivery_seconds_bucket{namespace="` + ns + `",session_type="single"}[1m])) by (le))`, false},
		{"gw_batch_push_p95", `histogram_quantile(0.95, sum(rate(gateway_batch_push_duration_seconds_bucket{namespace="` + ns + `"}[1m])) by (le))`, false},
		{"gw_batch_push_size_p95", `histogram_quantile(0.95, sum(rate(gateway_batch_push_user_count_bucket{namespace="` + ns + `"}[1m])) by (le))`, false},
		{"gw_ws_write_p95", `histogram_quantile(0.95, sum(rate(gateway_ws_write_duration_seconds_bucket{namespace="` + ns + `"}[1m])) by (le))`, false},
	}

	for _, q := range queries {
		val, found, err := p.queryScalar(q.query)
		if err != nil {
			if q.required {
				snap.Err = fmt.Errorf("prometheus required query %q failed: %w", q.name, err)
				return snap
			}
			continue
		}
		if !found {
			if q.required {
				snap.Err = fmt.Errorf("prometheus required query %q returned no data", q.name)
				return snap
			}
			continue
		}
		switch q.name {
		case "online_users":
			snap.OnlineUsers = val
		case "online_conns":
			snap.OnlineConns = val
		case "msgs_5min":
			snap.MsgsIn5Min = val
		case "send_rate":
			snap.SendRate = val
		case "single_chat_ok":
			snap.SingleChatOK = val
		case "single_chat_fail":
			snap.SingleChatFail = val
		case "group_chat_ok":
			snap.GroupChatOK = val
		case "group_chat_fail":
			snap.GroupChatFail = val
		case "redis_insert_ok":
			snap.RedisInsertOK = val
		case "redis_insert_fail":
			snap.RedisInsertFail = val
		case "mongo_insert_ok":
			snap.MongoInsertOK = val
		case "mongo_insert_fail":
			snap.MongoInsertFail = val
		case "seq_set_fail":
			snap.SeqSetFail = val
		case "push_fail":
			snap.PushFail = val
		case "long_time_push":
			snap.LongTimePush = val
		case "user_login":
			snap.UserLogin = val
		case "user_register":
			snap.UserRegister = val
		case "api_5xx":
			snap.API5XX = val
		case "chat_api_5xx":
			snap.ChatAPI5XX = val
		case "openim_api_5xx":
			snap.OpenIMAPI5XX = val
		case "gateway_send_rate":
			snap.GatewaySendRate = val
		case "push_in_flight":
			snap.PushMsgInFlight = val
		case "push_processing_p95":
			snap.PushProcessingP95 = val
		case "push_grpc_p95":
			snap.PushGrpcDeliveryP95 = val
		case "gw_ws_queue_p95":
			snap.GatewayWsQueueP95 = val
		case "push_zombie_candidates":
			snap.PushZombieCandidates = val
		case "push_zombie_dropped":
			snap.PushZombieDropped = val
		case "push_zombie_kept":
			snap.PushZombieKept = val
		case "push_zombie_unknown":
			snap.PushZombieUnknown = val
		case "push_zombie_fail_open":
			snap.PushZombieFailOpen = val
		case "push_zombie_cache_hit":
			snap.PushZombieCacheHit = val
		case "push_zombie_cache_miss":
			snap.PushZombieCacheMiss = val
		case "push_zombie_cache_error":
			snap.PushZombieCacheError = val
		case "push_zombie_db_lookup":
			snap.PushZombieDBLookup = val
		case "push_zombie_cache_write_failed":
			snap.PushZombieCacheWriteFailed = val
		case "kafka_produce_p95":
			snap.KafkaProduceP95 = val
		case "transfer_batch_p95":
			snap.TransferBatchP95 = val
		case "transfer_redis_p95":
			snap.TransferRedisCacheP95 = val
		case "transfer_mongo_p95":
			snap.TransferMongoWriteP95 = val
		case "push_group_size_p95":
			snap.PushGroupMemberP95 = val
		case "gw_encode_p95":
			snap.GatewayEncodeP95 = val
		case "e2e_group_p95":
			snap.E2EDeliveryGroupP95 = val
		case "e2e_single_p95":
			snap.E2EDeliverySingleP95 = val
		case "gw_batch_push_p95":
			snap.GatewayBatchPushP95 = val
		case "gw_batch_push_size_p95":
			snap.GatewayBatchPushSizeP95 = val
		case "gw_ws_write_p95":
			snap.GatewayWsWriteP95 = val
		}
	}

	// NOTE: MsgLagGrowthRate removed — the old computation compared per-message
	// production counters (single+group chat) against per-batch redis insert
	// counters, producing a false positive lag signal. Use CloudWatch MSK
	// SumOffsetLag (TSKafkaLag) for actual Kafka consumer lag instead.

	// Per-pod goroutines
	goroutines, err := p.queryVector(fmt.Sprintf(`go_goroutines{namespace="%s"}`, namespace))
	if err != nil {
		snap.Err = fmt.Errorf("prometheus required query %q failed: %w", "go_goroutines", err)
		return snap
	}
	if len(goroutines) == 0 {
		snap.Err = fmt.Errorf("prometheus required query %q returned no data", "go_goroutines")
		return snap
	}
	for pod, val := range goroutines {
		snap.PodMetrics = append(snap.PodMetrics, PodMetric{
			Pod:        pod,
			Goroutines: val,
		})
	}

	// Per-pod memory (alloc, heap in-use, heap released)
	memAlloc, err := p.queryVector(fmt.Sprintf(`go_memstats_alloc_bytes{namespace="%s"}`, namespace))
	if err == nil {
		for i, pm := range snap.PodMetrics {
			if val, ok := memAlloc[pm.Pod]; ok {
				snap.PodMetrics[i].MemAlloc = val
			}
		}
	}

	heapInUse, err := p.queryVector(fmt.Sprintf(`go_memstats_heap_inuse_bytes{namespace="%s"}`, namespace))
	if err == nil {
		for i, pm := range snap.PodMetrics {
			if val, ok := heapInUse[pm.Pod]; ok {
				snap.PodMetrics[i].HeapInUse = val
			}
		}
	}

	heapReleased, err := p.queryVector(fmt.Sprintf(`go_memstats_heap_released_bytes{namespace="%s"}`, namespace))
	if err == nil {
		for i, pm := range snap.PodMetrics {
			if val, ok := heapReleased[pm.Pod]; ok {
				snap.PodMetrics[i].HeapReleased = val
			}
		}
	}

	// Per-pod online user count (msg-gateway only)
	onlineUsers, err := p.queryVector(fmt.Sprintf(`online_user_num{namespace="%s"}`, namespace))
	if err == nil {
		for i, pm := range snap.PodMetrics {
			if val, ok := onlineUsers[pm.Pod]; ok {
				snap.PodMetrics[i].OnlineUsers = val
			}
		}
	}

	// Per-pod online connection count (msg-gateway only, upgrade version)
	onlineConns, err := p.queryVector(fmt.Sprintf(`online_user_conn_num{namespace="%s"}`, namespace))
	if err == nil {
		for i, pm := range snap.PodMetrics {
			if val, ok := onlineConns[pm.Pod]; ok {
				snap.PodMetrics[i].OnlineConns = val
			}
		}
	}

	return snap
}

func (p *PrometheusCollector) queryScalar(query string) (float64, bool, error) {
	u := fmt.Sprintf("%s/api/v1/query?query=%s", p.baseURL, url.QueryEscape(query))
	body, err := p.get(u)
	if err != nil {
		return 0, false, err
	}

	var result promResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, false, err
	}

	if result.Status != "success" {
		return 0, false, fmt.Errorf("prometheus: %s", result.Status)
	}

	if result.Data.ResultType == "vector" && len(result.Data.Result) > 0 {
		val, err := parsePromValue(result.Data.Result[0].Value)
		return val, err == nil, err
	}

	return 0, false, nil
}

func (p *PrometheusCollector) queryVector(query string) (map[string]float64, error) {
	u := fmt.Sprintf("%s/api/v1/query?query=%s", p.baseURL, url.QueryEscape(query))
	body, err := p.get(u)
	if err != nil {
		return nil, err
	}

	var result promResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("prometheus: %s", result.Status)
	}

	out := make(map[string]float64)
	for _, r := range result.Data.Result {
		pod := r.Metric["pod"]
		if pod == "" {
			pod = r.Metric["instance"]
		}
		val, err := parsePromValue(r.Value)
		if err == nil {
			out[pod] = val
		}
	}
	return out, nil
}

func parsePromValue(value interface{}) (float64, error) {
	// Prometheus returns [timestamp, "value"]
	arr, ok := value.([]interface{})
	if !ok || len(arr) < 2 {
		return 0, fmt.Errorf("unexpected value format")
	}
	s, ok := arr[1].(string)
	if !ok {
		return 0, fmt.Errorf("value is not string")
	}
	return strconv.ParseFloat(s, 64)
}

type promResponse struct {
	Status string   `json:"status"`
	Data   promData `json:"data"`
}

type promData struct {
	ResultType string       `json:"resultType"`
	Result     []promResult `json:"result"`
}

type promResult struct {
	Metric map[string]string `json:"metric"`
	Value  interface{}       `json:"value"`
}

// queryVectorLabeled returns all results with their full label maps.
func (p *PrometheusCollector) queryVectorLabeled(query string) ([]LabeledMetric, error) {
	u := fmt.Sprintf("%s/api/v1/query?query=%s", p.baseURL, url.QueryEscape(query))
	body, err := p.get(u)
	if err != nil {
		return nil, err
	}

	var result promResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("prometheus: %s", result.Status)
	}

	var out []LabeledMetric
	for _, r := range result.Data.Result {
		val, err := parsePromValue(r.Value)
		if err == nil {
			out = append(out, LabeledMetric{
				Labels: r.Metric,
				Value:  val,
			})
		}
	}
	return out, nil
}

// CollectChatAPI fetches chat-api and OpenIM service-level metrics
// that are not covered by the main Collect() method.
func (p *PrometheusCollector) CollectChatAPI(namespace string) ChatAPISnapshot {
	snap := ChatAPISnapshot{}
	ns := namespace

	type namedQuery struct {
		name     string
		query    string
		required bool
	}
	queries := []namedQuery{
		// HTTP summary
		{"http_total", `sum(rate(http_count{namespace="` + ns + `"}[1m]))`, true},
		{"http_2xx", `sum(rate(http_count{namespace="` + ns + `",status=~"2.."}[1m]))`, false},
		{"http_4xx", `sum(rate(http_count{namespace="` + ns + `",status=~"4.."}[1m]))`, false},
		{"http_5xx", `sum(rate(http_count{namespace="` + ns + `",status=~"5.."}[1m]))`, false},
		// API counters
		{"api_request", `sum(rate({__name__=~"api_request(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"api_success", `sum(rate({__name__=~"api_request_success(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"api_fail", `sum(rate({__name__=~"api_request_failed(_total)?",namespace="` + ns + `"}[1m]))`, false},
		// gRPC counters
		{"grpc_request", `sum(rate({__name__=~"grpc_request(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"grpc_success", `sum(rate({__name__=~"grpc_request_success(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"grpc_fail", `sum(rate({__name__=~"grpc_request_failed(_total)?",namespace="` + ns + `"}[1m]))`, false},
		// Message send
		{"send_msg", `sum(rate({__name__=~"send_msg(_total)?",namespace="` + ns + `"}[1m]))`, false},
		// Seq operations
		{"seq_get_ok", `sum(rate({__name__=~"seq_get_success(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"seq_get_fail", `sum(rate({__name__=~"seq_get_failed(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"seq_set_ok", `sum(rate({__name__=~"seq_set_success(_total)?",namespace="` + ns + `"}[1m]))`, false},
		// Message pull
		{"pull_redis_ok", `sum(rate({__name__=~"msg_pull_from_redis_success(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"pull_redis_fail", `sum(rate({__name__=~"msg_pull_from_redis_failed(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"pull_mongo_ok", `sum(rate({__name__=~"msg_pull_from_mongo_success(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"pull_mongo_fail", `sum(rate({__name__=~"msg_pull_from_mongo_failed(_total)?",namespace="` + ns + `"}[1m]))`, false},
		// Push success
		{"push_online_ok", `sum(rate({__name__=~"msg_online_push_success(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"push_offline_ok", `sum(rate({__name__=~"msg_offline_push_success(_total)?",namespace="` + ns + `"}[1m]))`, false},
		// Super group processing
		{"super_proc_ok", `sum(rate({__name__=~"work_super_group_chat_msg_process_success(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"super_proc_fail", `sum(rate({__name__=~"work_super_group_chat_msg_process_failed(_total)?",namespace="` + ns + `"}[1m]))`, false},
		// Conversation push
		{"conv_push_ok", `sum(rate({__name__=~"conversation_push_success(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"conv_push_fail", `sum(rate({__name__=~"conversation_push_failed(_total)?",namespace="` + ns + `"}[1m]))`, false},
		// WebSocket recv counters
		{"msg_recv", `sum(rate({__name__=~"msg_recv_total(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"newest_seq", `sum(rate({__name__=~"get_newest_seq_total(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"pull_by_seq", `sum(rate({__name__=~"pull_msg_by_seq_list_total(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"single_recv", `sum(rate({__name__=~"single_chat_msg_recv_success(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"group_recv", `sum(rate({__name__=~"group_chat_msg_recv_success(_total)?",namespace="` + ns + `"}[1m]))`, false},
		{"super_recv", `sum(rate({__name__=~"work_super_group_chat_msg_recv_success(_total)?",namespace="` + ns + `"}[1m]))`, false},
		// Batch send (IM-16749) — chat-api POST /v1/batch/send_batch_message
		{"batch_send_req_ok", `sum(rate(batch_send_requests_total{namespace="` + ns + `",status="success"}[1m]))`, false},
		{"batch_send_req_err", `sum(rate(batch_send_requests_total{namespace="` + ns + `",status="error"}[1m]))`, false},
		{"batch_send_targ_ok", `sum(rate(batch_send_targets_total{namespace="` + ns + `",status="success"}[1m]))`, false},
		{"batch_send_targ_fail", `sum(rate(batch_send_targets_total{namespace="` + ns + `",status="failed"}[1m]))`, false},
		{"batch_send_dur_p95", `histogram_quantile(0.95, sum(rate(batch_send_duration_seconds_bucket{namespace="` + ns + `"}[1m])) by (le))`, false},
		{"batch_send_setup_p95", `histogram_quantile(0.95, sum(rate(batch_send_setup_duration_seconds_bucket{namespace="` + ns + `"}[1m])) by (le))`, false},
		{"batch_send_loop_p95", `histogram_quantile(0.95, sum(rate(batch_send_loop_duration_seconds_bucket{namespace="` + ns + `"}[1m])) by (le))`, false},
		{"batch_send_rpc_p95", `histogram_quantile(0.95, sum(rate(batch_send_rpc_duration_seconds_bucket{namespace="` + ns + `"}[1m])) by (le))`, false},
		{"batch_send_targ_size_p95", `histogram_quantile(0.95, sum(rate(batch_send_targets_per_request_bucket{namespace="` + ns + `"}[1m])) by (le))`, false},
	}

	for _, q := range queries {
		val, found, err := p.queryScalar(q.query)
		if err != nil {
			if q.required {
				snap.Err = fmt.Errorf("prometheus required query %q failed: %w", q.name, err)
				return snap
			}
			continue
		}
		if !found {
			if q.required {
				snap.Err = fmt.Errorf("prometheus required query %q returned no data", q.name)
				return snap
			}
			continue
		}
		switch q.name {
		case "http_total":
			snap.TotalHTTPRate = val
		case "http_2xx":
			snap.Rate2XX = val
		case "http_4xx":
			snap.Rate4XX = val
		case "http_5xx":
			snap.Rate5XX = val
		case "api_request":
			snap.APIRequestRate = val
		case "api_success":
			snap.APISuccessRate = val
		case "api_fail":
			snap.APIFailRate = val
		case "grpc_request":
			snap.GRPCRequestRate = val
		case "grpc_success":
			snap.GRPCSuccessRate = val
		case "grpc_fail":
			snap.GRPCFailRate = val
		case "send_msg":
			snap.SendMsgRate = val
		case "seq_get_ok":
			snap.SeqGetOKRate = val
		case "seq_get_fail":
			snap.SeqGetFailRate = val
		case "seq_set_ok":
			snap.SeqSetOKRate = val
		case "pull_redis_ok":
			snap.MsgPullRedisOKRate = val
		case "pull_redis_fail":
			snap.MsgPullRedisFailRate = val
		case "pull_mongo_ok":
			snap.MsgPullMongoOKRate = val
		case "pull_mongo_fail":
			snap.MsgPullMongoFailRate = val
		case "push_online_ok":
			snap.OnlinePushOKRate = val
		case "push_offline_ok":
			snap.OfflinePushOKRate = val
		case "super_proc_ok":
			snap.SuperGroupProcOKRate = val
		case "super_proc_fail":
			snap.SuperGroupProcFailRate = val
		case "conv_push_ok":
			snap.ConvPushOKRate = val
		case "conv_push_fail":
			snap.ConvPushFailRate = val
		case "msg_recv":
			snap.MsgRecvTotalRate = val
		case "newest_seq":
			snap.NewestSeqTotalRate = val
		case "pull_by_seq":
			snap.PullBySeqListRate = val
		case "single_recv":
			snap.SingleChatRecvRate = val
		case "group_recv":
			snap.GroupChatRecvRate = val
		case "super_recv":
			snap.SuperGroupRecvRate = val
		// Batch send (IM-16749)
		case "batch_send_req_ok":
			snap.BatchSendRequestRate = val
		case "batch_send_req_err":
			snap.BatchSendErrorRate = val
		case "batch_send_targ_ok":
			snap.BatchSendTargetOKRate = val
		case "batch_send_targ_fail":
			snap.BatchSendTargetFailRate = val
		case "batch_send_dur_p95":
			snap.BatchSendDurationP95 = val
		case "batch_send_setup_p95":
			snap.BatchSendSetupP95 = val
		case "batch_send_loop_p95":
			snap.BatchSendLoopP95 = val
		case "batch_send_rpc_p95":
			snap.BatchSendRPCP95 = val
		case "batch_send_targ_size_p95":
			snap.BatchSendTargetsP95 = val
		}
	}

	// Per-endpoint HTTP breakdown
	results, err := p.queryVectorLabeled(
		`sum by (path, method, status) (rate(http_count{namespace="` + ns + `"}[1m]))`,
	)
	if err == nil {
		type epKey struct{ Path, Method string }
		epMap := map[epKey]*HTTPEndpointMetric{}
		for _, r := range results {
			path := r.Labels["path"]
			method := r.Labels["method"]
			status := r.Labels["status"]
			if path == "" {
				path = "<unknown>"
			}

			k := epKey{path, method}
			ep, ok := epMap[k]
			if !ok {
				ep = &HTTPEndpointMetric{Path: path, Method: method}
				epMap[k] = ep
			}

			if len(status) == 3 {
				switch status[0] {
				case '2':
					ep.Rate2XX += r.Value
				case '4':
					ep.Rate4XX += r.Value
				case '5':
					ep.Rate5XX += r.Value
				}
			}
			ep.Total += r.Value
		}

		for _, ep := range epMap {
			if ep.Total > 0.001 { // filter out noise
				snap.Endpoints = append(snap.Endpoints, *ep)
			}
		}
		sort.Slice(snap.Endpoints, func(i, j int) bool {
			return snap.Endpoints[i].Total > snap.Endpoints[j].Total
		})
	}

	return snap
}

// IsReachable checks if Prometheus is responding.
func (p *PrometheusCollector) IsReachable() bool {
	_, err := p.get(p.baseURL + "/api/v1/status/buildinfo")
	return err == nil
}

type prometheusHTTPError struct {
	status int
}

func (e prometheusHTTPError) Error() string {
	return fmt.Sprintf("prometheus http status %d", e.status)
}

func (p *PrometheusCollector) get(u string) ([]byte, error) {
	body, err := p.getOnce(u)
	if err == nil {
		return body, nil
	}
	if p.recover == nil {
		return nil, err
	}
	if _, ok := err.(prometheusHTTPError); ok {
		return nil, err
	}

	p.recoverMu.Lock()
	defer p.recoverMu.Unlock()
	if recoverErr := p.recover(); recoverErr != nil {
		return nil, fmt.Errorf("%w; recovery failed: %v", err, recoverErr)
	}
	return p.getOnce(u)
}

func (p *PrometheusCollector) getOnce(u string) ([]byte, error) {
	resp, err := p.client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, prometheusHTTPError{status: resp.StatusCode}
	}
	return body, nil
}
