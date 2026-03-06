# WebSocket Client Implementation Guide for Stress Test

This document describes exactly how the msg-gateway WebSocket server works so QA can verify their custom Locust WebSocket client is implemented correctly.

## 1. Connection URL

```
ws://<host>/
```

### Required Query Parameters

| Parameter | Type | Required | Example | Notes |
|-----------|------|----------|---------|-------|
| `sendID` | string | Yes | `"8542878524"` | User ID |
| `token` | string | Yes | JWT token from login | |
| `platformID` | int | Yes | `5` | See platform table below |
| `operationID` | string | Yes | Any unique string | UUID recommended |
| `isBackground` | bool | Yes | `false` | Set `false` for stress test |
| `isMsgResp` | bool | No | `true` | If `true`, server sends a JSON success response after upgrade |
| `sdkType` | string | No | `"js"` or `"go"` | Default `"go"` (protobuf encoding). Use `"js"` for JSON encoding. |
| `compression` | string | No | `"gzip"` | Leave empty for no compression |

### Platform IDs

| ID | Platform |
|----|----------|
| 1 | iOS |
| 2 | Android |
| 3 | Windows |
| 4 | macOS |
| 5 | Web |
| 6 | Mini Program |
| 7 | Linux |
| 8 | Android Pad |
| 9 | iPad |
| 10 | Admin |

**Important for stress test**: If `platformID=5` (Web), the server sends WebSocket Ping frames every 27s to keep the connection alive. For all other platforms, the server does NOT send pings — the client must send its own heartbeat.

### Alternative: JSON-encoded query (`v` parameter)

Instead of individual query parameters, you can pass a single `v` parameter with a base64url-encoded JSON object:

```
ws://<host>/?v=<base64url(JSON)>
```

JSON structure:
```json
{
  "token": "eyJ...",
  "userID": "8542878524",
  "platformID": 5,
  "operationID": "unique-op-id",
  "compression": "",
  "sdkType": "js",
  "sendResponse": true,
  "background": false
}
```

### Example Connection URL (query parameters)

```
ws://gateway.example.com/?sendID=8542878524&token=eyJ...&platformID=5&operationID=abc123&isBackground=false&isMsgResp=true&sdkType=js
```

## 2. Connection Flow

```
Client                              msg-gateway
  |                                      |
  |-- WS Upgrade GET /?sendID=...  ----->|
  |                                      |-- Validate query params
  |                                      |-- ParseToken via gRPC to auth service
  |                                      |-- Validate token matches userID+platformID
  |                                      |-- Upgrade to WebSocket
  |<---- 101 Switching Protocols --------|
  |                                      |
  |  (if isMsgResp=true)                 |
  |<---- Text: {"errCode":0,...}  -------|  ← success response
  |                                      |
  |  Connection is now live.             |
  |  Server starts readMessage() loop   |
  |  with 30-second read deadline.       |
  |                                      |
  |  Client MUST send data within 30s    |
  |  or connection will be closed.       |
```

## 3. Heartbeat (CRITICAL)

The server sets a **30-second read deadline** on every WebSocket connection. If the server receives **no data** from the client within 30 seconds, it closes the connection with an `i/o timeout` error.

### Accepted Heartbeat Formats

There are exactly **two** valid heartbeat formats:

#### Option A: WebSocket Ping Frame (Recommended)

Send a standard WebSocket **Ping control frame** (opcode `0x9`). The server's `pingHandler` will:
1. Reset the 30s read deadline
2. Respond with a Pong frame

In Python (websocket-client library):
```python
import websocket
ws = websocket.WebSocket()
ws.ping()  # sends WebSocket Ping frame
```

In Python (websockets library):
```python
import websockets
await ws.ping()
```

In Locust (if using websocket-client):
```python
ws.ping()
```

#### Option B: Text Frame with JSON `{"type":"ping"}`

Send a WebSocket **Text frame** containing exactly:
```json
{"type":"ping"}
```

The server will:
1. Parse the JSON
2. Respond with a Text frame: `{"type":"pong"}`
3. Loop back and reset the 30s read deadline

In Python:
```python
ws.send('{"type":"ping"}')
```

### Invalid Heartbeat Formats (will KILL the connection)

