// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package webguidance

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadSettingsMergesConfigAndRepoOverride(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "config.toml"), `
[web_guidance.modern_web]
cache_ttl = "12h"
allow_network_refresh = true
browser_policy = "baseline"
`)
	writeFile(t, filepath.Join(root, "repo_config.toml"), `
[web_guidance.modern_web]
cache_ttl = "2h"
allow_network_refresh = false
`)

	settings, err := LoadSettings(root)
	if err != nil {
		t.Fatalf("LoadSettings returned error: %v", err)
	}

	if settings.ModernWeb.CacheTTL != 2*time.Hour ||
		settings.ModernWeb.AllowNetworkRefresh ||
		settings.ModernWeb.BrowserPolicy != "baseline" {
		t.Fatalf("settings were not merged with repo override: %#v", settings)
	}
}

func TestLoadSettingsRejectsUnknownWebGuidanceConfig(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "repo_config.toml"), `
[web_guidance.unknown]
enabled = true
`)

	_, err := LoadSettings(root)
	if err == nil || !errors.Is(err, errUnknownWebGuidanceConfigPath) {
		t.Fatalf("LoadSettings error = %v, want unknown path", err)
	}
}

func TestLoadSettingsRejectsInvalidCacheTTL(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "repo_config.toml"), `
[web_guidance.modern_web]
cache_ttl = "soon"
`)

	_, err := LoadSettings(root)
	if err == nil || !errors.Is(err, errInvalidWebGuidanceDuration) {
		t.Fatalf("LoadSettings error = %v, want invalid duration", err)
	}
}
