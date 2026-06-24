# OpenIM Prometheus Metrics Study

## Purpose

Identify all available Prometheus metrics across OpenIM services (v3.8.3) and recommend which to add to the im-tui dashboard for a complete system health picture.

## Complete Metrics Inventory

### Per-Service Custom Metrics

#### msg-gateway (port 12140)
| Metric | Type | Description |
|--------|------|-------------|
| `online_user_num` | Gauge | Current WebSocket-connected users |

#### openim-msg (port 20130)
| Metric | Type | Description |
|--------|------|-------------|
| `single_chat_msg_process_success_total` | Counter | Single-chat messages processed OK |
| `single_chat_msg_process_failed_total` | Counter | Single-chat messages failed |
| `group_chat_msg_process_success_total` | Counter | Group-chat messages processed OK |
| `group_chat_msg_process_failed_total` | Counter | Group-chat messages failed |

#### msg-transfer (port 12020)
| Metric | Type | Description |
|--------|------|-------------|
| `msg_insert_redis_success_total` | Counter | Successful Redis message inserts |
| `msg_insert_redis_failed_total` | Counter | Failed Redis message inserts |
| `msg_insert_mongo_success_total` | Counter | Successful MongoDB message inserts |
| `msg_insert_mongo_failed_total` | Counter | Failed MongoDB message inserts |
| `seq_set_failed_total` | Counter | Sequence number assignment failures |

#### openim-push (port 12170)
| Metric | Type | Description |
|--------|------|-------------|
| `msg_offline_push_failed_total` | Counter | Failed offline push notifications |
| `msg_long_time_push_total` | Counter | Pushes taking >10 seconds |
| `push_zombie_filter_candidates_total{scope}` | Counter | Offline-push targets examined by the IM-17718 zombie filter |
| `push_zombie_filter_dropped_total{scope}` | Counter | Targets skipped because durable last-online is older than the threshold |
| `push_zombie_filter_kept_total{scope}` | Counter | Targets preserved after zombie filtering |
| `push_zombie_filter_unknown_total{scope}` | Counter | Targets preserved because MySQL last-online/login time is missing |
| `push_zombie_filter_fail_open_total{scope}` | Counter | Targets preserved because the filter lookup failed open |
| `push_zombie_filter_cache_hit_total{scope,source}` | Counter | Targets resolved by Redis cache before MySQL fallback |
| `push_zombie_filter_cache_miss_total{scope}` | Counter | Targets missing from Redis cache and requiring MySQL fallback |
| `push_zombie_filter_cache_error_total{scope}` | Counter | Targets falling back because Redis cache lookup failed |
| `push_zombie_filter_db_lookup_total{scope}` | Counter | Targets looked up from MySQL by zombie filter |
| `push_zombie_filter_cache_write_total{scope,result}` | Counter | Redis cache writebacks after MySQL lookup |

#### openim-rpc-auth (port 20160)
| Metric | Type | Description |
|--------|------|-------------|
| `user_login_total` | Counter | Total login events |

#### openim-rpc-user (port 20110)
| Metric | Type | Description |
|--------|------|-------------|
| `user_register_total` | Counter | Total registration events |

#### openim-api (port 12002)
| Metric | Type | Description |
|--------|------|-------------|
| `api_count{path,method,code}` | Counter | API calls by endpoint and app-level response code |
| `http_count{path,method,status}` | Counter | HTTP calls by endpoint and HTTP status code |

### Shared Metrics (all RPC services)

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `rpc_count` | Counter | `name`, `path`, `code` | RPC call count per service/method/status |
| `grpc_server_started_total` | Counter | `grpc_service`, `grpc_method`, `grpc_type` | RPCs started |
| `grpc_server_handled_total` | Counter | `grpc_service`, `grpc_method`, `grpc_type`, `grpc_code` | RPCs completed |
| `grpc_server_handling_seconds` | Histogram | `grpc_service`, `grpc_method`, `grpc_type` | RPC latency distribution |
| `go_goroutines` | Gauge | — | Goroutine count |
| `go_memstats_alloc_bytes` | Gauge | — | Heap allocation |
| `process_resident_memory_bytes` | Gauge | — | RSS memory |
| `process_cpu_seconds_total` | Counter | — | CPU time consumed |

### Services with NO custom metrics
- rpc-conversation, rpc-friend, rpc-group, rpc-third (only shared RPC + Go process metrics)

---

## Current im-tui Coverage

The Prometheus collector currently queries:

| Metric | Status |
|--------|--------|
| `online_user_num` | Available (msg-gateway) |
| `sent_msg_count_in_5_min` | **NOT in v3.8.3** — appears to be from our fork |
| `msg_gateway_send_msg_total` | **NOT in v3.8.3** — appears to be from our fork |
| `single_chat_msg_process_success_total` | Available (openim-msg) |
| `single_chat_msg_process_failed_total` | Available (openim-msg) |
| `group_chat_msg_process_success_total` | Available (openim-msg) |
| `group_chat_msg_process_failed_total` | Available (openim-msg) |
| `go_goroutines` | Available (all services) |
| `go_memstats_alloc_bytes` | Available (all services) |

---

## Recommended Additions (by priority)

### Tier 1 — Critical (add to overview + alerts)

