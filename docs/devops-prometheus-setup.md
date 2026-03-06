# DevOps Request: Expose Prometheus Metrics in SIT

## Background

Prometheus has been enabled in the OpenIM config (`openim-config` ConfigMap) and chat-api config (`openim-chat-config` ConfigMap) for the following services in `im-sit`. Each service now serves a `/metrics` HTTP endpoint on its configured prometheus port. However, **Prometheus cannot scrape them** because:

1. The k8s Services do not expose the prometheus ports
2. No `ServiceMonitor` resources exist for these services
3. chat-api ConfigMap is missing the `prometheus` config section entirely

## Current State

Confirmed working for chat-server services — verified by port-forwarding directly to the pod:

```
kubectl -n im-sit port-forward pod/msg-gateway-d74785976-9lfqj 12140:12140
curl http://localhost:12140/metrics   # returns online_user_num, rpc_count, etc.
```

chat-api metrics are **not yet active** — requires ConfigMap update first (see section below).

## Services and Prometheus Ports

### chat-server services (from `openim-config` ConfigMap, already deployed):

| Service | Prometheus Port | Key Metrics |
|---------|----------------|-------------|
| msg-gateway | 12140 | `online_user_num`, `rpc_count` |
| msg-transfer | 12020 | `msg_insert_redis_*_total`, `msg_insert_mongo_*_total`, `seq_set_failed_total` |
| openim-api | 12002 | `api_count`, `http_count` |
| openim-push | 12170 | `msg_offline_push_failed_total`, `msg_long_time_push_total` |
| openim-msg | 20130 | `single_chat_msg_process_*_total`, `group_chat_msg_process_*_total` |
| openim-rpc-user | 20110 | `user_register_total` |
| openim-rpc-auth | 20160 | `user_login_total` |
| openim-rpc-friend | 20120 | `rpc_count` |
| openim-rpc-conversation | 20180 | `rpc_count` |
| openim-rpc-third | 20190 | `rpc_count` |
| openim-rpc-group | **disabled** | — |

### chat-api (from `openim-chat-config` ConfigMap, needs config update):

| Service | Prometheus Port | Key Metrics |
|---------|----------------|-------------|
| chat-api | 12008 | `http_count{path,method,status}` |

## What Needs to Be Done

### 0. Add prometheus config to chat-api ConfigMap (PREREQUISITE)

The `openim-chat-config` ConfigMap in both SIT and UAT is missing the `prometheus` section. Without it, chat-api will not start a metrics server.

**File:** `deployment/im-sit/configmap/chat-api.yaml` (and `deployment/im/configmap/chat-api.yaml`)

Add at root level of `config.yaml` (e.g. after the `api:` section):

```yaml
    # Prometheus metrics
    prometheus:
      enable: true
      port: 12008
```

### 1. Add prometheus port to chat-api Deployment

The chat-api Deployment (`deployment/im-sit/chat-api/chat-api.yaml`) only exposes `containerPort: 10008`.

Add the prometheus port to `spec.template.spec.containers[0].ports`:

```yaml
ports:
  - containerPort: 10008
  - containerPort: 12008
    name: prometheus
```

### 2. Add prometheus port to chat-api Service

The chat-api Service only exposes port 10008. Add the prometheus port:

```yaml
ports:
  - name: http
    port: 10008
    protocol: TCP
    targetPort: 10008
  - name: prometheus
    port: 12008
    targetPort: 12008
    protocol: TCP
```

### 3. Add prometheus ports to chat-server Service specs

For each chat-server service, add a named port to its k8s `Service`. Example for msg-gateway:

```yaml
# Add to the msg-gateway Service spec.ports[]
- name: prometheus
  port: 12140
  targetPort: 12140
  protocol: TCP
```

### 4. Create ServiceMonitors

Create `ServiceMonitor` resources in `im-sit` so that `kube-prometheus-stack` Prometheus discovers and scrapes them.

Option A — one ServiceMonitor per service:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: chat-api-monitor
  namespace: im-sit
  labels:
    release: kube-prometheus-stack   # must match Prometheus serviceMonitorSelector
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: chat-api
  endpoints:
    - port: prometheus
      interval: 15s
      path: /metrics
```

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: msg-gateway-monitor
  namespace: im-sit
  labels:
    release: kube-prometheus-stack
spec:
  selector:
    matchLabels:
      app: msg-gateway
  endpoints:
    - port: prometheus
      interval: 15s
      path: /metrics
```

Option B — single ServiceMonitor covering all services (if they share a label pattern):

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: openim-monitor
  namespace: im-sit
  labels:
    release: kube-prometheus-stack
spec:
  selector:
    matchExpressions:
      - key: app
        operator: In
        values:
          - msg-gateway
          - msg-transfer
          - openim-api
          - openim-push
          - openim-msg
          - openim-rpc-user
          - openim-rpc-auth
          - openim-rpc-friend
          - openim-rpc-conversation
          - openim-rpc-third
  endpoints:
    - port: prometheus
      interval: 15s
      path: /metrics
```

Note: chat-api uses `app.kubernetes.io/name: chat-api` (not `app: chat-api`), so it needs its own ServiceMonitor or an additional matchExpression.

### 5. Verify serviceMonitorSelector

Check that `kube-prometheus-stack` Prometheus is configured to pick up ServiceMonitors from `im-sit`. The existing `manage-api-monitor` in `im-sit` suggests this already works, but verify the label selector matches (likely `release: kube-prometheus-stack`).

```bash
kubectl -n kube-prometheus-stack get prometheus -o jsonpath='{.items[0].spec.serviceMonitorSelector}'
kubectl -n kube-prometheus-stack get prometheus -o jsonpath='{.items[0].spec.serviceMonitorNamespaceSelector}'
```

## Recent Code Changes (2026-02-22)

### chat-server (code changes only, no k8s config changes needed)

The following metrics bugs were fixed in code. The prometheus config is already correct in the ConfigMap:

1. **Fixed metric name typo**: `msg_lone_time_push_total` → `msg_long_time_push_total`
2. **Wired Redis insert counters**: `msg_insert_redis_success_total` and `msg_insert_redis_failed_total` are now registered and incremented in msg-transfer
3. **Implemented HTTP/API metrics**: `http_count` and `api_count` CounterVecs now have real implementations (were previously stubs)

### chat-api (requires k8s config changes above)

Prometheus instrumentation was added from scratch:
- New `http_count{path,method,status}` CounterVec
- Gin middleware records all HTTP requests
- Separate `/metrics` server on configured port
- Controlled by `prometheus.enable` and `prometheus.port` in config

## Verification After Setup

```bash
# Check targets appear in Prometheus
curl http://<prometheus>/api/v1/targets?state=active | grep msg-gateway
curl http://<prometheus>/api/v1/targets?state=active | grep chat-api

# Check chat-server metrics are being scraped
curl 'http://<prometheus>/api/v1/query?query=online_user_num'
# Should return non-empty result

# Check chat-api metrics are being scraped
curl 'http://<prometheus>/api/v1/query?query=http_count'
# Should return results from both openim-api and chat-api
```

## Priority

1. **msg-gateway** (port 12140) — provides `online_user_num` (critical for dashboard)
2. **openim-msg** (port 20130) — chat message success/fail counters
3. **msg-transfer** (port 12020) — Redis/Mongo storage pipeline metrics
4. **chat-api** (port 12008) — HTTP request metrics for the business API layer
5. Remaining RPC services — lower priority, provide `rpc_count` only
