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
| `msg_offline_push_failed_total` | push | `rate(msg_offline_push_failed_total[1m])` | > 0 = warning |

**Why:** Redis/Mongo insert failures = messages not delivered/persisted. Seq failures = message ordering corruption. Push failures = users not notified.

### Tier 2 — Important (add to Application tab)

| Metric | Source | PromQL | Purpose |
|--------|--------|--------|---------|
| `msg_insert_redis_success_total` | msg-transfer | `rate(msg_insert_redis_success_total[1m])` | Storage throughput sparkline |
| `msg_insert_mongo_success_total` | msg-transfer | `rate(msg_insert_mongo_success_total[1m])` | Persistence throughput sparkline |
| `msg_long_time_push_total` | push | `rate(msg_long_time_push_total[1m])` | Push delivery quality |
| `user_login_total` | auth | `rate(user_login_total[1m])` | Login activity trend |
| `http_count{status=~"5.."}` | api | `sum(rate(http_count{status=~"5.."}[1m]))` | API server error rate |

### Tier 3 — Nice to have

| Metric | Source | PromQL | Purpose |
|--------|--------|--------|---------|
| `grpc_server_handling_seconds` | all RPC | `histogram_quantile(0.99, ...)` | P99 latency per service |
| `grpc_server_handled_total{grpc_code!="OK"}` | all RPC | `sum by (grpc_service) (rate(...))` | Error rate per microservice |
| `user_register_total` | user | `rate(user_register_total[1m])` | Registration trend |
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
