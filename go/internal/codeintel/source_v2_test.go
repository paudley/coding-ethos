// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/astfacts"
)

type sourceV2FakeExtractor struct {
	calls       int
	validations int
}

func (extractor *sourceV2FakeExtractor) Validate(_ context.Context) error {
	extractor.validations++

	return nil
}

func (extractor *sourceV2FakeExtractor) Extract(
	_ context.Context,
	files []ExternalExtractorRequestFile,
) (ExternalExtractorResponse, error) {
	extractor.calls++
	results := make([]ExternalExtractorResult, 0, len(files))
	for _, file := range files {
		language := astfacts.LanguageTurtle
		parser := externalExtractorParserRDF
		switch filepath.Ext(file.Path) {
		case ".trig":
			language = astfacts.LanguageTriG
		case ".nt":
			language = astfacts.LanguageNTriples
		case ".nq":
			language = astfacts.LanguageNQuads
		case ".rq", ".sparql":
			language = astfacts.LanguageSPARQL
			parser = externalExtractorParserSPARQL
		}

		results = append(results, ExternalExtractorResult{
			Path:          file.Path,
			ContentSHA256: file.ContentSHA256,
			Language:      language,
			Status:        "ok",
			DocumentKind:  language,
			Facts: []ExternalExtractorFact{{
				ID:   "fact:sha256:fixture",
				Kind: "triple",
				Provenance: ExternalExtractorProvenance{
					Class:          "EXTRACTED",
					Parser:         parser,
					ParserRevision: astfacts.PurrdfExtractorRevision,
					SpanFidelity:   externalExtractorSpanFidelitySubjectStart,
					SourcePath:     file.Path,
				},
			}},
		})
	}

	return ExternalExtractorResponse{
		Protocol:  astfacts.PurrdfExtractorProtocol,
		RequestID: externalExtractorRequestID(files),
		Extractor: ExternalExtractorIdentity{
			Name:           astfacts.PurrdfExtractorName,
			Version:        "1",
			PurrdfRevision: astfacts.PurrdfExtractorRevision,
		},
		Results: results,
	}, nil
}

