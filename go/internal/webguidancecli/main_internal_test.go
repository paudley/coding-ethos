// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package webguidancecli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"blackcat.ca/coding-ethos/go/internal/webguidance"
)

type cliFakeRunner struct{}

func (runner cliFakeRunner) Run(
	_ context.Context,
	_ string,
	args []string,
) (webguidance.CommandOutput, error) {
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, " view "):
		return webguidance.CommandOutput{Stdout: `{
  "name": "modern-web-guidance",
  "version": "0.0.174",
  "dist-tags": {"latest": "0.0.174"},
  "bin": {"modern-web-guidance": "skills/modern-web-guidance/modern-web.mjs"},
  "repository": {"url": "git+https://github.com/GoogleChrome/modern-web-guidance-src.git"}
}`}, nil
	case strings.Contains(joined, " search "):
		return webguidance.CommandOutput{Stdout: `[
  {"id":"navigation-drawer","category":"user-experience","description":"Create a navigation drawer.","featuresUsed":["Popover"],"tokenCount":4317,"similarity":0.637}
]`}, nil
	case strings.Contains(joined, " retrieve "):
		return webguidance.CommandOutput{Stdout: `--- Guide for navigation-drawer ---
## Overview

Use native popover.`}, nil
	default:
		return webguidance.CommandOutput{
				Stderr: "unexpected command",
			}, errors.New(
				"unexpected command",
			)
	}
}

func TestSearchWritesDefaultTOONFromCache(t *testing.T) {
	root := seedSearchCache(t)

	output := captureStdout(t, func() {
		err := run(context.Background(), []string{
			"search",
			"--root",
			root,
			"navigation drawer",
		})
		if err != nil {
			t.Fatalf("run search: %v", err)
		}
	})

	for _, want := range []string{
		"kind: modern_web_guidance",
		"operation: search",
		"cache_status: hit",
		"results[1]{id,category,similarity,tokens,description}:",
		"navigation-drawer",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("search TOON missing %q:\n%s", want, output)
		}
	}
	assertSingleTrailingNewline(t, output)
}

func TestSearchAcceptsFlagsAfterQuery(t *testing.T) {
	root := seedSearchCache(t)

	output := captureStdout(t, func() {
		err := run(context.Background(), []string{
			"search",
			"navigation drawer",
			"--root",
			root,
			"--limit",
			"1",
		})
		if err != nil {
			t.Fatalf("run search: %v", err)
		}
	})

	if !strings.Contains(output, "query: navigation drawer") ||
		strings.Contains(output, "query: navigation drawer --root") {
		t.Fatalf("search did not parse trailing flags correctly:\n%s", output)
	}
}

func TestRetrieveWritesJSONFromCache(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	_, err := webguidance.Adapter{
		Root:   root,
		Runner: cliFakeRunner{},
		Now:    func() time.Time { return now },
	}.Retrieve(context.Background(), webguidance.RetrieveInput{
		IDs: []string{"navigation-drawer"},
	})
	if err != nil {
		t.Fatalf("seed retrieve cache: %v", err)
	}

	output := captureStdout(t, func() {
		err := run(context.Background(), []string{
			"retrieve",
			"--root",
			root,
			"--format",
			"json",
			"navigation-drawer",
		})
		if err != nil {
			t.Fatalf("run retrieve: %v", err)
		}
	})

	if !strings.Contains(output, `"kind": "modern_web_guidance"`) ||
		!strings.Contains(output, `"content": "## Overview\n\nUse native popover."`) {
		t.Fatalf("retrieve JSON missing guide content:\n%s", output)
	}
	assertSingleTrailingNewline(t, output)
}

func TestRetrieveRequiresID(t *testing.T) {
	t.Parallel()

	err := run(context.Background(), []string{"retrieve"})
	if err == nil || !strings.Contains(err.Error(), "retrieve id is required") {
		t.Fatalf("retrieve error = %v, want id required", err)
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	t.Parallel()

	err := run(context.Background(), []string{"unknown"})
	if err == nil || !strings.Contains(err.Error(), "unknown web-guidance command") {
		t.Fatalf("unknown command error = %v", err)
	}
}

func seedSearchCache(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	_, err := webguidance.Adapter{
		Root:   root,
		Runner: cliFakeRunner{},
		Now:    func() time.Time { return now },
	}.Search(context.Background(), webguidance.SearchInput{Query: "navigation drawer"})
	if err != nil {
		t.Fatalf("seed search cache: %v", err)
	}

	return root
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	original := os.Stdout
	readFile, writeFile, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = writeFile

	var buffer bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buffer, readFile)
		close(done)
	}()

	fn()

	_ = writeFile.Close()
	os.Stdout = original
	<-done
	_ = readFile.Close()

	return buffer.String()
}

func assertSingleTrailingNewline(t *testing.T, output string) {
	t.Helper()

	if !strings.HasSuffix(output, "\n") {
		t.Fatalf("output missing trailing newline: %q", output)
	}
	if strings.HasSuffix(output, "\n\n") {
		t.Fatalf("output has extra trailing newline: %q", output)
	}
}
