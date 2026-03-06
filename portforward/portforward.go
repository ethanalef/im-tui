package portforward

import (
	"context"
	"fmt"
	"net"
	"os/exec"
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

	mu     sync.Mutex
	cmd    *exec.Cmd
	cancel context.CancelFunc
	ready  bool
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
	defer pf.mu.Unlock()

	if pf.cmd != nil {
		return nil // already running
	}

	ctx, cancel := context.WithCancel(context.Background())
	pf.cancel = cancel

	mapping := fmt.Sprintf("%d:%d", pf.localPort, pf.remotePort)
	pf.cmd = exec.CommandContext(ctx, "kubectl",
		"--kubeconfig", pf.kubeconfig,
		"port-forward",
		"-n", pf.namespace,
		pf.service,
		mapping,
	)

	// Start in background
	if err := pf.cmd.Start(); err != nil {
		pf.cmd = nil
		cancel()
		return fmt.Errorf("starting port-forward: %w", err)
	}

	// Wait for local port to be reachable
	addr := fmt.Sprintf("127.0.0.1:%d", pf.localPort)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			conn.Close()
			pf.ready = true
			return nil
		}

		// Check if process exited early
		if pf.cmd.ProcessState != nil {
			return fmt.Errorf("port-forward exited prematurely")
		}

		time.Sleep(200 * time.Millisecond)
	}

	// Timed out - kill and cleanup
	pf.Stop()
	return fmt.Errorf("port-forward to %s timed out", pf.service)
}

// Stop kills the port-forward subprocess.
func (pf *PortForward) Stop() {
	pf.mu.Lock()
	defer pf.mu.Unlock()

	if pf.cancel != nil {
		pf.cancel()
	}
	if pf.cmd != nil && pf.cmd.Process != nil {
		pf.cmd.Process.Kill()
		pf.cmd.Wait()
	}
	pf.cmd = nil
	pf.ready = false
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

func findFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 9090 // fallback
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}
