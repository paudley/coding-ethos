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
)

const (
	adminPIDFile       = "/etc/coding-ethos-admin.pids"
	procStatMinFields  = 2
	procStatPPIDOffset = 1
)

var (
	errMalformedProcStat = errors.New("malformed process stat")
	errAdminRepoRequired = errors.New(
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
		"pre-commit/hooks/run-go-hook.sh",
	} {
		_, err := os.Stat(filepath.Join(path, marker))
		if err != nil {
			return false
		}
	}

	return true
}

func processAncestryApproved(pid int, path string) (bool, error) {
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

func readApprovedPIDs(path string) (map[int]bool, error) {
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
