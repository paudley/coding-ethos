// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package webguidance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	err   error
	calls [][]string
}

func (runner *fakeRunner) Run(
	_ context.Context,
	_ string,
	args []string,
) (CommandOutput, error) {
	runner.calls = append(runner.calls, append([]string(nil), args...))
	if runner.err != nil {
		return CommandOutput{Stderr: "offline"}, runner.err
	}

	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, " view "):
		return CommandOutput{Stdout: upstreamMetadataJSON}, nil
	case strings.Contains(joined, " list"):
		return CommandOutput{Stdout: upstreamListJSON}, nil
	case strings.Contains(joined, " search "):
		return CommandOutput{Stdout: upstreamSearchJSON}, nil
	case strings.Contains(joined, " retrieve "):
		return CommandOutput{Stdout: upstreamRetrieveMarkdown}, nil
	default:
		return CommandOutput{Stderr: "unexpected command"}, errors.New("unexpected command")
	}
}

const upstreamMetadataJSON = `{
  "name": "modern-web-guidance",
  "version": "0.0.174",
  "dist-tags": {"latest": "0.0.174"},
  "bin": {"modern-web-guidance": "skills/modern-web-guidance/modern-web.mjs"},
  "repository": {"url": "git+https://github.com/GoogleChrome/modern-web-guidance-src.git"}
}`

const upstreamListJSON = `[
  {"id":"html","category":"html","description":"Modern HTML guidance."}
]`

const upstreamSearchJSON = `[
  {"id":"navigation-drawer","category":"user-experience","description":"Create a navigation drawer.","featuresUsed":["Popover"],"tokenCount":4317,"similarity":0.637},
  {"id":"position-aware-tooltips","category":"user-experience","description":"Build tooltips.","featuresUsed":["Anchor positioning"],"tokenCount":1648,"similarity":0.4719}
]`

const upstreamRetrieveMarkdown = `--- Guide for navigation-drawer ---
## Overview

Use native popover and scroll snap.

### Implementation

Wire the trigger and drawer state.`