| What you send | What happens | Error |
|---------------|-------------|-------|
| Plain text `"ping"` | JSON parse fails | `invalid character 'p' looking for beginning of value` → connection closed |
| `{"type":"heartbeat"}` | Unsupported type | `not support text message type heartbeat` → connection closed |
| `"PING"` | JSON parse fails | connection closed |
| Empty text frame `""` | JSON parse fails | connection closed |
| Binary non-protobuf data | Decode fails in `handleMessage` | connection closed |

### Heartbeat Interval

- Server read deadline: **30 seconds**
- Recommended client ping interval: **10 seconds** (as SDK does)
- Maximum safe interval: **< 25 seconds** (leave margin)

### Heartbeat Timing Diagram

```
Time(s)  Client                     Server (readTimeout=30s)
  0      Connect                    SetReadDeadline(now+30s)
  10     send ping frame ───────>   pingHandler: SetReadDeadline(now+30s)
  20     send ping frame ───────>   pingHandler: SetReadDeadline(now+30s)
  30     send ping frame ───────>   pingHandler: SetReadDeadline(now+30s)
  ...    (connection stays alive)

  0      Connect                    SetReadDeadline(now+30s)
  ...    (no ping sent)
  30                                ReadMessage() returns i/o timeout
                                    Connection CLOSED
```

## 4. Sending Messages After Connect

After the WebSocket handshake, the server expects **binary frames** containing a protobuf-encoded `Req` message (when `sdkType=go`) or **JSON-encoded** `Req` (when `sdkType=js`).

### Req Structure (JSON, when sdkType=js)

```json
{
  "reqIdentifier": 1001,
  "sendID": "8542878524",
  "operationID": "unique-op-id",
  "msgIncr": "1",
  "token": "",
  "data": "<base64 protobuf>"
}
```

### Common reqIdentifier Values

| ID | Operation | Description |
|----|-----------|-------------|
| 1001 | GetNewestSeq | Get latest message sequence |
| 1002 | PullMsgBySeqList | Pull messages by sequence list |
| 1003 | SendMsg | Send a message |
| 1005 | PullMsg | Pull messages |

**For stress testing heartbeat only**: You do NOT need to send any binary/JSON Req messages. Just sending WebSocket Ping frames every 10s is sufficient to keep the connection alive.

## 5. Verification Checklist

Use this to verify your Locust WebSocket implementation:

- [ ] **Query params correct**: All required params present (`sendID`, `token`, `platformID`, `operationID`, `isBackground`)
- [ ] **platformID valid**: Must be 1-10 (see table above)
- [ ] **isBackground is a bool string**: `"false"`, not missing
- [ ] **Heartbeat format**: Using WebSocket Ping frame OR `{"type":"ping"}` text frame
- [ ] **Heartbeat interval**: Sending every 10s (must be < 30s)
- [ ] **NOT sending**: plain `"ping"`, `"heartbeat"`, empty frames, or custom formats
- [ ] **After connect**: If `isMsgResp=true`, read the initial success response before sending anything
- [ ] **Connection confirmed**: Check server logs for `registerClient: first login` with your userID

## 6. Quick Debug

To verify your connection is working on SIT, check the msg-gateway logs:

```bash
# Should see your user registered
kubectl logs -n im-sit deployment/msg-gateway --tail=100 | grep "YOUR_USER_ID"

# Should NOT see i/o timeout for your user within 30s of connect
kubectl logs -n im-sit deployment/msg-gateway --tail=100 | grep "i/o timeout" | grep "YOUR_USER_ID"
```

If you see `i/o timeout` exactly 30 seconds after connection, your heartbeat is not reaching the server or is in an invalid format.

## 7. Reference Source Code

| File | What it does |
|------|-------------|
| `internal/msggateway/constant.go:64` | `pongWait = 30 * time.Second` — the read deadline |
| `internal/msggateway/client_conn.go:118-121` | `setReadDeadline()` — sets the 30s deadline |
| `internal/msggateway/client_conn.go:123-156` | `readMessage()` loop — deadline reset each iteration |
| `internal/msggateway/client_conn.go:158-179` | `onReadTextMessage()` — handles `{"type":"ping"}` |
| `internal/msggateway/client_conn.go:181-192` | `pingHandler()` — handles WebSocket Ping frames |
| `internal/msggateway/ws_server.go:632-701` | `wsHandler()` — full connection lifecycle |
| `internal/msggateway/context.go:104-178` | `ParseEssentialArgs()` — query parameter parsing |
