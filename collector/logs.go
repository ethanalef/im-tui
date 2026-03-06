package collector

import (
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

const maxLogLines = 100

// LogCollector fetches kubectl logs for each IM service and extracts error lines.
type LogCollector struct {
	kubeconfig string
	namespace  string
	services   []string
	sinceSec   int
}

// LogSnapshot holds parsed log data from all services.
type LogSnapshot struct {
	Services []ServiceLogSummary
	Lines    []LogLine // recent error/fail/timeout/panic lines, newest first
	Err      error
}

// ServiceLogSummary counts error categories for a single service.
type ServiceLogSummary struct {
	Name     string
	Errors   int
	Fails    int
	Timeouts int
	Panics   int
	Total    int
}

// LogLine is a single parsed log line with error context.
type LogLine struct {
	Time    string // extracted timestamp or "unknown"
	Service string
	Level   string // "ERROR", "FAIL", "TIMEOUT", "PANIC"
	Message string // truncated line content
}

func NewLogCollector(kubeconfig, namespace string, services []string, sinceSec int) *LogCollector {
	return &LogCollector{
		kubeconfig: kubeconfig,
		namespace:  namespace,
		services:   services,
		sinceSec:   sinceSec,
	}
}

var (
	reError   = regexp.MustCompile(`(?i)\berror\b`)
	reFail    = regexp.MustCompile(`(?i)\bfail`)
	reTimeout = regexp.MustCompile(`(?i)(timeout|deadline)`)
	rePanic   = regexp.MustCompile(`(?i)\bpanic\b`)
	reTime    = regexp.MustCompile(`\d{2}:\d{2}:\d{2}`)
	reANSI    = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	// Match JSON "error" field value
	reJSONError = regexp.MustCompile(`"error"\s*:\s*"((?:[^"\\]|\\.)*)"`)
)

func (lc *LogCollector) Collect() LogSnapshot {
	snap := LogSnapshot{}

	var allLines []LogLine

	for _, svc := range lc.services {
		summary, lines := lc.collectService(svc)
		snap.Services = append(snap.Services, summary)
		allLines = append(allLines, lines...)
	}

	// Sort newest first and cap at maxLogLines
	sort.Slice(allLines, func(i, j int) bool {
		return allLines[i].Time > allLines[j].Time
	})
	if len(allLines) > maxLogLines {
		allLines = allLines[:maxLogLines]
	}
	snap.Lines = allLines

	return snap
}

func (lc *LogCollector) collectService(svc string) (ServiceLogSummary, []LogLine) {
	summary := ServiceLogSummary{Name: svc}

	sinceArg := fmt.Sprintf("--since=%ds", lc.sinceSec)
	args := []string{
		"--kubeconfig", lc.kubeconfig,
		"-n", lc.namespace,
		"logs", fmt.Sprintf("deployment/%s", svc),
		sinceArg,
		"--tail=500",
	}

	cmd := exec.Command("kubectl", args...)
	cmd.Env = append(cmd.Environ(), "KUBECONFIG="+lc.kubeconfig)
	out, err := cmd.Output()
	if err != nil {
		// Deployment not found or not ready — return zero counts
		return summary, nil
	}

	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return summary, nil
	}

	lines := strings.Split(raw, "\n")
	summary.Total = len(lines)

	var matched []LogLine
	for _, line := range lines {
		level := classifyLine(line)
		if level == "" {
			continue
		}

		switch level {
		case "ERROR":
			summary.Errors++
		case "FAIL":
			summary.Fails++
		case "TIMEOUT":
			summary.Timeouts++
		case "PANIC":
			summary.Panics++
		}

		ts := extractTime(line)
		msg := extractMessage(line)

		matched = append(matched, LogLine{
			Time:    ts,
			Service: svc,
			Level:   level,
			Message: msg,
		})
	}

	return summary, matched
}

// classifyLine returns the highest-priority matching category, or "" for no match.
func classifyLine(line string) string {
	if rePanic.MatchString(line) {
		return "PANIC"
	}
	if reTimeout.MatchString(line) {
		return "TIMEOUT"
	}
	if reFail.MatchString(line) {
		return "FAIL"
	}
	if reError.MatchString(line) {
		return "ERROR"
	}
	return ""
}

// extractTime pulls the first HH:MM:SS from the line, or returns current time.
func extractTime(line string) string {
	match := reTime.FindString(line)
	if match != "" {
		return match
	}
	return time.Now().Format("15:04:05")
}

// extractMessage parses OpenIM structured logs (tab-separated, ANSI-colored)
// and extracts the meaningful description + error content.
//
// Format: datetime \t LEVEL \t [PID:n] \t component \t [version] \t [source:line] \t description \t {json}
func extractMessage(line string) string {
	// Strip ANSI color codes
	clean := reANSI.ReplaceAllString(line, "")

	// Split on tabs — OpenIM logs are tab-delimited
	fields := strings.Split(clean, "\t")

	// Trim whitespace from all fields
	trimmed := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f != "" {
			trimmed = append(trimmed, f)
		}
	}

	// Need at least the structured prefix fields (datetime, level, pid, component, version, source)
	// Fields after that are: description, {json params}
	if len(trimmed) >= 7 {
		desc := trimmed[6]
		// Try to extract "error" field from JSON params if present
		if len(trimmed) >= 8 {
			jsonPart := trimmed[7]
			if errMatch := reJSONError.FindStringSubmatch(jsonPart); len(errMatch) >= 2 {
				return desc + ": " + errMatch[1]
			}
			// No explicit error key — append raw JSON
			return desc + ": " + jsonPart
		}
		return desc
	}

	// Not structured — return cleaned full line
	if len(trimmed) > 0 {
		return strings.Join(trimmed, " ")
	}
	return strings.TrimSpace(clean)
}
