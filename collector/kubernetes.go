package collector

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type KubernetesCollector struct {
	kubeconfig string
	namespace  string
}

func NewKubernetesCollector(kubeconfig, namespace string) *KubernetesCollector {
	return &KubernetesCollector{
		kubeconfig: kubeconfig,
		namespace:  namespace,
	}
}

func (k *KubernetesCollector) Collect() KubernetesSnapshot {
	snap := KubernetesSnapshot{}

	// Collect pods, top, hpa, events in sequence (kubectl might not like parallel)
	pods, err := k.getPods()
	if err != nil {
		snap.Err = fmt.Errorf("get pods: %w", err)
		return snap
	}
	snap.Pods = pods

	// Merge CPU/memory from kubectl top and compute usage percentages
	topMetrics := k.getTopPods()
	for i, p := range snap.Pods {
		if m, ok := topMetrics[p.Name]; ok {
			snap.Pods[i].CPUUsage = m.cpu
			snap.Pods[i].MemUsage = m.mem

			// Compute usage/limit percentages
			cpuUsageM := parseCPUMilli(m.cpu)
			cpuLimitM := parseCPUMilli(p.CPULimit)
			if cpuLimitM > 0 {
				snap.Pods[i].CPUPercent = float64(cpuUsageM) / float64(cpuLimitM) * 100
			}

			memUsageMi := parseMemMi(m.mem)
			memLimitMi := parseMemMi(p.MemLimit)
			if memLimitMi > 0 {
				snap.Pods[i].MemPercent = float64(memUsageMi) / float64(memLimitMi) * 100
			}
		}
	}

	hpas, err := k.getHPAs()
	if err == nil {
		snap.HPAs = hpas
	}

	events, err := k.getEvents()
	if err == nil {
		snap.Events = events
	}

	return snap
}

func (k *KubernetesCollector) kubectl(args ...string) ([]byte, error) {
	allArgs := append([]string{"--kubeconfig", k.kubeconfig, "-n", k.namespace}, args...)
	cmd := exec.Command("kubectl", allArgs...)
	cmd.Env = append(cmd.Environ(), "KUBECONFIG="+k.kubeconfig)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%s: %s", err, string(ee.Stderr))
		}
		return nil, err
	}
	return out, nil
}

func (k *KubernetesCollector) getPods() ([]PodInfo, error) {
	out, err := k.kubectl("get", "pods", "-o", "json")
	if err != nil {
		return nil, err
	}

	var podList struct {
		Items []struct {
			Metadata struct {
				Name              string    `json:"name"`
				CreationTimestamp time.Time `json:"creationTimestamp"`
			} `json:"metadata"`
			Spec struct {
				Containers []struct {
					Resources struct {
						Requests map[string]string `json:"requests"`
						Limits   map[string]string `json:"limits"`
					} `json:"resources"`
				} `json:"containers"`
			} `json:"spec"`
			Status struct {
				Phase             string `json:"phase"`
				ContainerStatuses []struct {
					Ready        bool `json:"ready"`
					RestartCount int  `json:"restartCount"`
				} `json:"containerStatuses"`
			} `json:"status"`
		} `json:"items"`
	}

	if err := json.Unmarshal(out, &podList); err != nil {
		return nil, fmt.Errorf("parsing pods: %w", err)
	}

	var pods []PodInfo
	for _, item := range podList.Items {
		restarts := 0
		ready := 0
		total := len(item.Status.ContainerStatuses)
		for _, cs := range item.Status.ContainerStatuses {
			restarts += cs.RestartCount
			if cs.Ready {
				ready++
			}
		}

		age := formatAge(time.Since(item.Metadata.CreationTimestamp))

		// Aggregate resource specs across all containers in the pod
		var cpuReq, cpuLim, memReq, memLim string
		if len(item.Spec.Containers) == 1 {
			c := item.Spec.Containers[0]
			cpuReq = c.Resources.Requests["cpu"]
			cpuLim = c.Resources.Limits["cpu"]
			memReq = c.Resources.Requests["memory"]
			memLim = c.Resources.Limits["memory"]
		} else {
			// Multi-container: sum milliCPU and MiB across containers
			var totalCPUReqM, totalCPULimM, totalMemReqM, totalMemLimM int64
			for _, c := range item.Spec.Containers {
				totalCPUReqM += parseCPUMilli(c.Resources.Requests["cpu"])
				totalCPULimM += parseCPUMilli(c.Resources.Limits["cpu"])
				totalMemReqM += parseMemMi(c.Resources.Requests["memory"])
				totalMemLimM += parseMemMi(c.Resources.Limits["memory"])
			}
			cpuReq = formatCPUMilli(totalCPUReqM)
			cpuLim = formatCPUMilli(totalCPULimM)
			memReq = formatMemMi(totalMemReqM)
			memLim = formatMemMi(totalMemLimM)
		}

		pods = append(pods, PodInfo{
			Name:       item.Metadata.Name,
			Status:     item.Status.Phase,
			Ready:      fmt.Sprintf("%d/%d", ready, total),
			Restarts:   restarts,
			Age:        age,
			CPURequest: cpuReq,
			CPULimit:   cpuLim,
			MemRequest: memReq,
			MemLimit:   memLim,
		})
	}
	return pods, nil
}

type topMetric struct {
	cpu string
	mem string
}