func TestSyncSourceIndexBuildsReusableBaseLaneDeltaAndExactQuery(t *testing.T) {
	t.Parallel()

	root := initSourceV2TestRepository(t)
	writeSourceV2TestFile(t, root, "app.go", "package app\n\nfunc Current() {}\n")
	writeSourceV2TestFile(t, root, "old.rs", "pub fn old() {}\n")
	runSourceV2TestGit(t, root, "add", "app.go", "old.rs")
	runSourceV2TestGit(t, root, "commit", "-m", "base")

	writeSourceV2TestFile(
		t,
		root,
		"app.go",
		"package app\n\nfunc Current() { helper() }\nfunc helper() {}\n",
	)
	if err := os.Remove(filepath.Join(root, "old.rs")); err != nil {
		t.Fatalf("remove tracked Rust source: %v", err)
	}
	writeSourceV2TestFile(
		t,
		root,
		"ontology.ttl",
		"@prefix ex: <https://example.test/> .\nex:s ex:p ex:o .\n",
	)

	legacyPath := filepath.Join(root, ".coding-ethos", "code-intel.duckdb")
	writeSourceV2TestFile(t, root, ".coding-ethos/code-intel.duckdb", "legacy-sentinel")

	extractor := &sourceV2FakeExtractor{}
	first, err := SyncSourceIndex(context.Background(), root, extractor)
	if err != nil {
		t.Fatalf("sync source index: %v", err)
	}
	if first.Contract != SourceV2Contract ||
		first.SourceReadiness.Status != SourceStatusExact ||
		first.SourceReadiness.Identity.GenerationID == "" {
		t.Fatalf("sync receipt = %#v", first)
	}
	if first.Storage.SharedRoot != filepath.Join(
		root,
		".git",
		"coding-ethos",
		"code-intel",
		"v2",
	) {
		t.Fatalf("shared root = %q", first.Storage.SharedRoot)
	}
	if first.Storage.LaneRoot == first.Storage.SharedRoot ||
		!strings.HasPrefix(first.Storage.LaneRoot, filepath.Join(root, ".coding-ethos")) {
		t.Fatalf("lane root = %q", first.Storage.LaneRoot)
	}
	if got := sourceV2CoverageForLanguage(
		first.SourceReadiness.Coverage,
		astfacts.LanguageTurtle,
	); got.Eligible != 1 ||
		got.Indexed != 1 {
		t.Fatalf("Turtle coverage = %#v", got)
	}
	if extractor.calls != 1 || extractor.validations != 1 {
		t.Fatalf(
			"external extractor calls = %d validations = %d",
			extractor.calls,
			extractor.validations,
		)
	}

	delta, err := loadSourceV2DeltaManifest(
		mustSourceV2Layout(t, root),
		first.Storage.DeltaManifestID,
	)
	if err != nil {
		t.Fatalf("load lane delta: %v", err)
	}
	if !slices.Contains(delta.Tombstones, "old.rs") {
		t.Fatalf("delta tombstones = %#v", delta.Tombstones)
	}

	query, err := QuerySourceIndex(context.Background(), root, SourceIndexQuery{
		Path: "ontology.ttl",
	})
	if err != nil {
		t.Fatalf("query source index: %v", err)
	}
	if len(query.Records) != 1 || len(query.Records[0].Facts) != 1 ||
		query.SourceReadiness.Identity.GenerationID != first.SourceReadiness.Identity.GenerationID {
		t.Fatalf("query result = %#v", query)
	}

	second, err := SyncSourceIndex(context.Background(), root, extractor)
	if err != nil {
		t.Fatalf("repeat source sync: %v", err)
	}
	if second.Storage.BaseManifestID != first.Storage.BaseManifestID ||
		second.Storage.DeltaManifestID != first.Storage.DeltaManifestID ||
		second.SourceReadiness.Identity.GenerationID != first.SourceReadiness.Identity.GenerationID {
		t.Fatalf(
			"repeat generation changed: first=%#v second=%#v",
			first.Storage,
			second.Storage,
		)
	}
	if second.Sync.FragmentsReused == 0 || extractor.calls != 1 ||
		extractor.validations != 2 {
		t.Fatalf(
			"repeat sync did not reuse fragments with a fresh identity check: %#v calls=%d validations=%d",
			second.Sync,
			extractor.calls,
			extractor.validations,
		)
	}
	if got := sourceV2CoverageForLanguage(
		second.SourceReadiness.Coverage,
		astfacts.LanguageTurtle,
	); got.CacheHits != 1 {
		t.Fatalf("repeat Turtle coverage = %#v", got)
	}

	legacy, err := os.ReadFile(legacyPath)
	if err != nil || string(legacy) != "legacy-sentinel" {
		t.Fatalf("legacy v1 store changed: %q err=%v", legacy, err)
	}

	writeSourceV2TestFile(t, root, "app.go", "package app\n\nfunc Changed() {}\n")
	status, err := SourceIndexStatus(context.Background(), root)
	if err != nil {
		t.Fatalf("read stale source status: %v", err)
	}
	if status.SourceReadiness.Status != SourceStatusStale {
		t.Fatalf("source status = %#v", status.SourceReadiness)
	}
	if _, err = QuerySourceIndex(
		context.Background(),
		root,
		SourceIndexQuery{},
	); !errors.Is(
		err,
		ErrSourceIndexStale,
	) {
		t.Fatalf("stale query error = %v", err)
	}
}

