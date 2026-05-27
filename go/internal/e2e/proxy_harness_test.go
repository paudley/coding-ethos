// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package e2e_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/e2e"
)

func TestAgentProxyHarnessRecordsProviderAndFileReadEvidence(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("agent proxy e2e harness uses a real temp repository")
	}

	sourceRoot := repoRootFromWorkingDirectory(t)
	repo := e2e.FromReference(t, sourceRoot, "policy-lint-basic")
	provider := e2e.NewProxyProviderServer(t)

	content, err := os.ReadFile(filepath.Join(repo.Root, "pkg", "clean.py"))
	if err != nil {
		t.Fatalf("read fixture file: %v", err)
	}

	response, err := provider.Send(agentproxy.ProviderRequest{
		SessionID: "proxy-session-1",
		Provider:  "codex",
		Model:     "fixture-model",
		Messages: []agentproxy.Message{{
			Role:    agentproxy.RoleUser,
			Content: string(content),
		}},
	})
	if err != nil {
		t.Fatalf("send provider request: %v", err)
	}

	store, err := codeintel.Open(
		context.Background(),
		filepath.Join(repo.Root, ".coding-ethos", "code-intel.duckdb"),
	)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	err = store.RecordProxyEvent(context.Background(), agentproxy.ProviderEvent{
		ID:            "proxy-event-1",
		SessionID:     response.SessionID,
		Kind:          agentproxy.EventFileRead,
		Provider:      response.Provider,
		Model:         response.Model,
		RecordedAtUTC: time.Now().UTC(),
		RepoRoot:      repo.Root,
		TargetPath:    "pkg/clean.py",
		InputHash:     agentproxy.HashText(string(content)),
		OutputHash:    agentproxy.HashText(response.Messages[0].Content),
		TokenUsage:    response.Usage,
		Transforms: []agentproxy.TransformRecord{{
			Name:         "semantic-pagination",
			Reason:       "bounded file context",
			InputTokens:  response.Usage.InputTokens,
			OutputTokens: response.Usage.OutputTokens,
		}},
		Metadata: map[string]string{"fixture_provider": provider.URL()},
	})
	if err != nil {
		t.Fatalf("record proxy event: %v", err)
	}

	events, err := store.ProxyEvents(
		context.Background(),
		codeintel.ProxyEventQuery{SessionID: "proxy-session-1"},
	)
	if err != nil {
		t.Fatalf("query proxy events: %v", err)
	}

	if len(events) != 1 ||
		events[0].TargetPath != "pkg/clean.py" ||
		events[0].InputHash == "" ||
		len(events[0].Transforms) != 1 {
		t.Fatalf("events = %#v", events)
	}
}