func (k *KubernetesCollector) getTopPods() map[string]topMetric {
	out, err := k.kubectl("top", "pods", "--no-headers")
	if err != nil {
		return nil
	}

	result := make(map[string]topMetric)
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			result[fields[0]] = topMetric{
				cpu: fields[1],
				mem: fields[2],
			}
		}
	}
	return result
}

func (k *KubernetesCollector) getHPAs() ([]HPAInfo, error) {
	out, err := k.kubectl("get", "hpa", "-o", "json")
	if err != nil {
		return nil, err
	}

	var hpaList struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Spec struct {
				MinReplicas *int `json:"minReplicas"`
				MaxReplicas int  `json:"maxReplicas"`
			} `json:"spec"`
			Status struct {
				CurrentReplicas int `json:"currentReplicas"`
				CurrentMetrics  []struct {
					Resource *struct {
						Name    string `json:"name"`
						Current struct {
							AverageUtilization *int `json:"averageUtilization"`
						} `json:"current"`
					} `json:"resource"`
				} `json:"currentMetrics"`
			} `json:"status"`
		} `json:"items"`
	}

	if err := json.Unmarshal(out, &hpaList); err != nil {
		return nil, fmt.Errorf("parsing hpa: %w", err)
	}

	var hpas []HPAInfo
	for _, item := range hpaList.Items {
		minR := 1
		if item.Spec.MinReplicas != nil {
			minR = *item.Spec.MinReplicas
		}

		targets := "unknown"
		for _, m := range item.Status.CurrentMetrics {
			if m.Resource != nil && m.Resource.Current.AverageUtilization != nil {
				targets = fmt.Sprintf("%s: %d%%", m.Resource.Name, *m.Resource.Current.AverageUtilization)
				break
			}
		}

		hpas = append(hpas, HPAInfo{
			Name:        item.Metadata.Name,
			Targets:     targets,
			MinReplicas: minR,
			MaxReplicas: item.Spec.MaxReplicas,
			Current:     item.Status.CurrentReplicas,
		})
	}
	return hpas, nil
}

func (k *KubernetesCollector) getEvents() ([]EventInfo, error) {
	out, err := k.kubectl("get", "events", "--field-selector", "type=Warning", "-o", "json")
	if err != nil {
		return nil, err
	}

	var eventList struct {
		Items []struct {
			Type    string `json:"type"`
			Reason  string `json:"reason"`
			Message string `json:"message"`
			Count   int    `json:"count"`
			InvolvedObject struct {
				Name string `json:"name"`
				Kind string `json:"kind"`
			} `json:"involvedObject"`
			LastTimestamp time.Time `json:"lastTimestamp"`
		} `json:"items"`
	}

	if err := json.Unmarshal(out, &eventList); err != nil {
		return nil, fmt.Errorf("parsing events: %w", err)
	}

	var events []EventInfo
	for _, item := range eventList.Items {
		age := ""
		if !item.LastTimestamp.IsZero() {
			age = formatAge(time.Since(item.LastTimestamp))
		}
		count := item.Count
		if count == 0 {
			count = 1
		}
		events = append(events, EventInfo{
			Type:    item.Type,
			Reason:  item.Reason,
			Object:  item.InvolvedObject.Kind + "/" + item.InvolvedObject.Name,
			Message: item.Message,
			Age:     age,
			Count:   count,
		})
	}
	return events, nil
}

// IsReachable tests kubectl connectivity.
func (k *KubernetesCollector) IsReachable() bool {
	_, err := k.kubectl("get", "namespaces", "--no-headers", "-o", "name")
	return err == nil
}

// parseCPUMilli parses a Kubernetes CPU quantity (e.g. "500m", "4", "4000m") into millicores.
func parseCPUMilli(s string) int64 {
	if s == "" {
		return 0
	}
	if strings.HasSuffix(s, "m") {
		v, _ := strconv.ParseInt(strings.TrimSuffix(s, "m"), 10, 64)
		return v
	}
	// Whole cores (e.g. "4" = 4000m)
	v, _ := strconv.ParseInt(s, 10, 64)
	return v * 1000
}

// parseMemMi parses a Kubernetes memory quantity into MiB.
func parseMemMi(s string) int64 {
	if s == "" {
		return 0
	}
	switch {
	case strings.HasSuffix(s, "Gi"):
		v, _ := strconv.ParseInt(strings.TrimSuffix(s, "Gi"), 10, 64)
		return v * 1024
	case strings.HasSuffix(s, "Mi"):
		v, _ := strconv.ParseInt(strings.TrimSuffix(s, "Mi"), 10, 64)
		return v
	case strings.HasSuffix(s, "Ki"):
		v, _ := strconv.ParseInt(strings.TrimSuffix(s, "Ki"), 10, 64)
		return v / 1024
	default:
		// Plain bytes
		v, _ := strconv.ParseInt(s, 10, 64)
		return v / (1024 * 1024)
	}
}

func formatCPUMilli(m int64) string {
	if m == 0 {
		return ""
	}
	return fmt.Sprintf("%dm", m)
}

func formatMemMi(mi int64) string {
	if mi == 0 {
		return ""
	}
	if mi >= 1024 && mi%1024 == 0 {
		return fmt.Sprintf("%dGi", mi/1024)
	}
	return fmt.Sprintf("%dMi", mi)
}

func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + "d"
	}
}