These metrics indicate data loss or service degradation. Any non-zero failure rate is a P1.

| Metric | Source | PromQL | Alert Threshold |
|--------|--------|--------|-----------------|
| `msg_insert_redis_failed_total` | msg-transfer | `rate(msg_insert_redis_failed_total[1m])` | > 0 = critical |
| `msg_insert_mongo_failed_total` | msg-transfer | `rate(msg_insert_mongo_failed_total[1m])` | > 0 = critical |
| `seq_set_failed_total` | msg-transfer | `rate(seq_set_failed_total[1m])` | > 0 = critical |
| `msg_offline_push_failed_total` | push | `rate(msg_offline_push_failed_total[1m])` | Environment-specific warning (`push_fail_warn_per_sec`); unset keeps > 0 = warning |
| `push_zombie_filter_fail_open_total` | push | `rate(push_zombie_filter_fail_open_total[1m])` | > 0 = warning |
| `push_zombie_filter_cache_error_total` | push | `rate(push_zombie_filter_cache_error_total[1m])` | > 0 = warning |

**Why:** Redis/Mongo insert failures = messages not delivered/persisted. Seq failures = message ordering corruption. Push failures = users not notified.

### Tier 2 — Important (add to Application tab)

| Metric | Source | PromQL | Purpose |
|--------|--------|--------|---------|
| `msg_insert_redis_success_total` | msg-transfer | `rate(msg_insert_redis_success_total[1m])` | Storage throughput sparkline |
| `msg_insert_mongo_success_total` | msg-transfer | `rate(msg_insert_mongo_success_total[1m])` | Persistence throughput sparkline |
| `msg_long_time_push_total` | push | `rate(msg_long_time_push_total[1m])` | Push delivery quality |
| `push_zombie_filter_dropped_total` | push | `rate(push_zombie_filter_dropped_total[1m])` | Zombie offline-push reduction rate |
| `push_zombie_filter_cache_hit_total` | push | `rate(push_zombie_filter_cache_hit_total[1m])` | Redis cache effectiveness |
| `push_zombie_filter_db_lookup_total` | push | `rate(push_zombie_filter_db_lookup_total[1m])` | MySQL fallback pressure |
| `user_login_total` | auth | `increase(user_login_total[1h])` | Login count in last hour |
| `http_count{status=~"5.."}` | api | `sum(rate(http_count{status=~"5.."}[1m]))` | API server error rate |

### Tier 3 — Nice to have

| Metric | Source | PromQL | Purpose |
|--------|--------|--------|---------|
| `grpc_server_handling_seconds` | all RPC | `histogram_quantile(0.99, ...)` | P99 latency per service |
| `grpc_server_handled_total{grpc_code!="OK"}` | all RPC | `sum by (grpc_service) (rate(...))` | Error rate per microservice |
| `user_register_total` | user | `increase(user_register_total[1h])` | Registration count in last hour |
| `process_resident_memory_bytes` | all | per-pod | True RSS (complements go_memstats) |
| `process_cpu_seconds_total` | all | `rate(...[1m])` per pod | Per-pod CPU |

---

## Fork-Specific Metrics

Two metrics in our Prometheus collector (`sent_msg_count_in_5_min` and `msg_gateway_send_msg_total`) do NOT exist in upstream v3.8.3. Verify whether our fork exposes them. If not, replace with:

- `sent_msg_count_in_5_min` → compute from `rate(single_chat_msg_process_success_total[5m]) + rate(group_chat_msg_process_success_total[5m])`
- `msg_gateway_send_msg_total` → use `sum(rate(single_chat_msg_process_success_total[1m]) + rate(group_chat_msg_process_success_total[1m]))` as send rate

---

## Implementation Impact on im-tui

Adding Tier 1 metrics requires:
1. New PromQL queries in `collector/prometheus.go`
2. New fields in `collector.PrometheusSnapshot`
3. New fields in `export.AppMetrics` (for JSONL export)
4. New alert rules in `alert/evaluator.go`
5. Display in `view/application.go` and `view/overview.go`

The msg-transfer metrics come from a **different scrape target** (port 12020 vs msg-gateway's 12140), so they need Prometheus to scrape msg-transfer as well — covered in the DevOps request.

### SMS Verification Provider Metric

`sms_verify_code_send_total{provider,result,reason}` is emitted by chat-rpc for verification-code send attempts. `im-tui` consumes last-hour counts by provider/result/reason:

- `provider="tencent", result="success", reason="ok"` → confirms Tencent fallback is delivering verification codes when Aliyun is broken.
- `provider="aliyun", reason="business_stopped"` → critical, Aliyun account/business is stopped.
- `provider="tencent", reason="insufficient_balance"` → critical, Tencent balance/package needs recharge.
- `provider="all", reason="no_provider_success"` → critical, no provider delivered the code.
- `provider="tencent", reason="phone_format_error"` → warning, request phone formatting needs investigation.
- Other failure reasons use the configurable `sms_fail_warn_per_hour` / `sms_fail_crit_per_hour` thresholds.

The TUI intentionally does not query or display phone numbers from Prometheus labels. PROD stdout is configured at error level, so success and failover-success `ZInfo` lines may not appear in logs; Tencent success should be checked from the hourly Prometheus counters.
