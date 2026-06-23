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

func TestCollectSMSVerifyMetrics(t *testing.T) {
	c := newMockPrometheusCollector(t, func(query string) []map[string]any {
		switch {
		case strings.Contains(query, "online_user_num") && strings.Contains(query, `job="msg-gateway"`):
			return []map[string]any{promTestResult(map[string]string{}, "1")}
		case strings.Contains(query, "go_goroutines"):
			return []map[string]any{promTestResult(map[string]string{"pod": "chat-rpc-0"}, "42")}
		case strings.Contains(query, "sms_verify_code_send_total") && strings.Contains(query, `reason!~`):
			return []map[string]any{promTestResult(map[string]string{}, "0.5")}
		case strings.Contains(query, "sms_verify_code_send_total") && strings.Contains(query, `result="failure"`) && !strings.Contains(query, `reason=`):
			return []map[string]any{promTestResult(map[string]string{}, "1.5")}
		case strings.Contains(query, "sms_verify_code_send_total") && strings.Contains(query, `provider="aliyun"`) && strings.Contains(query, `reason="business_stopped"`):
			return []map[string]any{promTestResult(map[string]string{}, "0.4")}
		case strings.Contains(query, "sms_verify_code_send_total") && strings.Contains(query, `provider="tencent"`) && strings.Contains(query, `reason="phone_format_error"`):
			return []map[string]any{promTestResult(map[string]string{}, "0.3")}
		case strings.Contains(query, "sms_verify_code_send_total") && strings.Contains(query, `provider="tencent"`) && strings.Contains(query, `reason="insufficient_balance"`):
			return []map[string]any{promTestResult(map[string]string{}, "0.2")}
		case strings.Contains(query, "sms_verify_code_send_total") && strings.Contains(query, `provider="all"`) && strings.Contains(query, `reason="no_provider_success"`):
			return []map[string]any{promTestResult(map[string]string{}, "0.1")}
		default:
			return nil
		}
	})

	snap := c.Collect("im")
	if snap.Err != nil {
		t.Fatalf("expected snapshot to be healthy, got %v", snap.Err)
	}
	if snap.SMSFailTotal != 1.5 {
		t.Fatalf("SMSFailTotal = %v, want 1.5", snap.SMSFailTotal)
	}
	if snap.SMSAliBusinessStopped != 0.4 {
		t.Fatalf("SMSAliBusinessStopped = %v, want 0.4", snap.SMSAliBusinessStopped)
	}
	if snap.SMSTencentPhoneFormat != 0.3 {
		t.Fatalf("SMSTencentPhoneFormat = %v, want 0.3", snap.SMSTencentPhoneFormat)
	}
	if snap.SMSTencentInsufficientBalance != 0.2 {
		t.Fatalf("SMSTencentInsufficientBalance = %v, want 0.2", snap.SMSTencentInsufficientBalance)
	}
	if snap.SMSNoProviderSuccess != 0.1 {
		t.Fatalf("SMSNoProviderSuccess = %v, want 0.1", snap.SMSNoProviderSuccess)
	}
	if snap.SMSOtherFailure != 0.5 {
		t.Fatalf("SMSOtherFailure = %v, want 0.5", snap.SMSOtherFailure)
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
