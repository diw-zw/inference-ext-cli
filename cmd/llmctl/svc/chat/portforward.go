/*
Copyright 2026 The RBG Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package chat

import (
	"bufio"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// PortForwardSession manages a kubectl port-forward subprocess lifetime.
type PortForwardSession struct {
	cmd      *exec.Cmd
	exitChan chan struct{} // closed when process exits, broadcasts to all waiters
	mu       sync.RWMutex
	alive    bool
	stopped  bool
}

// StartPortForward spawns a kubectl port-forward to the given pod and waits
// until the tunnel is ready (or returns an error). readyTimeout controls how
// long to wait for the "Forwarding from" confirmation line. The caller must
// call Stop() when the session is no longer needed.
func StartPortForward(kubeconfig, namespace, podName string, localPort, remotePort int32, readyTimeout time.Duration) (*PortForwardSession, error) {
	args := []string{
		"port-forward",
		"-n", namespace,
		podName,
		fmt.Sprintf("%d:%d", localPort, remotePort),
	}
	if kubeconfig != "" {
		args = append([]string{"--kubeconfig", kubeconfig}, args...)
	}

	cmd := exec.Command("kubectl", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("port-forward stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("port-forward stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("port-forward start: %w", err)
	}

	exitChan := make(chan struct{})
	readyChan := make(chan struct{})

	// Capture stderr so it can be included in error messages on failure.
	var (
		stderrMu  sync.Mutex
		stderrBuf strings.Builder
	)
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			stderrMu.Lock()
			stderrBuf.WriteString(line)
			stderrBuf.WriteByte('\n')
			stderrMu.Unlock()
		}
	}()

	// Read stdout; signal readyChan when forwarding is confirmed.
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "Forwarding from") {
				select {
				case <-readyChan:
				default:
					close(readyChan)
				}
			}
		}
	}()

	// Monitor goroutine: wait for process to exit and close exitChan.
	// This is the ONLY goroutine that calls cmd.Wait().
	// Closing exitChan broadcasts to all waiters (Stop() and alive monitor).
	go func() {
		_ = cmd.Wait()
		close(exitChan)
	}()

	// Wait for ready signal, process exit, or timeout.
	select {
	case <-readyChan:
		// Tunnel is up.
	case <-exitChan:
		var err error
		if cmd.ProcessState != nil && !cmd.ProcessState.Success() {
			err = fmt.Errorf("port-forward exited with code %d", cmd.ProcessState.ExitCode())
		} else {
			err = fmt.Errorf("port-forward exited unexpectedly")
		}
		stderrMu.Lock()
		stderrOutput := strings.TrimSpace(stderrBuf.String())
		stderrMu.Unlock()
		if stderrOutput != "" {
			return nil, fmt.Errorf("port-forward failed before becoming ready: %w\nkubectl stderr: %s", err, stderrOutput)
		}
		return nil, fmt.Errorf("port-forward failed before becoming ready: %w", err)
	case <-time.After(readyTimeout):
		// Kill the process on timeout
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return nil, fmt.Errorf("timeout waiting for port-forward to become ready")
	}

	session := &PortForwardSession{
		cmd:      cmd,
		exitChan: exitChan,
		alive:    true,
	}

	// Update alive status when process exits
	go func() {
		<-exitChan
		session.mu.Lock()
		session.alive = false
		session.mu.Unlock()
	}()

	return session, nil
}

// IsAlive returns true if the port-forward process is still running.
func (s *PortForwardSession) IsAlive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.alive
}

// Stop terminates the port-forward subprocess.
func (s *PortForwardSession) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	s.mu.Unlock()

	// Kill the process if it's still running
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}

	// Wait for process to exit (exitChan closed by monitor goroutine)
	// Multiple goroutines can safely receive from a closed channel.
	<-s.exitChan
}