func TestSourceIndexStatusRejectsMutatedImmutableManifest(t *testing.T) {
	t.Parallel()

	root := initSourceV2TestRepository(t)
	writeSourceV2TestFile(t, root, "app.go", "package app\n")
	runSourceV2TestGit(t, root, "add", "app.go")
	runSourceV2TestGit(t, root, "commit", "-m", "base")

	receipt, err := SyncSourceIndex(context.Background(), root, nil)
	if err != nil {
		t.Fatalf("sync source index: %v", err)
	}
	layout := mustSourceV2Layout(t, root)
	delta, err := loadSourceV2DeltaManifest(layout, receipt.Storage.DeltaManifestID)
	if err != nil {
		t.Fatalf("load delta manifest: %v", err)
	}
	delta.Tombstones = append(delta.Tombstones, "app.go")
	payload, err := json.MarshalIndent(delta, "", "  ")
	if err != nil {
		t.Fatalf("marshal mutated delta: %v", err)
	}
	payload = append(payload, '\n')
	if err = os.WriteFile(
		layout.deltaManifestPath(delta.ManifestID),
		payload,
		0o600,
	); err != nil {
		t.Fatalf("mutate delta manifest: %v", err)
	}

	status, err := SourceIndexStatus(context.Background(), root)
	if err != nil {
		t.Fatalf("read status after manifest mutation: %v", err)
	}
	if status.SourceReadiness.Status != SourceStatusFailed ||
		len(status.SourceReadiness.Reasons) != 1 ||
		!strings.Contains(status.SourceReadiness.Reasons[0], "content ID mismatch") {
		t.Fatalf("mutated manifest status = %#v", status.SourceReadiness)
	}
}

func TestSyncSourceIndexReusesSharedBaseAcrossWorktrees(t *testing.T) {
	t.Parallel()

	root := initSourceV2TestRepository(t)
	writeSourceV2TestFile(t, root, "model.ttl", "<s> <p> <o> .\n")
	runSourceV2TestGit(t, root, "add", "model.ttl")
	runSourceV2TestGit(t, root, "commit", "-m", "base")

	extractor := &sourceV2FakeExtractor{}
	first, err := SyncSourceIndex(context.Background(), root, extractor)
	if err != nil {
		t.Fatalf("sync source index in primary worktree: %v", err)
	}

	lane := filepath.Join(t.TempDir(), "lane")
	runSourceV2TestGit(t, root, "worktree", "add", "--quiet", "--detach", lane, "HEAD")
	second, err := SyncSourceIndex(context.Background(), lane, extractor)
	if err != nil {
		t.Fatalf("sync source index in second worktree: %v", err)
	}
	if second.Storage.BaseManifestID != first.Storage.BaseManifestID ||
		extractor.calls != 1 || extractor.validations != 2 ||
		second.Sync.FragmentsReused == 0 {
		t.Fatalf(
			"shared base was not reused: first=%#v second=%#v calls=%d validations=%d",
			first.Storage,
			second.Storage,
			extractor.calls,
			extractor.validations,
		)
	}
}

func TestCommandBatchExtractorUsesFrozenPurRDFEnvelope(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	script := filepath.Join(directory, "coding-ethos-purrdf-extractor")
	writeSourceV2Executable(t, script, `#!/usr/bin/env python3
import json, sys
request = json.load(sys.stdin)
files = request["files"]
response = {
  "protocol": "coding-ethos.external-extractor.v1",
  "request_id": request["request_id"],
  "extractor": {
    "name": "purrdf",
    "version": "1",
    "purrdf_revision": "3aa4ba514ee9cdbfb6966bd0f1fedb87e0d8a0b6"
  },
  "results": [{
    "path": item["path"],
    "content_sha256": item["content_sha256"],
    "language": "sparql",
    "status": "ok",
    "document_kind": "query",
    "facts": []
  } for item in files]
}
json.dump(response, sys.stdout)
`)

	extractor, err := NewCommandBatchExtractor(script)
	if err != nil {
		t.Fatalf("construct command extractor: %v", err)
	}
	files := []ExternalExtractorRequestFile{{
		Path:          "query.rq",
		ContentSHA256: astfacts.ContentHash([]byte("SELECT * WHERE { ?s ?p ?o }")),
		Content:       "SELECT * WHERE { ?s ?p ?o }",
	}}
	response, err := extractor.Extract(context.Background(), files)
	if err != nil {
		t.Fatalf("extract frozen envelope: %v", err)
	}
	if response.Protocol != astfacts.PurrdfExtractorProtocol ||
		response.RequestID != externalExtractorRequestID(files) ||
		len(
			response.Results,
		) != 1 || response.Results[0].Language != astfacts.LanguageSPARQL {
		t.Fatalf("extractor response = %#v", response)
	}
}

