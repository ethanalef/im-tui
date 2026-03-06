package collector

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type LocustCollector struct {
	baseURL string
	client  *http.Client
}

func NewLocustCollector(baseURL string) *LocustCollector {
	return &LocustCollector{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 3 * time.Second,
		},
	}
}

func (l *LocustCollector) Collect() LocustSnapshot {
	snap := LocustSnapshot{}

	resp, err := l.client.Get(l.baseURL + "/stats/requests")
	if err != nil {
		snap.Available = false
		snap.Err = err
		return snap
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		snap.Available = false
		snap.Err = err
		return snap
	}

	var stats locustStats
	if err := json.Unmarshal(body, &stats); err != nil {
		snap.Available = false
		snap.Err = fmt.Errorf("parsing locust stats: %w", err)
		return snap
	}

	snap.Available = true
	snap.State = stats.State
	snap.UserCount = stats.UserCount
	snap.TotalRPS = stats.TotalRPS
	snap.FailRatio = stats.FailRatio

	for _, s := range stats.Stats {
		if s.Name == "Aggregated" {
			continue
		}
		ep := LocustEndpoint{
			Method:          s.Method,
			Name:            s.Name,
			NumRequests:     s.NumRequests,
			NumFailures:     s.NumFailures,
			RPS:             s.CurrentRPS,
			AvgResponseTime: s.AvgResponseTime,
			P50:             getPercentile(s.ResponseTimes, 50),
			P95:             getPercentile(s.ResponseTimes, 95),
			P99:             getPercentile(s.ResponseTimes, 99),
			MaxResponseTime: s.MaxResponseTime,
		}
		if s.NumRequests > 0 {
			ep.FailPercent = float64(s.NumFailures) / float64(s.NumRequests) * 100
		}
		snap.Endpoints = append(snap.Endpoints, ep)
	}

	for _, f := range stats.Errors {
		snap.Failures = append(snap.Failures, LocustFailure{
			Method:      f.Method,
			Name:        f.Name,
			Error:       f.Error,
			Occurrences: f.Occurrences,
		})
	}

	return snap
}

func getPercentile(responseTimes map[string]float64, p int) float64 {
	key := fmt.Sprintf("%d", p)
	if v, ok := responseTimes[key]; ok {
		return v
	}
	// Try float key format
	key = fmt.Sprintf("%.1f", float64(p))
	if v, ok := responseTimes[key]; ok {
		return v
	}
	return 0
}

// IsReachable checks if Locust is running and responding.
func (l *LocustCollector) IsReachable() bool {
	resp, err := l.client.Get(l.baseURL + "/stats/requests")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

type locustStats struct {
	State     string       `json:"state"`
	UserCount int          `json:"user_count"`
	TotalRPS  float64      `json:"total_rps"`
	FailRatio float64      `json:"fail_ratio"`
	Stats     []locustStat `json:"stats"`
	Errors    []locustErr  `json:"errors"`
}

type locustStat struct {
	Method          string             `json:"method"`
	Name            string             `json:"name"`
	NumRequests     int                `json:"num_requests"`
	NumFailures     int                `json:"num_failures"`
	CurrentRPS      float64            `json:"current_rps"`
	AvgResponseTime float64            `json:"avg_response_time"`
	MaxResponseTime float64            `json:"max_response_time"`
	ResponseTimes   map[string]float64 `json:"response_times"`
}

type locustErr struct {
	Method      string `json:"method"`
	Name        string `json:"name"`
	Error       string `json:"error"`
	Occurrences int    `json:"occurrences"`
}