func TestSearchFetchesCachesAndAddsProvenance(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	runner := &fakeRunner{}

	response, err := Adapter{
		Root:   root,
		Runner: runner,
		Now:    func() time.Time { return now },
	}.Search(context.Background(), SearchInput{
		Query:         "navigation drawer",
		Limit:         1,
		BrowserPolicy: "baseline widely available",
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if response.Kind != "modern_web_guidance" ||
		response.Operation != "search" ||
		!response.Advisory ||
		response.Provenance.PackageName != PackageName ||
		response.Provenance.ResolvedVersion != "0.0.174" ||
		response.Provenance.SourceURL != "https://github.com/GoogleChrome/modern-web-guidance-src" ||
		!strings.HasPrefix(response.Provenance.ContentHash, "sha256:") {
		t.Fatalf("response missing provenance: %#v", response)
	}
	if response.Cache.Status != "refreshed" || response.Cache.Hit {
		t.Fatalf("cache status = %#v", response.Cache)
	}
	if len(response.Results) != 1 ||
		response.Results[0].ID != "navigation-drawer" ||
		response.Results[0].TokenCount != 4317 ||
		!slices.Contains(response.Results[0].FeaturesUsed, "Popover") {
		t.Fatalf("unexpected search results: %#v", response.Results)
	}
	if !slices.Contains(response.Provenance.GuideIDs, "navigation-drawer") {
		t.Fatalf("guide IDs missing search result: %#v", response.Provenance.GuideIDs)
	}
	if _, err := os.Stat(response.Cache.Path); err != nil {
		t.Fatalf("cache response was not written: %v", err)
	}
	if !slices.Contains(runner.calls[0], "--cache") {
		t.Fatalf("npm command did not carry repo-local cache: %#v", runner.calls)
	}
}

func TestSearchUsesFreshCacheWithoutNetwork(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)

	_, err := Adapter{
		Root:   root,
		Runner: &fakeRunner{},
		Now:    func() time.Time { return now },
	}.Search(context.Background(), SearchInput{Query: "navigation drawer"})
	if err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	runner := &fakeRunner{err: errors.New("network disabled")}
	response, err := Adapter{
		Root:   root,
		Runner: runner,
		Now:    func() time.Time { return now.Add(time.Hour) },
	}.Search(context.Background(), SearchInput{Query: "navigation drawer"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if response.Cache.Status != "hit" || !response.Cache.Hit || response.Cache.Stale {
		t.Fatalf("fresh cache status = %#v", response.Cache)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("fresh cache should not call network runner: %#v", runner.calls)
	}
}

func TestStaleCacheReturnsWhenNetworkRefreshDisabled(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)

	_, err := Adapter{
		Root:   root,
		Runner: &fakeRunner{},
		Now:    func() time.Time { return now },
	}.Search(context.Background(), SearchInput{Query: "navigation drawer"})
	if err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	writeFile(t, filepath.Join(root, "repo_config.toml"), `
[web_guidance.modern_web]
cache_ttl = "1h"
allow_network_refresh = false
`)

	response, err := Adapter{
		Root:   root,
		Runner: &fakeRunner{err: errors.New("network disabled")},
		Now:    func() time.Time { return now.Add(2 * time.Hour) },
	}.Search(context.Background(), SearchInput{Query: "navigation drawer"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if response.Cache.Status != "stale" || !response.Cache.Hit || !response.Cache.Stale {
		t.Fatalf("stale cache status = %#v", response.Cache)
	}
	if len(response.Warnings) == 0 {
		t.Fatalf("stale cache response missing warning: %#v", response)
	}
}

func TestNoCacheAndNoNetworkReturnsActionableError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "repo_config.toml"), `
[web_guidance.modern_web]
allow_network_refresh = false
`)

	_, err := Adapter{Root: root}.List(context.Background(), ListInput{})
	if err == nil || !errors.Is(err, errNoCachedModernWebGuidance) {
		t.Fatalf("List error = %v, want no cache", err)
	}
}

func TestRetrieveParsesGuideContentAndSections(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)

	response, err := Adapter{
		Root:   root,
		Runner: &fakeRunner{},
		Now:    func() time.Time { return now },
	}.Retrieve(context.Background(), RetrieveInput{IDs: []string{"navigation-drawer"}})
	if err != nil {
		t.Fatalf("Retrieve returned error: %v", err)
	}

	if len(response.Guides) != 1 ||
		response.Guides[0].ID != "navigation-drawer" ||
		!strings.HasPrefix(response.Guides[0].ContentHash, "sha256:") ||
		len(response.Guides[0].Sections) != 2 ||
		response.Guides[0].Sections[0].Title != "Overview" {
		t.Fatalf("unexpected retrieved guide: %#v", response.Guides)
	}
}

func TestParseSectionsDropsPreambleBeforeFirstHeading(t *testing.T) {
	t.Parallel()

	sections := parseSections("introductory preamble\n\n## Overview\n\nUse popover.")
	if len(sections) != 1 ||
		sections[0].Title != "Overview" ||
		sections[0].Content != "Use popover." {
		t.Fatalf("sections = %#v, want preamble discarded", sections)
	}
}

func TestFormatTOONIncludesCacheProvenanceAndGuides(t *testing.T) {
	t.Parallel()

	output := FormatTOON(Response{
		Kind:      "modern_web_guidance",
		Operation: "retrieve",
		Advisory:  true,
		Cache: CacheStatus{
			Status: "hit",
			Path:   "/repo/.coding-ethos/cache/modern-web-guidance/responses/a.json",
			Hit:    true,
		},
		Provenance: Provenance{
			PackageName:     PackageName,
			ResolvedVersion: "0.0.174",
			DistTag:         DistTagLatest,
			FetchTimeUTC:    "2026-06-23T12:00:00Z",
			ContentHash:     "sha256:abc",
		},
		Guides: []GuideContent{{
			ID:          "navigation-drawer",
			Content:     "## Overview\nUse popover.",
			ContentHash: "sha256:def",
			Sections:    []Section{{Title: "Overview", Level: 2}},
		}},
	})

	for _, want := range []string{
		"kind: modern_web_guidance",
		"cache_status: hit",
		"package: modern-web-guidance",
		"guides[1]{id,content_hash,sections,content}:",
		"## Overview\\nUse popover.",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("TOON output missing %q:\n%s", want, output)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	err := os.WriteFile(
		filepath.Clean(path),
		[]byte(strings.TrimSpace(content)+"\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