func TestCommandBatchExtractorAgainstPurRDFHelper(t *testing.T) {
	executable := strings.TrimSpace(os.Getenv("CODING_ETHOS_PURRDF_EXTRACTOR"))
	if executable == "" {
		t.Skip("set CODING_ETHOS_PURRDF_EXTRACTOR for the cross-language integration test")
	}

	extractor, err := NewCommandBatchExtractor(executable)
	if err != nil {
		t.Fatalf("construct PurRDF helper adapter: %v", err)
	}
	sources := map[string]string{
		"model.ttl":  "<https://example.test/s> <https://example.test/p> <https://example.test/o> .\n",
		"model.trig": "@prefix ex: <https://example.test/> .\nex:g { ex:s ex:p ex:o . }\n",
		"model.nt":   "<https://example.test/s> <https://example.test/p> <https://example.test/o> .\n",
		"model.nq":   "<https://example.test/s> <https://example.test/p> <https://example.test/o> <https://example.test/g> .\n",
		"query.rq":   "SELECT * WHERE { ?s ?p ?o }\n",
	}
	files := make([]ExternalExtractorRequestFile, 0, len(sources))
	for path, content := range sources {
		files = append(files, ExternalExtractorRequestFile{
			Path:          path,
			ContentSHA256: astfacts.ContentHash([]byte(content)),
			Content:       content,
		})
	}

	response, err := extractor.Extract(context.Background(), files)
	if err != nil {
		t.Fatalf("extract through real PurRDF helper: %v", err)
	}
	expectedLanguages := map[string]string{
		"model.ttl":  astfacts.LanguageTurtle,
		"model.trig": astfacts.LanguageTriG,
		"model.nt":   astfacts.LanguageNTriples,
		"model.nq":   astfacts.LanguageNQuads,
		"query.rq":   astfacts.LanguageSPARQL,
	}
	if len(response.Results) != len(expectedLanguages) {
		t.Fatalf(
			"real helper results = %d, want %d",
			len(response.Results),
			len(expectedLanguages),
		)
	}
	for _, result := range response.Results {
		if result.Language != expectedLanguages[result.Path] || result.Status != "ok" ||
			len(result.Facts) == 0 {
			t.Errorf("real helper result = %#v", result)
		}
	}

	root := initSourceV2TestRepository(t)
	for path, content := range sources {
		writeSourceV2TestFile(t, root, path, content)
	}
	runSourceV2TestGit(t, root, "add", ".")
	runSourceV2TestGit(t, root, "commit", "-m", "PurRDF fixtures")
	receipt, err := SyncSourceIndex(context.Background(), root, extractor)
	if err != nil {
		t.Fatalf("sync source generation through real PurRDF helper: %v", err)
	}
	if receipt.SourceReadiness.Status != SourceStatusExact {
		t.Fatalf("real PurRDF source readiness = %#v", receipt.SourceReadiness)
	}
	for _, language := range expectedLanguages {
		coverage := sourceV2CoverageForLanguage(receipt.SourceReadiness.Coverage, language)
		if coverage.Eligible != 1 || coverage.Indexed != 1 {
			t.Errorf("real PurRDF %s coverage = %#v", language, coverage)
		}
	}
}

func TestSyncSourceIndexFailsClosedWithoutPurRDFExtractor(t *testing.T) {
	t.Parallel()

	root := initSourceV2TestRepository(t)
	writeSourceV2TestFile(t, root, "model.ttl", "<s> <p> <o> .\n")
	runSourceV2TestGit(t, root, "add", "model.ttl")
	runSourceV2TestGit(t, root, "commit", "-m", "rdf")

	_, err := SyncSourceIndex(context.Background(), root, nil)
	if !errors.Is(err, ErrExternalExtractorRequired) {
		t.Fatalf("sync without PurRDF extractor error = %v", err)
	}
	if _, statErr := os.Stat(
		mustSourceV2Layout(t, root).statusPath(),
	); !errors.Is(
		statErr,
		os.ErrNotExist,
	) {
		t.Fatalf("failed sync published a current receipt: %v", statErr)
	}
}

