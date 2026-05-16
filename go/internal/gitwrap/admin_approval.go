// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package gitwrap

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

const (
	adminPIDFile       = "/etc/coding-ethos-admin.pids"
	procStatMinFields  = 2
	procStatPPIDOffset = 1
)

var (
	errMalformedProcStat = apperror.StaticError("malformed process stat")
	errAdminRepoRequired = apperror.StaticError(
		"--admin-approved is only valid inside the coding-ethos repository",
	)
	errAdminPIDRequired = errors.New(
		"--admin-approved requires an approved process ancestor in " +
			"/etc/coding-ethos-admin.pids",
	)
)

func VerifyAdminApproved(cwd string) error {
	if !isCodingEthosRepo(cwd) {
		return errAdminRepoRequired
	}

	approved, err := processAncestryApproved(os.Getpid(), adminPIDFile)
	if err != nil {
		return err
	}

	if !approved {
		return errAdminPIDRequired
	}

	return nil
}

func isCodingEthosRepo(cwd string) bool {
	return IsCodingEthosRepo(cwd)
}

// IsCodingEthosRepo reports whether cwd is inside this repository.
func IsCodingEthosRepo(cwd string) bool {
	if cwd == "" {
		return false
	}

	current, err := filepath.Abs(cwd)
	if err != nil {
		return false
	}

	for {
		if codingEthosRepoMarker(current) {
			return true
		}

		parent := filepath.Dir(current)
		if parent == current {
			return false
		}

		current = parent
	}
}

func codingEthosRepoMarker(path string) bool {
	if filepath.Base(path) != "coding-ethos" {
		return false
	}

	for _, marker := range []string{
		"coding_ethos.yml",
		"config.yaml",
		"go/go.mod",
		"bin/coding-ethos-run",
	} {
		_, err := os.Stat(filepath.Join(path, marker))
		if err != nil {
			return false
		}
	}

	return true
}

func processAncestryApproved(pid int, path string) (bool, error) {
	return ProcessAncestryApproved(pid, path)
}

// ProcessAncestryApproved checks pid and its parents against an admin PID file.
func ProcessAncestryApproved(pid int, path string) (bool, error) {
	approvedPIDs, err := readApprovedPIDs(path)
	if err != nil {
		return false, err
	}

	for current := pid; current > 0; {
		if approvedPIDs[current] {
			return true, nil
		}

		parent, err := parentPID(current)
		if err != nil {
			return false, err
		}

		if parent == current {
			return false, nil
		}

		current = parent
	}

	return false, nil
}

// ProcessAncestryContains reports whether ancestorPID appears in pid's process
// ancestry. It fails closed when process ancestry cannot be inspected.
func ProcessAncestryContains(pid, ancestorPID int) (bool, error) {
	for current := pid; current > 0; {
		if current == ancestorPID {
			return true, nil
		}

		parent, err := parentPID(current)
		if err != nil {
			return false, err
		}

		if parent == current {
			return false, nil
		}

		current = parent
	}

	return false, nil
}

// ProcessCommandLine returns a process argv from /proc. Empty command lines are
// treated as unavailable evidence by callers that require provenance.
func ProcessCommandLine(pid int) ([]string, error) {
	payload, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return nil, fmt.Errorf("read process cmdline for pid %d: %w", pid, err)
	}

	parts := strings.Split(strings.TrimRight(string(payload), "\x00"), "\x00")

	args := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			args = append(args, part)
		}
	}

	return args, nil
}

func readApprovedPIDs(path string) (map[int]bool, error) {
	return ReadApprovedPIDs(path)
}

// ReadApprovedPIDs loads the approved admin process IDs from path.
func ReadApprovedPIDs(path string) (map[int]bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open admin pid file: %w", err)
	}
	defer file.Close()

	approved := map[int]bool{}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		pid, parseErr := strconv.Atoi(line)
		if parseErr != nil {
			return nil, fmt.Errorf("parse admin pid %q: %w", line, parseErr)
		}

		approved[pid] = true
	}

	err = scanner.Err()
	if err != nil {
		return nil, fmt.Errorf("read admin pid file: %w", err)
	}

	return approved, nil
}

func parentPID(pid int) (int, error) {
	payload, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, fmt.Errorf("read process stat for pid %d: %w", pid, err)
	}

	stat := string(payload)

	closeIndex := strings.LastIndex(stat, ")")
	if closeIndex < 0 || closeIndex+2 >= len(stat) {
		return 0, fmt.Errorf(
			"read process stat for pid %d: %w",
			pid,
			errMalformedProcStat,
		)
	}

	fields := strings.Fields(stat[closeIndex+2:])
	if len(fields) < procStatMinFields {
		return 0, fmt.Errorf(
			"read process stat for pid %d: %w",
			pid,
			errMalformedProcStat,
		)
	}

	parent, err := strconv.Atoi(fields[procStatPPIDOffset])
	if err != nil {
		return 0, fmt.Errorf("parse parent pid for pid %d: %w", pid, err)
	}

	return parent, nil
}
