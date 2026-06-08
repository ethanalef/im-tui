package collector

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCollectReportsErrorWhenRequiredMetricMissing(t *testing.T) {
	c := newMockPrometheusCollector(t, func(query string) []map[string]any {
		return nil
	})

	snap := c.Collect("im")
	if snap.Err == nil {
		t.Fatal("expected missing online_user_num to make snapshot unhealthy")
	}
	if !strings.Contains(snap.Err.Error(), "online_users") {
		t.Fatalf("expected online_users error, got %v", snap.Err)
	}
}

func TestCollectAcceptsRealZeroRequiredMetric(t *testing.T) {
	c := newMockPrometheusCollector(t, func(query string) []map[string]any {
		switch {
		case strings.Contains(query, "online_user_num") && strings.Contains(query, `job="msg-gateway"`):
			return []map[string]any{promTestResult(map[string]string{}, "0")}
		case strings.Contains(query, "go_goroutines"):
			return []map[string]any{promTestResult(map[string]string{"pod": "msg-gateway-0"}, "42")}
		default:
			return nil
		}
	})

	snap := c.Collect("im")
	if snap.Err != nil {
		t.Fatalf("expected real zero online_user_num to be healthy, got %v", snap.Err)
	}
	if snap.OnlineUsers != 0 {
		t.Fatalf("expected online users to be 0, got %v", snap.OnlineUsers)
	}
	if len(snap.PodMetrics) != 1 || snap.PodMetrics[0].Goroutines != 42 {
		t.Fatalf("expected one goroutine pod metric, got %+v", snap.PodMetrics)
	}
}

func TestCollectChatAPIReportsErrorWhenRequiredMetricMissing(t *testing.T) {
	c := newMockPrometheusCollector(t, func(query string) []map[string]any {
		return nil
	})

	snap := c.CollectChatAPI("im")
	if snap.Err == nil {
		t.Fatal("expected missing http_count to make chat api snapshot unhealthy")
	}
	if !strings.Contains(snap.Err.Error(), "http_total") {
		t.Fatalf("expected http_total error, got %v", snap.Err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func newMockPrometheusCollector(t *testing.T, resultsFor func(query string) []map[string]any) *PrometheusCollector {
	t.Helper()
	c := NewPrometheusCollector("http://prometheus.test")
	c.client = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			body := promVectorBody(t, resultsFor(r.URL.Query().Get("query")))
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		}),
	}
	return c
}

func promVectorBody(t *testing.T, results []map[string]any) []byte {
	t.Helper()
	if results == nil {
		results = []map[string]any{}
	}
	var buf bytes.Buffer
	err := json.NewEncoder(&buf).Encode(map[string]any{
		"status": "success",
		"data": map[string]any{
			"resultType": "vector",
			"result":     results,
		},
	})
	if err != nil {
		t.Fatalf("encode response: %v", err)
	}
	return buf.Bytes()
}

func promTestResult(labels map[string]string, value string) map[string]any {
	return map[string]any{
		"metric": labels,
		"value":  []any{float64(1), value},
	}
}
