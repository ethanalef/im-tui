package collector

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"
)

type PrometheusCollector struct {
	baseURL string
	client  *http.Client
}

func NewPrometheusCollector(baseURL string) *PrometheusCollector {
	return &PrometheusCollector{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (p *PrometheusCollector) Collect(namespace string) PrometheusSnapshot {
	snap := PrometheusSnapshot{}

	// Scalar queries
	type queryResult struct {
		name string
		val  float64
		err  error
	}

	ns := namespace

	// Ordered queries — each entry maps a name to a PromQL expression.
	// Fork-specific metrics (sent_msg_count_in_5_min, msg_gateway_send_msg_total)
	// are replaced with computed equivalents from upstream v3.8.3 counters.
	type namedQuery struct {
		name  string
		query string
	}
	queries := []namedQuery{
		{"online_users", `sum(online_user_num{namespace="` + ns + `",job="msg-gateway"})`},
		// Message processing counters — old fork omits _total suffix, upgrade has it.
		// Use __name__ regex to match both: "metric" and "metric_total".
		// NOTE: group_chat_msg_process_success is registered but NEVER incremented;
		// all group msgs use work_super_group_chat_msg_process_success instead.
		{"msgs_5min", `sum(rate({__name__=~"single_chat_msg_process_success(_total)?",namespace="` + ns + `"}[5m]) + rate({__name__=~"work_super_group_chat_msg_process_success(_total)?",namespace="` + ns + `"}[5m]))`},
		{"send_rate", `sum(rate({__name__=~"single_chat_msg_process_success(_total)?",namespace="` + ns + `"}[1m]) + rate({__name__=~"work_super_group_chat_msg_process_success(_total)?",namespace="` + ns + `"}[1m]))`},
		{"single_chat_ok", `sum(rate({__name__=~"single_chat_msg_process_success(_total)?",namespace="` + ns + `"}[1m]))`},
		{"single_chat_fail", `sum(rate({__name__=~"single_chat_msg_process_failed(_total)?",namespace="` + ns + `"}[1m]))`},
		{"group_chat_ok", `sum(rate({__name__=~"work_super_group_chat_msg_process_success(_total)?",namespace="` + ns + `"}[1m]))`},
		{"group_chat_fail", `sum(rate({__name__=~"work_super_group_chat_msg_process_failed(_total)?",namespace="` + ns + `"}[1m]))`},
		// Tier 1: msg-transfer storage pipeline
		{"redis_insert_ok", `sum(rate({__name__=~"msg_insert_redis_success(_total)?",namespace="` + ns + `"}[1m]))`},
		{"redis_insert_fail", `sum(rate({__name__=~"msg_insert_redis_failed(_total)?",namespace="` + ns + `"}[1m]))`},
		{"mongo_insert_ok", `sum(rate({__name__=~"msg_insert_mongo_success(_total)?",namespace="` + ns + `"}[1m]))`},
		{"mongo_insert_fail", `sum(rate({__name__=~"msg_insert_mongo_failed(_total)?",namespace="` + ns + `"}[1m]))`},
		{"seq_set_fail", `sum(rate({__name__=~"seq_set_failed(_total)?",namespace="` + ns + `"}[1m]))`},
		// Tier 1: push failures
		{"push_fail", `sum(rate({__name__=~"msg_offline_push_failed(_total)?",namespace="` + ns + `"}[1m]))`},
		// Tier 2: push quality + activity
		{"long_time_push", `sum(rate({__name__=~"msg_long_time_push(_total)?",namespace="` + ns + `"}[1m]))`},
		{"user_login", `sum(rate({__name__=~"user_login(_total)?",namespace="` + ns + `"}[1m]))`},
		{"user_register", `sum(rate({__name__=~"user_register(_total)?",namespace="` + ns + `"}[1m]))`},
		// NOTE: metric names are http_count and api_count (NO _total suffix)
		{"api_5xx", `sum(rate(http_count{namespace="` + ns + `",status=~"5.."}[1m]))`},
		{"chat_api_5xx", `sum(rate(http_count{namespace="` + ns + `",job=~".*chat-api.*",status=~"5.."}[1m]))`},
		{"openim_api_5xx", `sum(rate(http_count{namespace="` + ns + `",job=~".*openim-api.*",status=~"5.."}[1m]))`},
		// Gateway-level send counter (now available via ServiceMonitor)
		{"gateway_send_rate", `sum(rate(msg_gateway_send_msg_total{namespace="` + ns + `"}[1m]))`},
		// Push pipeline metrics (invisible queue visibility)
		{"push_in_flight", `sum(push_msg_in_flight{namespace="` + ns + `"})`},
		{"push_processing_p95", `histogram_quantile(0.95, sum(rate(push_msg_processing_duration_seconds_bucket{namespace="` + ns + `"}[1m])) by (le))`},
		{"push_grpc_p95", `histogram_quantile(0.95, sum(rate(push_grpc_delivery_duration_seconds_bucket{namespace="` + ns + `"}[1m])) by (le))`},
		{"gw_ws_queue_p95", `histogram_quantile(0.95, sum(rate(gateway_ws_write_queue_len_bucket{namespace="` + ns + `"}[1m])) by (le))`},
		// Pipeline latency histograms (upgrade version only — gracefully ignored if metrics absent)
		{"kafka_produce_p95", `histogram_quantile(0.95, sum(rate(kafka_produce_duration_seconds_bucket{namespace="` + ns + `"}[1m])) by (le))`},
		{"transfer_batch_p95", `histogram_quantile(0.95, sum(rate(msg_transfer_batch_duration_seconds_bucket{namespace="` + ns + `"}[1m])) by (le))`},
		{"transfer_redis_p95", `histogram_quantile(0.95, sum(rate(msg_transfer_redis_cache_duration_seconds_bucket{namespace="` + ns + `"}[1m])) by (le))`},
		{"transfer_mongo_p95", `histogram_quantile(0.95, sum(rate(msg_transfer_mongo_write_duration_seconds_bucket{namespace="` + ns + `"}[1m])) by (le))`},
		{"push_group_size_p95", `histogram_quantile(0.95, sum(rate(push_group_member_count_bucket{namespace="` + ns + `"}[1m])) by (le))`},
		{"gw_encode_p95", `histogram_quantile(0.95, sum(rate(gateway_msg_encode_duration_seconds_bucket{namespace="` + ns + `"}[1m])) by (le))`},
		{"e2e_group_p95", `histogram_quantile(0.95, sum(rate(message_e2e_delivery_seconds_bucket{namespace="` + ns + `",session_type="group"}[1m])) by (le))`},
		{"e2e_single_p95", `histogram_quantile(0.95, sum(rate(message_e2e_delivery_seconds_bucket{namespace="` + ns + `",session_type="single"}[1m])) by (le))`},
		{"gw_batch_push_p95", `histogram_quantile(0.95, sum(rate(gateway_batch_push_duration_seconds_bucket{namespace="` + ns + `"}[1m])) by (le))`},
		{"gw_batch_push_size_p95", `histogram_quantile(0.95, sum(rate(gateway_batch_push_user_count_bucket{namespace="` + ns + `"}[1m])) by (le))`},
		{"gw_ws_write_p95", `histogram_quantile(0.95, sum(rate(gateway_ws_write_duration_seconds_bucket{namespace="` + ns + `"}[1m])) by (le))`},
	}

	for _, q := range queries {
		val, err := p.queryScalar(q.query)
		if err != nil {
			// Continue collecting remaining metrics rather than aborting.
			// Some metrics may not exist in all deployments.
			continue
		}
		switch q.name {
		case "online_users":
			snap.OnlineUsers = val
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
	if err == nil {
		for pod, val := range goroutines {
			snap.PodMetrics = append(snap.PodMetrics, PodMetric{
				Pod:        pod,
				Goroutines: val,
			})
		}
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

	return snap
}

func (p *PrometheusCollector) queryScalar(query string) (float64, error) {
	u := fmt.Sprintf("%s/api/v1/query?query=%s", p.baseURL, url.QueryEscape(query))
	resp, err := p.client.Get(u)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var result promResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	if result.Status != "success" {
		return 0, fmt.Errorf("prometheus: %s", result.Status)
	}

	if result.Data.ResultType == "vector" && len(result.Data.Result) > 0 {
		return parsePromValue(result.Data.Result[0].Value)
	}

	return 0, nil
}

func (p *PrometheusCollector) queryVector(query string) (map[string]float64, error) {
	u := fmt.Sprintf("%s/api/v1/query?query=%s", p.baseURL, url.QueryEscape(query))
	resp, err := p.client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
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
	resp, err := p.client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
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
		name  string
		query string
	}
	queries := []namedQuery{
		// HTTP summary
		{"http_total", `sum(rate(http_count{namespace="` + ns + `"}[1m]))`},
		{"http_2xx", `sum(rate(http_count{namespace="` + ns + `",status=~"2.."}[1m]))`},
		{"http_4xx", `sum(rate(http_count{namespace="` + ns + `",status=~"4.."}[1m]))`},
		{"http_5xx", `sum(rate(http_count{namespace="` + ns + `",status=~"5.."}[1m]))`},
		// API counters
		{"api_request", `sum(rate({__name__=~"api_request(_total)?",namespace="` + ns + `"}[1m]))`},
		{"api_success", `sum(rate({__name__=~"api_request_success(_total)?",namespace="` + ns + `"}[1m]))`},
		{"api_fail", `sum(rate({__name__=~"api_request_failed(_total)?",namespace="` + ns + `"}[1m]))`},
		// gRPC counters
		{"grpc_request", `sum(rate({__name__=~"grpc_request(_total)?",namespace="` + ns + `"}[1m]))`},
		{"grpc_success", `sum(rate({__name__=~"grpc_request_success(_total)?",namespace="` + ns + `"}[1m]))`},
		{"grpc_fail", `sum(rate({__name__=~"grpc_request_failed(_total)?",namespace="` + ns + `"}[1m]))`},
		// Message send
		{"send_msg", `sum(rate({__name__=~"send_msg(_total)?",namespace="` + ns + `"}[1m]))`},
		// Seq operations
		{"seq_get_ok", `sum(rate({__name__=~"seq_get_success(_total)?",namespace="` + ns + `"}[1m]))`},
		{"seq_get_fail", `sum(rate({__name__=~"seq_get_failed(_total)?",namespace="` + ns + `"}[1m]))`},
		{"seq_set_ok", `sum(rate({__name__=~"seq_set_success(_total)?",namespace="` + ns + `"}[1m]))`},
		// Message pull
		{"pull_redis_ok", `sum(rate({__name__=~"msg_pull_from_redis_success(_total)?",namespace="` + ns + `"}[1m]))`},
		{"pull_redis_fail", `sum(rate({__name__=~"msg_pull_from_redis_failed(_total)?",namespace="` + ns + `"}[1m]))`},
		{"pull_mongo_ok", `sum(rate({__name__=~"msg_pull_from_mongo_success(_total)?",namespace="` + ns + `"}[1m]))`},
		{"pull_mongo_fail", `sum(rate({__name__=~"msg_pull_from_mongo_failed(_total)?",namespace="` + ns + `"}[1m]))`},
		// Push success
		{"push_online_ok", `sum(rate({__name__=~"msg_online_push_success(_total)?",namespace="` + ns + `"}[1m]))`},
		{"push_offline_ok", `sum(rate({__name__=~"msg_offline_push_success(_total)?",namespace="` + ns + `"}[1m]))`},
		// Super group processing
		{"super_proc_ok", `sum(rate({__name__=~"work_super_group_chat_msg_process_success(_total)?",namespace="` + ns + `"}[1m]))`},
		{"super_proc_fail", `sum(rate({__name__=~"work_super_group_chat_msg_process_failed(_total)?",namespace="` + ns + `"}[1m]))`},
		// Conversation push
		{"conv_push_ok", `sum(rate({__name__=~"conversation_push_success(_total)?",namespace="` + ns + `"}[1m]))`},
		{"conv_push_fail", `sum(rate({__name__=~"conversation_push_failed(_total)?",namespace="` + ns + `"}[1m]))`},
		// WebSocket recv counters
		{"msg_recv", `sum(rate({__name__=~"msg_recv_total(_total)?",namespace="` + ns + `"}[1m]))`},
		{"newest_seq", `sum(rate({__name__=~"get_newest_seq_total(_total)?",namespace="` + ns + `"}[1m]))`},
		{"pull_by_seq", `sum(rate({__name__=~"pull_msg_by_seq_list_total(_total)?",namespace="` + ns + `"}[1m]))`},
		{"single_recv", `sum(rate({__name__=~"single_chat_msg_recv_success(_total)?",namespace="` + ns + `"}[1m]))`},
		{"group_recv", `sum(rate({__name__=~"group_chat_msg_recv_success(_total)?",namespace="` + ns + `"}[1m]))`},
		{"super_recv", `sum(rate({__name__=~"work_super_group_chat_msg_recv_success(_total)?",namespace="` + ns + `"}[1m]))`},
	}

	for _, q := range queries {
		val, err := p.queryScalar(q.query)
		if err != nil {
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
	resp, err := p.client.Get(p.baseURL + "/api/v1/status/buildinfo")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}
