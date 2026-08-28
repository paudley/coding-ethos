// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build linux

package sandboxexec

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func writeSSHFixture(t *testing.T, path, content string) {
	t.Helper()

	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}

	err = os.WriteFile(path, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}

func TestInspectSystemSSHConfigCollectsReadableTree(t *testing.T) {
	root := t.TempDir()
	writeSSHFixture(t, filepath.Join(root, "ssh_config"), "Include ssh_config.d/*.conf\n")
	writeSSHFixture(
		t,
		filepath.Join(root, "ssh_config.d", "10-test.conf"),
		"Host example\n",
	)

	tree, err := inspectSystemSSHConfig(root, os.Getuid())
	if err != nil {
		t.Fatalf("inspectSystemSSHConfig() error = %v", err)
	}

	if tree.needsOwnershipFix {
		t.Fatalf("caller-owned tree must not request a fix: %#v", tree)
	}

	if !slices.Contains(tree.dirs, "ssh_config.d") {
		t.Fatalf("directory not collected: %#v", tree.dirs)
	}

	relatives := make([]string, 0, len(tree.files))
	for _, file := range tree.files {
		relatives = append(relatives, file.relativePath)
	}

	if !slices.Contains(relatives, "ssh_config") ||
		!slices.Contains(relatives, filepath.Join("ssh_config.d", "10-test.conf")) {
		t.Fatalf("files not collected: %#v", relatives)
	}
}

func TestInspectSystemSSHConfigFlagsForeignOwnership(t *testing.T) {
	root := t.TempDir()
	writeSSHFixture(t, filepath.Join(root, "ssh_config"), "Host example\n")

	// Files are owned by the current user; pretending to be a different uid
	// makes that ownership foreign, which is exactly how root-owned files look
	// from inside the single-identity user namespace.
	tree, err := inspectSystemSSHConfig(root, os.Getuid()+1)
	if err != nil {
		t.Fatalf("inspectSystemSSHConfig() error = %v", err)
	}

	if os.Getuid() == 0 {
		if tree.needsOwnershipFix {
			t.Fatalf("root-owned files are always acceptable: %#v", tree)
		}

		return
	}

	if !tree.needsOwnershipFix {
		t.Fatalf("foreign ownership not detected: %#v", tree)
	}
}

func TestInspectSystemSSHConfigMissingRootIsEmpty(t *testing.T) {
	tree, err := inspectSystemSSHConfig(
		filepath.Join(t.TempDir(), "absent"),
		os.Getuid(),
	)
	if err != nil {
		t.Fatalf("inspectSystemSSHConfig() error = %v", err)
	}

	if tree.needsOwnershipFix || len(tree.files) != 0 || len(tree.dirs) != 0 {
		t.Fatalf("missing root must yield an empty tree: %#v", tree)
	}
}

func TestInspectSystemSSHConfigSkipsUnreadableFiles(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can read mode-0000 files, so the skip cannot be observed")
	}

	root := t.TempDir()
	writeSSHFixture(t, filepath.Join(root, "ssh_config"), "Host example\n")

	hidden := filepath.Join(root, "ssh_host_ed25519_key")
	writeSSHFixture(t, hidden, "PRIVATE\n")

	err := os.Chmod(hidden, 0o000)
	if err != nil {
		t.Fatalf("chmod fixture: %v", err)
	}

	tree, err := inspectSystemSSHConfig(root, os.Getuid())
	if err != nil {
		t.Fatalf("inspectSystemSSHConfig() error = %v", err)
	}

	for _, file := range tree.files {
		if file.relativePath == "ssh_host_ed25519_key" {
			t.Fatalf("unreadable file must be left out of the rebuild: %#v", tree)
		}
	}
}

func TestMaterializeSSHConfigTreeRecreatesFiles(t *testing.T) {
	target := t.TempDir()
	tree := sshConfigTree{
		dirs: []string{"ssh_config.d"},
		files: []sshConfigFile{
			{relativePath: "ssh_config", content: []byte("Include ssh_config.d/*.conf\n")},
			{
				relativePath: filepath.Join("ssh_config.d", "10-test.conf"),
				content:      []byte("Host example\n"),
			},
		},
	}

	err := materializeSSHConfigTree(target, tree)
	if err != nil {
		t.Fatalf("materializeSSHConfigTree() error = %v", err)
	}

	for _, file := range tree.files {
		path := filepath.Join(target, file.relativePath)

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read rebuilt file %s: %v", path, readErr)
		}

		if string(content) != string(file.content) {
			t.Fatalf("rebuilt content mismatch for %s: %q", path, content)
		}

		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("stat rebuilt file %s: %v", path, statErr)
		}

		// OpenSSH rejects configuration writable by group or others.
		if info.Mode().Perm()&0o022 != 0 {
			t.Fatalf("rebuilt file %s is too permissive: %v", path, info.Mode())
		}
	}
}

func TestApplySystemSSHConfigSkipsWithoutNetwork(t *testing.T) {
	err := applySystemSSHConfig(options{requiresNetwork: false})
	if err != nil {
		t.Fatalf("network-isolated tools must skip the rebuild: %v", err)
	}
}