func TestSyncSourceIndexDoesNotRequirePurRDFForBuiltInLanguages(t *testing.T) {
	t.Parallel()

	root := initSourceV2TestRepository(t)
	writeSourceV2TestFile(t, root, "main.rs", "fn main() {}\n")
	runSourceV2TestGit(t, root, "add", "main.rs")
	runSourceV2TestGit(t, root, "commit", "-m", "rust")

	receipt, err := SyncSourceIndex(context.Background(), root, nil)
	if err != nil {
		t.Fatalf("sync built-in language without PurRDF extractor: %v", err)
	}
	if receipt.SourceReadiness.Status != SourceStatusExact {
		t.Fatalf("built-in-only source readiness = %#v", receipt.SourceReadiness)
	}
}

func TestSyncSourceIndexRejectsWrongPurRDFHelperIdentity(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	script := filepath.Join(directory, "coding-ethos-purrdf-extractor")
	writeSourceV2Executable(t, script, `#!/usr/bin/env python3
import json, sys
request = json.load(sys.stdin)
json.dump({
  "protocol": "coding-ethos.external-extractor.v1",
  "request_id": request["request_id"],
  "extractor": {
    "name": "purrdf",
    "version": "1",
    "purrdf_revision": "wrong"
  },
  "results": []
}, sys.stdout)
`)
	extractor, err := NewCommandBatchExtractor(script)
	if err != nil {
		t.Fatalf("construct wrong-identity extractor: %v", err)
	}

	root := initSourceV2TestRepository(t)
	writeSourceV2TestFile(t, root, "model.ttl", "<s> <p> <o> .\n")
	runSourceV2TestGit(t, root, "add", "model.ttl")
	runSourceV2TestGit(t, root, "commit", "-m", "rdf")

	_, err = SyncSourceIndex(context.Background(), root, extractor)
	if !errors.Is(err, ErrExternalExtractorRequired) ||
		!strings.Contains(err.Error(), "unexpected PurRDF extractor identity") {
		t.Fatalf("wrong-identity sync error = %v", err)
	}
	if _, statErr := os.Stat(
		mustSourceV2Layout(t, root).statusPath(),
	); !errors.Is(
		statErr,
		os.ErrNotExist,
	) {
		t.Fatalf("wrong-identity sync published a current receipt: %v", statErr)
	}
}

func TestRepositoryIDIsStableAcrossEquivalentClones(t *testing.T) {
	t.Parallel()

	seed := initSourceV2TestRepository(t)
	writeSourceV2TestFile(t, seed, "app.go", "package app\n")
	runSourceV2TestGit(t, seed, "add", "app.go")
	runSourceV2TestGit(t, seed, "commit", "-m", "root")

	remoteParent := t.TempDir()
	remote := filepath.Join(remoteParent, "canonical.git")
	runSourceV2TestGit(t, remoteParent, "init", "--quiet", "--bare", remote)
	runSourceV2TestGit(t, seed, "remote", "add", "origin", remote)
	runSourceV2TestGit(t, seed, "push", "--quiet", "origin", "HEAD:refs/heads/main")

	cloneParent := t.TempDir()
	firstRoot := filepath.Join(cloneParent, "first")
	secondRoot := filepath.Join(cloneParent, "second")
	runSourceV2TestGit(
		t,
		cloneParent,
		"clone",
		"--quiet",
		"--branch",
		"main",
		remote,
		firstRoot,
	)
	runSourceV2TestGit(
		t,
		cloneParent,
		"clone",
		"--quiet",
		"--branch",
		"main",
		remote,
		secondRoot,
	)

	first := mustSourceV2Layout(t, firstRoot)
	second := mustSourceV2Layout(t, secondRoot)
	if first.repositoryID != second.repositoryID {
		t.Fatalf(
			"clone repository IDs differ: %q != %q",
			first.repositoryID,
			second.repositoryID,
		)
	}
	if first.worktreeID == second.worktreeID {
		t.Fatalf("clone worktree IDs unexpectedly match: %q", first.worktreeID)
	}
}

