// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package outputsurface

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadSettingsMergesConfigTomlAndRepoOverride(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeConfigFixture(t, root, "config.toml", `
[outputs.prune.surfaces.proxy_temp_evidence]
max_age = "48h"
`)
	writeConfigFixture(t, root, "repo_config.toml", `
[outputs.report]
include_temp = true

[outputs.prune.surfaces.proxy_temp_evidence]
max_age = "12h"
`)

	settings, err := LoadSettings(root)
	if err != nil {
		t.Fatalf("LoadSettings returned error: %v", err)
	}

	if !settings.Report.IncludeTemp {
		t.Fatal("repo_config.toml include_temp override was not applied")
	}
	policy := settings.Prune.Surfaces["proxy_temp_evidence"]
	if policy.MaxAge != 12*time.Hour {
		t.Fatalf("proxy max age = %s, want 12h", policy.MaxAge)
	}
}

func TestLoadSettingsRejectsUnknownSurface(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeConfigFixture(t, root, "config.toml", `
[outputs.prune.surfaces.unknown]
max_age = "1h"
`)

	_, err := LoadSettings(root)
	if err == nil {
		t.Fatal("LoadSettings accepted unknown surface")
	}
}

func writeConfigFixture(t *testing.T, root, name, payload string) {
	t.Helper()

	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
