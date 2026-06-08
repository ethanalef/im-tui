package portforward

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// PortForward manages a kubectl port-forward subprocess.
type PortForward struct {
	kubeconfig string
	namespace  string
	service    string
	remotePort int
	localPort  int

	restartMu sync.Mutex
	mu        sync.Mutex
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	done      chan error
	ready     bool
}

// New creates a port-forward manager. localPort=0 means pick a free port.
func New(kubeconfig, namespace, service string, remotePort, localPort int) *PortForward {
	if localPort == 0 {
		localPort = findFreePort()
	}
	return &PortForward{
		kubeconfig: kubeconfig,
		namespace:  namespace,
		service:    service,
		remotePort: remotePort,
		localPort:  localPort,
	}
}

// Start launches the port-forward subprocess and waits until the local port
// is accepting connections (or timeout).
func (pf *PortForward) Start() error {
	pf.mu.Lock()
	if pf.cmd != nil {
		select {
		case <-pf.done:
			pf.cmd = nil
			pf.cancel = nil
			pf.done = nil
			pf.ready = false
		default:
			pf.mu.Unlock()
			return nil // already running
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "kubectl",
		"--kubeconfig", pf.kubeconfig,
		"port-forward",
		"-n", pf.namespace,
		pf.service,
		fmt.Sprintf("%d:%d", pf.localPort, pf.remotePort),
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		cancel()
		pf.mu.Unlock()
		return fmt.Errorf("starting port-forward: %w", err)
	}

	exited := make(chan error, 1)
	go func() {
		exited <- cmd.Wait()
	}()

	pf.cmd = cmd
	pf.cancel = cancel
	pf.done = exited
	pf.ready = false
	pf.mu.Unlock()

	addr := fmt.Sprintf("127.0.0.1:%d", pf.localPort)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			conn.Close()
			pf.mu.Lock()
			pf.ready = true
			pf.mu.Unlock()
			return nil
		}

		select {
		case waitErr := <-exited:
			pf.clearIfCurrent(cmd)
			errMsg := strings.TrimSpace(stderr.String())
			if errMsg != "" {
				return fmt.Errorf("port-forward exited: %s", errMsg)
			}
			if waitErr != nil {
				return fmt.Errorf("port-forward exited: %w", waitErr)
			}
			return fmt.Errorf("port-forward exited prematurely")
		default:
		}

		time.Sleep(200 * time.Millisecond)
	}

	cancel()
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	<-exited
	pf.clearIfCurrent(cmd)
	errMsg := strings.TrimSpace(stderr.String())
	if errMsg != "" {
		return fmt.Errorf("port-forward to %s timed out: %s", pf.service, errMsg)
	}
	return fmt.Errorf("port-forward to %s timed out", pf.service)
}

// Stop kills the port-forward subprocess.
func (pf *PortForward) Stop() {
	pf.mu.Lock()
	cmd := pf.cmd
	cancel := pf.cancel
	done := pf.done

	if cancel != nil {
		cancel()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	pf.mu.Unlock()

	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}

	pf.clearIfCurrent(cmd)
}

// Restart recreates the subprocess on the same local port.
func (pf *PortForward) Restart() error {
	pf.restartMu.Lock()
	defer pf.restartMu.Unlock()
	pf.Stop()
	return pf.Start()
}

// LocalURL returns the local URL to connect to.
func (pf *PortForward) LocalURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", pf.localPort)
}

// LocalPort returns the assigned local port.
func (pf *PortForward) LocalPort() int {
	return pf.localPort
}

// IsReady returns whether the port-forward is established.
func (pf *PortForward) IsReady() bool {
	pf.mu.Lock()
	defer pf.mu.Unlock()
	return pf.ready
}

func (pf *PortForward) clearIfCurrent(cmd *exec.Cmd) {
	pf.mu.Lock()
	defer pf.mu.Unlock()
	if pf.cmd != cmd {
		return
	}
	pf.cmd = nil
	pf.cancel = nil
	pf.done = nil
	pf.ready = false
}

func findFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 9090 // fallback
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}