func TestRepositoryIDUsesDocumentedLocalFallback(t *testing.T) {
	t.Parallel()

	root := initSourceV2TestRepository(t)
	writeSourceV2TestFile(t, root, "app.go", "package app\n")
	runSourceV2TestGit(t, root, "add", "app.go")
	runSourceV2TestGit(t, root, "commit", "-m", "root")

	layout := mustSourceV2Layout(t, root)
	rootCommit, err := sourceV2GitOutput(
		context.Background(),
		root,
		"rev-list",
		"--max-parents=0",
		"HEAD",
	)
	if err != nil {
		t.Fatalf("read root commit: %v", err)
	}
	expected := RepositoryID(sourceV2Digest(
		"repository",
		"local-common-dir:"+filepath.ToSlash(layout.commonGitDir)+"\x00"+rootCommit,
	))
	if layout.repositoryID != expected {
		t.Fatalf("local repository ID = %q, want %q", layout.repositoryID, expected)
	}
}

func TestNormalizeSourceV2Origin(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tests := map[string]string{
		"git@EXAMPLE.com:org/repo.git":                     "ssh://example.com/org/repo",
		"ssh://other:secret@EXAMPLE.com:22/org/repo.git/":  "ssh://example.com/org/repo",
		"https://user:secret@EXAMPLE.com:443/org/repo.git": "https://example.com/org/repo",
	}
	for input, expected := range tests {
		actual, err := normalizeSourceV2Origin(root, input)
		if err != nil {
			t.Fatalf("normalize %q: %v", input, err)
		}
		if actual != expected {
			t.Errorf("normalize %q = %q, want %q", input, actual, expected)
		}
	}
}

func sourceV2CoverageForLanguage(
	coverage []LanguageCoverage,
	language string,
) LanguageCoverage {
	for _, item := range coverage {
		if item.Language == language {
			return item
		}
	}

	return LanguageCoverage{}
}

func initSourceV2TestRepository(t testing.TB) string {
	t.Helper()

	root := t.TempDir()
	runSourceV2TestGit(t, root, "init", "--quiet")
	runSourceV2TestGit(t, root, "config", "user.email", "test@example.test")
	runSourceV2TestGit(t, root, "config", "user.name", "Code Intel Test")

	return root
}

func mustSourceV2Layout(t *testing.T, root string) sourceV2Layout {
	t.Helper()

	layout, err := resolveSourceV2Layout(context.Background(), root)
	if err != nil {
		t.Fatalf("resolve v2 layout: %v", err)
	}

	return layout
}

func writeSourceV2TestFile(t testing.TB, root, relative, contents string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create test source directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write test source %s: %v", relative, err)
	}
}

func writeSourceV2Executable(t testing.TB, path, contents string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		t.Fatalf("write test executable: %v", err)
	}
}

func runSourceV2TestGit(t testing.TB, root string, arguments ...string) {
	t.Helper()

	command := exec.Command("git", arguments...)
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}

func BenchmarkSourceV2WarmSync(b *testing.B) {
	root := initSourceV2TestRepository(b)
	writeSourceV2TestFile(b, root, "app.go", "package app\n\nfunc Current() {}\n")
	runSourceV2TestGit(b, root, "add", "app.go")
	runSourceV2TestGit(b, root, "commit", "-m", "base")

	if _, err := SyncSourceIndex(context.Background(), root, nil); err != nil {
		b.Fatalf("prime source index: %v", err)
	}

	b.ResetTimer()
	for range b.N {
		if _, err := SyncSourceIndex(context.Background(), root, nil); err != nil {
			b.Fatalf("warm source sync: %v", err)
		}
	}
}

