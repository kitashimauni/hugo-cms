//go:build !windows

package services

import (
	"context"
	"fmt"
	"hugo-cms/pkg/config"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLocalPreviewManagerStopsPackageManagerWrapperProcessTree(t *testing.T) {
	for _, mode := range []string{"graceful", "ignore"} {
		t.Run(mode, func(t *testing.T) {
			manager, site := newTestLocalPreviewManager(t)
			site.Generator = "eleventy"
			helpers := t.TempDir()
			pidPath := filepath.Join(helpers, "child.pid")
			signalPath := filepath.Join(helpers, "signal")
			manager.commandFactory = func(ctx context.Context, runtime config.SiteRuntime, port int, _ string) (*exec.Cmd, error) {
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLocalPreviewProcessTreeHelper$")
				cmd.Env = append(os.Environ(),
					"HOMECMS_PROCESS_TREE_HELPER=wrapper",
					"HOMECMS_PROCESS_TREE_MODE="+mode,
					"HOMECMS_PROCESS_TREE_PORT="+strconv.Itoa(port),
					"HOMECMS_PROCESS_TREE_PID_PATH="+pidPath,
					"HOMECMS_PROCESS_TREE_SIGNAL_PATH="+signalPath,
				)
				return cmd, nil
			}

			slot, err := manager.EnsureReady(site)
			if err != nil {
				t.Fatalf("EnsureReady() error = %v", err)
			}
			waitForFile(t, pidPath)

			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			started := time.Now()
			if err := manager.Stop(ctx, site.ID); err != nil {
				t.Fatalf("Stop() error = %v", err)
			}

			if _, ok := manager.Status(site.ID); ok {
				t.Fatal("Stop() should release the lifecycle slot after the process tree exits")
			}
			listener, err := net.Listen("tcp", net.JoinHostPort(LocalPreviewBindAddress, strconv.Itoa(slot.Port)))
			if err != nil {
				t.Fatalf("preview port remains occupied after Stop(): %v", err)
			}
			_ = listener.Close()

			if mode == "graceful" {
				if elapsed := time.Since(started); elapsed >= localPreviewProcessGracePeriod {
					t.Fatalf("graceful process-tree stop took %s; SIGTERM was not handled", elapsed)
				}
				if signal := strings.TrimSpace(readFile(t, signalPath)); signal != "terminated" {
					t.Fatalf("child signal marker = %q, want terminated", signal)
				}
			}
		})
	}
}

func TestLocalPreviewProcessTreeHelper(t *testing.T) {
	if os.Getenv("HOMECMS_PROCESS_TREE_HELPER") != "wrapper" {
		return
	}
	port, err := strconv.Atoi(os.Getenv("HOMECMS_PROCESS_TREE_PORT"))
	if err != nil {
		os.Exit(2)
	}
	mode := os.Getenv("HOMECMS_PROCESS_TREE_MODE")
	pidPath := os.Getenv("HOMECMS_PROCESS_TREE_PID_PATH")
	signalPath := os.Getenv("HOMECMS_PROCESS_TREE_SIGNAL_PATH")

	child := exec.Command(os.Args[0], "-test.run=^TestLocalPreviewProcessTreeChild$")
	child.Env = append(os.Environ(),
		"HOMECMS_PROCESS_TREE_HELPER=child",
		"HOMECMS_PROCESS_TREE_MODE="+mode,
		"HOMECMS_PROCESS_TREE_PORT="+strconv.Itoa(port),
		"HOMECMS_PROCESS_TREE_SIGNAL_PATH="+signalPath,
	)
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		os.Exit(3)
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
		_ = child.Process.Kill()
		os.Exit(4)
	}
	select {}
}

func TestLocalPreviewProcessTreeChild(t *testing.T) {
	if os.Getenv("HOMECMS_PROCESS_TREE_HELPER") != "child" {
		return
	}
	port, err := strconv.Atoi(os.Getenv("HOMECMS_PROCESS_TREE_PORT"))
	if err != nil {
		os.Exit(5)
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(LocalPreviewBindAddress, strconv.Itoa(port)))
	if err != nil {
		os.Exit(6)
	}
	defer listener.Close()

	server := &httpServerForProcessTree{listener: listener}
	go server.serve()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	for {
		sig := <-signals
		if os.Getenv("HOMECMS_PROCESS_TREE_MODE") == "ignore" {
			continue
		}
		if err := os.WriteFile(os.Getenv("HOMECMS_PROCESS_TREE_SIGNAL_PATH"), []byte("terminated"), 0600); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		_ = sig
		return
	}
}

type httpServerForProcessTree struct {
	listener net.Listener
}

func (s *httpServerForProcessTree) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok\r\n"))
		_ = conn.Close()
	}
}

func waitForFile(t *testing.T, filename string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filename); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", filename)
}

func readFile(t *testing.T, filename string) string {
	t.Helper()
	contents, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("read %s: %v", filename, err)
	}
	return string(contents)
}
