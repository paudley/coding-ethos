// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package lintcapture_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"blackcat.ca/coding-ethos/go/lintcapture"
)

func TestProxyInterceptionDefaults(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ethos := filepath.Join(root, "coding-ethos")
	consumer := filepath.Join(root, "consumer")
	writeFile(t, filepath.Join(ethos, "config.yaml"), "python:\n  extra_paths: []\n")

	config, err := lintcapture.LoadRuntimeConfig(ethos, consumer)
	if err != nil {
		t.Fatalf("LoadRuntimeConfig(): %v", err)
	}

	if got := config.ProxyInterceptionMode(); got != "off" {
		t.Fatalf("mode default = %q, want off", got)
	}

	if got := config.ProxyInterceptionOnError(); got != "fail_closed" {
		t.Fatalf("on_error default = %q, want fail_closed", got)
	}

	if got := config.ProxyInterceptionMaxNormalizeBytes(); got != 8*1024*1024 {
		t.Fatalf("max_normalize default = %d, want 8 MiB", got)
	}

	if got := config.ProxyInterceptionAllowHosts(); got != nil {
		t.Fatalf("allow_hosts default = %#v, want nil", got)
	}

	if got := config.ProxyInterceptionCAApproval(); got != "" {
		t.Fatalf("ca_approval default = %q, want empty", got)
	}
}

func TestProxyInterceptionExplicitValues(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ethos := filepath.Join(root, "coding-ethos")
	consumer := filepath.Join(root, "consumer")
	repoConfig := filepath.Join(root, "explicit.yaml")
	writeFile(t, filepath.Join(ethos, "config.yaml"), "python:\n  extra_paths: []\n")
	writeFile(t, repoConfig, `
proxy:
  interception:
    mode: required
    on_error: passthrough
    ca_approval: sha256:abc123
    max_normalize_bytes: 4096
    allow_hosts:
      - API.OpenAI.com
      - " "
      - api.anthropic.com
`)

	config, err := lintcapture.LoadRuntimeConfigWithRepoConfig(ethos, consumer, repoConfig)
	if err != nil {
		t.Fatalf("LoadRuntimeConfigWithRepoConfig(): %v", err)
	}

	if got := config.ProxyInterceptionMode(); got != "required" {
		t.Fatalf("mode = %q, want required", got)
	}

	if got := config.ProxyInterceptionOnError(); got != "passthrough" {
		t.Fatalf("on_error = %q, want passthrough", got)
	}

	if got := config.ProxyInterceptionCAApproval(); got != "sha256:abc123" {
		t.Fatalf("ca_approval = %q", got)
	}

	if got := config.ProxyInterceptionMaxNormalizeBytes(); got != 4096 {
		t.Fatalf("max_normalize = %d, want 4096", got)
	}

	want := []string{"api.openai.com", "api.anthropic.com"}
	if got := config.ProxyInterceptionAllowHosts(); !reflect.DeepEqual(got, want) {
		t.Fatalf("allow_hosts = %#v, want %#v (lowercased, blanks dropped)", got, want)
	}
}

func TestLoadRuntimeConfigWithMissingExplicitRepoConfig(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ethos := filepath.Join(root, "coding-ethos")
	consumer := filepath.Join(root, "consumer")
	writeFile(t, filepath.Join(ethos, "config.yaml"), "python:\n  extra_paths: []\n")

	_, err := lintcapture.LoadRuntimeConfigWithRepoConfig(
		ethos,
		consumer,
		filepath.Join(root, "does-not-exist.yaml"),
	)
	if err == nil {
		t.Fatal("expected error for missing explicit repo config")
	}
}