func TestExternalExtractorResponseRejectsWrongRevision(t *testing.T) {
	t.Parallel()

	files := []ExternalExtractorRequestFile{{
		Path:          "model.ttl",
		ContentSHA256: "abc",
		Content:       "",
	}}
	request := ExternalExtractorRequest{
		Protocol:  astfacts.PurrdfExtractorProtocol,
		RequestID: externalExtractorRequestID(files),
		Files:     files,
	}
	response := ExternalExtractorResponse{
		Protocol:  request.Protocol,
		RequestID: request.RequestID,
		Extractor: ExternalExtractorIdentity{
			Name:           astfacts.PurrdfExtractorName,
			Version:        "1",
			PurrdfRevision: "wrong",
		},
	}
	if err := validateExternalExtractorResponse(request, response); err == nil {
		payload, _ := json.Marshal(response)
		t.Fatalf("wrong extractor revision accepted: %s", payload)
	}
}

func TestExternalExtractorFactsEnforceSemanticProvenanceAndSpans(t *testing.T) {
	t.Parallel()

	validTurtle := validExternalExtractorResult(astfacts.LanguageTurtle)
	if err := validateExternalExtractorFacts(validTurtle); err != nil {
		t.Fatalf("valid Turtle facts: %v", err)
	}
	validSPARQL := validExternalExtractorResult(astfacts.LanguageSPARQL)
	if err := validateExternalExtractorFacts(validSPARQL); err != nil {
		t.Fatalf("valid SPARQL facts: %v", err)
	}

	unidentified := validExternalExtractorResult(astfacts.LanguageTurtle)
	unidentified.Facts[0].ID = ""
	duplicate := validExternalExtractorResult(astfacts.LanguageTurtle)
	duplicate.Facts = append(duplicate.Facts, duplicate.Facts[0])
	wrongParser := validExternalExtractorResult(astfacts.LanguageTurtle)
	wrongParser.Facts[0].Provenance.Parser = "wrong-parser"
	wrongFidelity := validExternalExtractorResult(astfacts.LanguageTurtle)
	wrongFidelity.Facts[0].Provenance.SpanFidelity = externalExtractorSpanFidelityNone
	invalidStart := validExternalExtractorResult(astfacts.LanguageTurtle)
	invalidStart.Facts[0].Provenance.Start = &ExternalExtractorPosition{
		ByteOffset: -1,
		Line:       0,
		Column:     0,
	}
	spanFreeStart := validExternalExtractorResult(astfacts.LanguageSPARQL)
	spanFreeStart.Facts[0].Provenance.Start = &ExternalExtractorPosition{
		Line:   1,
		Column: 1,
	}

	for name, result := range map[string]ExternalExtractorResult{
		"unidentified":     unidentified,
		"duplicate":        duplicate,
		"wrong parser":     wrongParser,
		"wrong fidelity":   wrongFidelity,
		"invalid start":    invalidStart,
		"span-free start":  spanFreeStart,
		"unknown language": {Language: "python"},
	} {
		if err := validateExternalExtractorFacts(result); err == nil {
			t.Errorf("%s external facts unexpectedly passed", name)
		}
	}
}

func validExternalExtractorResult(language string) ExternalExtractorResult {
	parser := externalExtractorParserRDF
	spanFidelity := externalExtractorSpanFidelitySubjectStart
	start := &ExternalExtractorPosition{Line: 1, Column: 1}
	if language == astfacts.LanguageSPARQL {
		parser = externalExtractorParserSPARQL
		spanFidelity = externalExtractorSpanFidelityNone
		start = nil
	}

	path := "model.ttl"
	if language == astfacts.LanguageSPARQL {
		path = "query.rq"
	}

	return ExternalExtractorResult{
		Path:     path,
		Language: language,
		Facts: []ExternalExtractorFact{{
			ID:   "fact:sha256:fixture",
			Kind: "semantic",
			Provenance: ExternalExtractorProvenance{
				Start:          start,
				Class:          "EXTRACTED",
				Parser:         parser,
				ParserRevision: astfacts.PurrdfExtractorRevision,
				SpanFidelity:   spanFidelity,
				SourcePath:     path,
			},
		}},
	}
}
