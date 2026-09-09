// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !windows

package e2e

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	processGroupHelperMode = "CODING_ETHOS_E2E_PROCESS_GROUP_HELPER"
	processGroupReadyFD    = "CODING_ETHOS_E2E_PROCESS_GROUP_READY_FD"
	processGroupChildPID   = "child-pid="
)

func TestCommandCancellationTerminatesProcessGroup(t *testing.T) {
	if mode := os.Getenv(processGroupHelperMode); mode != "" {
		runProcessGroupHelper(t, mode)

		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(
		ctx,
		os.Args[0],
		"-test.run=^TestCommandCancellationTerminatesProcessGroup$",
	)
	readyReader, readyWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("create process-group readiness pipe: %v", err)
	}
	defer func() { _ = readyReader.Close() }()

	cmd.Env = append(
		os.Environ(),
		processGroupHelperMode+"=parent",
		processGroupReadyFD+"=3",
	)
	cmd.ExtraFiles = []*os.File{readyWriter}
	configureCommandProcessGroup(cmd)
	configureCommandCancellation(cmd)

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Start(); err != nil {
		_ = readyWriter.Close()
		t.Fatalf("start process-group helper: %v", err)
	}
	if err := readyWriter.Close(); err != nil {
		cancel()
		_ = cmd.Wait()
		t.Fatalf("close process-group readiness writer: %v", err)
	}

	if err := readyReader.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		cancel()
		_ = cmd.Wait()
		t.Fatalf("bound process-group readiness read: %v", err)
	}
	line, err := bufio.NewReader(readyReader).ReadString('\n')
	if err != nil {
		cancel()
		_ = cmd.Wait()
		t.Fatalf("process-group helper did not become ready: %v\n%s", err, &output)
	}
	childPID := processGroupHelperChildPID(t, line)

	started := time.Now()
	cancel()
	err = cmd.Wait()
	elapsed := time.Since(started)
	if err == nil || !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("canceled process group returned err=%v context=%v", err, ctx.Err())
	}
	if elapsed >= time.Second {
		t.Fatalf("ready process-group cancellation took %s", elapsed)
	}

	assertProcessExited(t, childPID)
}

func runProcessGroupHelper(t *testing.T, mode string) {
	t.Helper()

	if mode == "child" {
		time.Sleep(30 * time.Second)

		return
	}
	if mode != "parent" {
		t.Fatalf("unknown process-group helper mode %q", mode)
	}
	readyDescriptor, err := strconv.Atoi(os.Getenv(processGroupReadyFD))
	if err != nil {
		t.Fatalf("parse process-group readiness descriptor: %v", err)
	}
	ready := os.NewFile(uintptr(readyDescriptor), "process-group-ready")
	if ready == nil {
		t.Fatal("open process-group readiness descriptor")
	}
	defer func() { _ = ready.Close() }()
	syscall.CloseOnExec(readyDescriptor)

	child := exec.Command(
		os.Args[0],
		"-test.run=^TestCommandCancellationTerminatesProcessGroup$",
	)
	child.Env = append(os.Environ(), processGroupHelperMode+"=child")
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		t.Fatalf("start process-group child: %v", err)
	}
	if _, err := ready.WriteString(
		processGroupChildPID + strconv.Itoa(child.Process.Pid) + "\n",
	); err != nil {
		t.Fatalf("write process-group readiness: %v", err)
	}
	if err := ready.Close(); err != nil {
		t.Fatalf("close process-group readiness: %v", err)
	}
	time.Sleep(30 * time.Second)
}

func processGroupHelperChildPID(t *testing.T, line string) int {
	t.Helper()

	value, found := strings.CutPrefix(strings.TrimSpace(line), processGroupChildPID)
	if !found {
		t.Fatalf("invalid process-group readiness line %q", line)
	}
	pid, err := strconv.Atoi(value)
	if err != nil {
		t.Fatalf("parse process-group child PID %q: %v", value, err)
	}

	return pid
}

func assertProcessExited(t *testing.T, pid int) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process-group child %d survived cancellation", pid)
}
