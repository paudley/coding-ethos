// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package configdata_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/configdata"
)

func TestLoadYAMLMapHandlesObjectsAndEmptyFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	configPath := filepath.Join(dir, "config.yaml")
	writeConfigData(t, configPath, "tool:\n  enabled: true\n")

	config, err := configdata.LoadYAMLMap(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if configdata.GetPath(config, "tool.enabled", false) != true {
		t.Fatalf("loaded config = %#v", config)
	}

	emptyPath := filepath.Join(dir, "empty.yaml")
	writeConfigData(t, emptyPath, "")

	empty, err := configdata.LoadYAMLMap(emptyPath)
	if err != nil {
		t.Fatalf("load empty config: %v", err)
	}

	if len(empty) != 0 {
		t.Fatalf("empty config = %#v", empty)
	}
}

func TestLoadYAMLMapReportsReadAndParseErrors(t *testing.T) {
	t.Parallel()

	_, err := configdata.LoadYAMLMap(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("missing file did not report an error")
	}

	path := filepath.Join(t.TempDir(), "bad.yaml")
	writeConfigData(t, path, "value: [\n")

	_, err = configdata.LoadYAMLMap(path)
	if err == nil {
		t.Fatal("invalid yaml did not report an error")
	}
}

func TestDeepMergeRecursesWithoutMutatingInputs(t *testing.T) {
	t.Parallel()

	base := configdata.Map{
		"tool": map[string]any{
			"enabled": true,
			"args":    []any{"a"},
		},
		"keep": "base",
	}
	override := configdata.Map{
		"tool": map[string]any{
			"args": []any{"b"},
		},
		"new": "override",
	}

	merged := configdata.DeepMerge(base, override)

	if got := configdata.GetPath(merged, "tool.enabled", nil); got != true {
		t.Fatalf("merged tool.enabled = %#v", got)
	}

	if got := configdata.GetPath(merged, "tool.args", nil); !reflect.DeepEqual(
		got,
		[]any{"b"},
	) {
		t.Fatalf("merged tool.args = %#v", got)
	}

	if got := merged["keep"]; got != "base" {
		t.Fatalf("merged keep = %#v", got)
	}

	if got := merged["new"]; got != "override" {
		t.Fatalf("merged new = %#v", got)
	}

	if got := configdata.GetPath(base, "tool.args", nil); !reflect.DeepEqual(
		got,
		[]any{"a"},
	) {
		t.Fatalf("base was mutated: %#v", base)
	}
}

func TestAccessHelpersNormalizeExpectedTypes(t *testing.T) {
	t.Parallel()

	values := configdata.Map{
		"name":       " service ",
		"int":        7,
		"int64":      int64(8),
		"float":      float64(9),
		"string_int": " 10 ",
		"bad_int":    "x",
		"mapping":    map[string]any{"x": "y"},
		"list":       []any{"a", " ", 2},
	}

	if configdata.StringAt(values, "name") != "service" {
		t.Fatalf("StringAt returned %q", configdata.StringAt(values, "name"))
	}

	if configdata.StringAt(nil, "name") != "" ||
		configdata.StringAt(values, "missing") != "" {
		t.Fatal("StringAt should return empty string for missing values")
	}

	assertIntValues(t, values, map[string]int{
		"int":        7,
		"int64":      8,
		"float":      9,
		"string_int": 10,
		"bad_int":    0,
		"missing":    0,
	})

	if got := configdata.StringList(values["list"]); !reflect.DeepEqual(
		got,
		[]string{"a", "2"},
	) {
		t.Fatalf("StringList(list) = %#v", got)
	}

	if got := configdata.StringList(" value "); !reflect.DeepEqual(
		got,
		[]string{"value"},
	) {
		t.Fatalf("StringList(scalar) = %#v", got)
	}

	if configdata.StringList(nil) != nil || configdata.StringList(" ") != nil {
		t.Fatal("StringList should return nil for empty values")
	}

	if got := configdata.MapValue(values["mapping"]); got["x"] != "y" {
		t.Fatalf("MapValue = %#v", got)
	}

	if configdata.MapValue("nope") != nil {
		t.Fatal("MapValue should reject non-maps")
	}

	if got := configdata.ListValue(values["list"]); len(got) != 3 {
		t.Fatalf("ListValue = %#v", got)
	}

	if configdata.ListValue("nope") != nil {
		t.Fatal("ListValue should reject non-lists")
	}
}

func TestGetPathReturnsFallbackForMissingOrNonMapSegments(t *testing.T) {
	t.Parallel()

	config := configdata.Map{"a": map[string]any{"b": "value"}, "plain": "text"}

	if got := configdata.GetPath(config, "a.b", "fallback"); got != "value" {
		t.Fatalf("GetPath existing = %#v", got)
	}

	for _, path := range []string{"a.c", "plain.value"} {
		if got := configdata.GetPath(config, path, "fallback"); got != "fallback" {
			t.Fatalf("GetPath(%s) = %#v", path, got)
		}
	}
}

func assertIntValues(t *testing.T, values configdata.Map, cases map[string]int) {
	t.Helper()

	for key, want := range cases {
		if got := configdata.IntAt(values, key); got != want {
			t.Fatalf("IntAt(%s) = %d, want %d", key, got, want)
		}
	}
}

func writeConfigData(t *testing.T, path, content string) {
	t.Helper()

	err := os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("write config %s: %v", path, err)
	}
}
