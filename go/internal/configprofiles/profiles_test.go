// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package configprofiles_test

import (
	"os"
	"path/filepath"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/configdata"
	"blackcat.ca/coding-ethos/go/internal/configprofiles"
)

func TestGoStaticSiteProfileDisablesPythonGatesWithoutPythonSources(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	merged := configprofiles.Apply(
		configdata.Map{},
		configdata.Map{"repo": map[string]any{"kind": "go-static-site"}},
		repo,
	)

	assertConfigPath(t, merged, "go.enabled", true)
	assertConfigPath(t, merged, "python.pytest_gate.enabled", false)
	assertConfigPath(t, merged, "python.docstring_coverage.enabled", false)
	assertConfigPath(t, merged, "python.type_check.enabled", false)
}

func TestGoStaticSiteProfileKeepsPythonGatesWhenPythonSourcesExist(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()

	err := os.WriteFile(filepath.Join(repo, "tool.py"), []byte("print('ok')\n"), 0o600)
	if err != nil {
		t.Fatalf("write python source: %v", err)
	}

	merged := configprofiles.Apply(
		configdata.Map{"python": map[string]any{
			"pytest_gate": map[string]any{"enabled": true},
		}},
		configdata.Map{"profiles": []any{"go"}},
		repo,
	)

	assertConfigPath(t, merged, "go.enabled", true)
	assertConfigPath(t, merged, "python.pytest_gate.enabled", true)
}

func TestGoStaticSiteProfileIgnoresPythonSourcesInEthosCheckout(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	ethos := filepath.Join(repo, "vendor", "policy-tools")

	err := os.MkdirAll(ethos, 0o755)
	if err != nil {
		t.Fatalf("create ethos checkout: %v", err)
	}

	err = os.WriteFile(filepath.Join(ethos, "tool.py"), []byte("print('ok')\n"), 0o600)
	if err != nil {
		t.Fatalf("write ethos python source: %v", err)
	}

	merged := configprofiles.ApplyWithEthosRoot(
		configdata.Map{},
		configdata.Map{"profiles": []any{"go"}},
		repo,
		ethos,
	)

	assertConfigPath(t, merged, "python.pytest_gate.enabled", false)
}

func TestExplicitRepoConfigOverridesProfileDefaults(t *testing.T) {
	t.Parallel()

	merged := configprofiles.Apply(
		configdata.Map{},
		configdata.Map{
			"repo": map[string]any{"kind": "go-static-site"},
			"python": map[string]any{
				"pytest_gate": map[string]any{"enabled": true},
			},
		},
		t.TempDir(),
	)

	assertConfigPath(t, merged, "python.pytest_gate.enabled", true)
}

func assertConfigPath(t *testing.T, config configdata.Map, path string, want any) {
	t.Helper()

	if got := configdata.GetPath(config, path, nil); got != want {
		t.Fatalf("%s = %#v, want %#v", path, got, want)
	}
}
