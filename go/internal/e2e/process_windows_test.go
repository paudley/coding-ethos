// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build windows

package e2e

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

const (
	windowsProcessTreeHelperMode = "CODING_ETHOS_E2E_WINDOWS_PROCESS_TREE_HELPER"
	windowsProcessTreeChildPID   = "child-pid="
)

type windowsProcessTreeReady struct {
	Err  error
	Line string
}

func TestCommandCancellationTerminatesWindowsProcessTree(t *testing.T) {
	if mode := os.Getenv(windowsProcessTreeHelperMode); mode != "" {
		runWindowsProcessTreeHelper(t, mode)

		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(
		ctx,
		os.Args[0],
		"-test.run=^TestCommandCancellationTerminatesWindowsProcessTree$",
	)
	cmd.Env = append(os.Environ(), windowsProcessTreeHelperMode+"=parent")
	configureCommandProcessGroup(cmd)
	configureCommandCancellation(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("open process-tree stdout: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err = cmd.Start(); err != nil {
		t.Fatalf("start process-tree helper: %v", err)
	}

	ready := make(chan windowsProcessTreeReady, 1)
	go func() {
		line, readErr := bufio.NewReader(stdout).ReadString('\n')
		ready <- windowsProcessTreeReady{Err: readErr, Line: line}
	}()

	var childPID int
	select {
	case result := <-ready:
		if result.Err != nil {
			cancel()
			_ = cmd.Wait()
			t.Fatalf("process-tree helper did not become ready: %v", result.Err)
		}
		childPID = windowsProcessTreeHelperChildPID(t, result.Line)
	case <-time.After(10 * time.Second):
		cancel()
		_ = cmd.Wait()
		t.Fatal("process-tree helper readiness timed out")
	}

	cancel()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	select {
	case err = <-waited:
		if err == nil || !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatalf("canceled process tree returned err=%v context=%v", err, ctx.Err())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled process tree did not return")
	}

	assertWindowsProcessExited(t, childPID)
}

func runWindowsProcessTreeHelper(t *testing.T, mode string) {
	t.Helper()

	if mode == "child" {
		time.Sleep(30 * time.Second)

		return
	}
	if mode != "parent" {
		t.Fatalf("unknown process-tree helper mode %q", mode)
	}

	child := exec.Command(
		os.Args[0],
		"-test.run=^TestCommandCancellationTerminatesWindowsProcessTree$",
	)
	child.Env = append(os.Environ(), windowsProcessTreeHelperMode+"=child")
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		t.Fatalf("start process-tree child: %v", err)
	}
	if _, err := os.Stdout.WriteString(
		windowsProcessTreeChildPID + strconv.Itoa(child.Process.Pid) + "\n",
	); err != nil {
		t.Fatalf("write process-tree readiness: %v", err)
	}
	time.Sleep(30 * time.Second)
}

func windowsProcessTreeHelperChildPID(t *testing.T, line string) int {
	t.Helper()

	value, found := strings.CutPrefix(
		strings.TrimSpace(line),
		windowsProcessTreeChildPID,
	)
	if !found {
		t.Fatalf("invalid process-tree readiness line %q", line)
	}
	pid, err := strconv.Atoi(value)
	if err != nil {
		t.Fatalf("parse process-tree child PID %q: %v", value, err)
	}

	return pid
}

func assertWindowsProcessExited(t *testing.T, pid int) {
	t.Helper()

	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return
	}
	if err != nil {
		t.Fatalf("open process-tree child %d: %v", pid, err)
	}
	defer func() { _ = windows.CloseHandle(handle) }()

	status, err := windows.WaitForSingleObject(
		handle,
		uint32((5*time.Second)/time.Millisecond),
	)
	if err != nil {
		t.Fatalf("wait for process-tree child %d: %v", pid, err)
	}
	if status != windows.WAIT_OBJECT_0 {
		t.Fatalf("process-tree child %d survived cancellation", pid)
	}
}
