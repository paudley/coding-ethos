// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package outputsurface

import (
	"errors"
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

func TestLoadSettingsParsesRetentionUnits(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeConfigFixture(t, root, "repo_config.toml", `
[outputs.report]
default_format = "human"

[outputs.prune]
enabled = "true"
auto_enabled = "true"

[outputs.prune.surfaces.lint_traces]
enabled = "true"
auto = "true"
max_age = "2d"
keep_last = "3"
max_bytes = "5KiB"
require_code_intel_ingest = "false"
vacuum_after_prune = "true"
`)

	settings, err := LoadSettings(root)
	if err != nil {
		t.Fatalf("LoadSettings returned error: %v", err)
	}

	policy := settings.Prune.Surfaces["lint_traces"]
	if settings.Report.DefaultFormat != "human" ||
		!settings.Prune.Enabled ||
		!settings.Prune.AutoEnabled ||
		!policy.Enabled ||
		!policy.Auto ||
		policy.MaxAge != 48*time.Hour ||
		policy.KeepLast != 3 ||
		policy.MaxBytes != 5*kibibyte ||
		policy.RequireCodeIntelIngest ||
		!policy.VacuumAfterPrune {
		t.Fatalf("parsed settings = %#v", settings)
	}
}

func TestLoadSettingsRejectsInvalidDuration(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeConfigFixture(t, root, "repo_config.toml", `
[outputs.prune.surfaces.lint_traces]
max_age = "soon"
`)

	_, err := LoadSettings(root)
	if err == nil {
		t.Fatal("LoadSettings accepted invalid max_age")
	}
}

func TestLoadSettingsRejectsInvalidBytes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeConfigFixture(t, root, "repo_config.toml", `
[outputs.prune.surfaces.lint_traces]
max_bytes = "large"
`)

	_, err := LoadSettings(root)
	if err == nil {
		t.Fatal("LoadSettings accepted invalid max_bytes")
	}
}

func TestLoadSettingsRejectsInvalidIntegerPolicy(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeConfigFixture(t, root, "repo_config.toml", `
[outputs.prune.surfaces.hook_runs]
keep_last = "many"
`)

	_, err := LoadSettings(root)
	if err == nil {
		t.Fatal("LoadSettings accepted invalid keep_last")
	}
}

func TestLoadSettingsRejectsNonTablePruneSurfacesWithSpecificError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeConfigFixture(t, root, "repo_config.toml", `
[outputs.prune]
surfaces = "bad"
`)

	_, err := LoadSettings(root)
	if err == nil || !errors.Is(err, errPruneSurfacesConfigMustBeTable) {
		t.Fatalf("LoadSettings error = %v, want prune surfaces table error", err)
	}
}

func TestLoadSettingsRejectsUnknownOutputConfigPaths(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"outputs": `
[outputs.unknown]
enabled = true
`,
		"report": `
[outputs.report]
unknown = true
`,
		"prune": `
[outputs.prune]
unknown = true
`,
		"surface": `
[outputs.prune.surfaces.lint_traces]
unknown = true
`,
	}

	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			writeConfigFixture(t, root, "repo_config.toml", payload)

			_, err := LoadSettings(root)
			if err == nil {
				t.Fatal("LoadSettings accepted unknown output config path")
			}
		})
	}
}

func TestConfigParsingHelpersCoverUnitsAndFallbacks(t *testing.T) {
	t.Parallel()

	duration, err := ParseDuration("30d")
	if err != nil {
		t.Fatalf("ParseDuration returned error: %v", err)
	}
	if duration != 30*24*time.Hour {
		t.Fatalf("duration = %s, want 720h", duration)
	}

	bytes, err := parseBytesText("2MiB")
	if err != nil {
		t.Fatalf("parseBytesText returned error: %v", err)
	}
	if bytes != 2*kibibyte*kibibyte {
		t.Fatalf("bytes = %d, want 2 MiB", bytes)
	}
	if bytesText(0) != "" || bytesText(512) != "512" {
		t.Fatalf("bytesText returned unexpected values")
	}
	if !boolValue(" TRUE ") || boolValue("false") {
		t.Fatalf("boolValue did not parse string booleans")
	}
	intValueFromInt32, err := intValue(int32(7))
	if err != nil || intValueFromInt32 != 7 {
		t.Fatalf("intValue did not parse supported fallbacks")
	}
	intValueFromFloat, err := intValue(9.5)
	if err != nil || intValueFromFloat != 9 {
		t.Fatalf("intValue did not parse supported fallbacks")
	}
	if _, err := intValue("bad"); err == nil {
		t.Fatalf("intValue accepted invalid string")
	}
}

func writeConfigFixture(t *testing.T, root, name, payload string) {
	t.Helper()

	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
