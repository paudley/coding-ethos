// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/realgit"
)

const (
	sourceV2DirMode              = 0o700
	sourceV2FileMode             = 0o600
	sourceV2FragmentPrefixLength = 2
)

var (
	errEmptySourceV2Origin       = errors.New("origin URL is empty")
	errImmutableSourceV2Conflict = errors.New(
		"immutable code-intel object differs",
	)
	errInvalidSourceV2Origin     = errors.New("invalid repository origin URL")
	errInvalidSourceV2Receipt    = errors.New("invalid code-intel v2 status receipt")
	errMissingSourceV2RootCommit = errors.New(
		"resolve repository root commit: Git returned no commits",
	)
	errShallowSourceV2Repository = errors.New(
		"derive code-intel repository identity: shallow history " +
			"does not expose the root commit",
	)
)

type sourceV2Layout struct {
	repositoryRoot string
	sharedRoot     string
	laneRoot       string
	commonGitDir   string
	repositoryID   RepositoryID
	worktreeID     string
}

func resolveSourceV2Layout(ctx context.Context, root string) (sourceV2Layout, error) {
	if strings.TrimSpace(root) == "" {
		root = "."
	}

	repositoryRoot, err := filepath.Abs(root)
	if err != nil {
		return sourceV2Layout{}, fmt.Errorf("resolve code-intel repository root: %w", err)
	}

	commonGitDir, err := sourceV2GitOutput(
		ctx,
		repositoryRoot,
		"rev-parse",
		"--path-format=absolute",
		"--git-common-dir",
	)
	if err != nil {
		return sourceV2Layout{}, fmt.Errorf("resolve Git common directory: %w", err)
	}

	commonGitDir, err = filepath.Abs(strings.TrimSpace(commonGitDir))
	if err != nil {
		return sourceV2Layout{}, fmt.Errorf("normalize Git common directory: %w", err)
	}

	repositoryRoot = filepath.Clean(repositoryRoot)
	commonGitDir = filepath.Clean(commonGitDir)

	repositoryID, err := sourceV2RepositoryID(ctx, repositoryRoot, commonGitDir)
	if err != nil {
		return sourceV2Layout{}, err
	}

	worktreeID := sourceV2Digest("worktree", filepath.ToSlash(repositoryRoot))
	sharedRoot := filepath.Join(commonGitDir, "coding-ethos", "code-intel", "v2")
	laneRoot := filepath.Join(
		ResolveStateRoot(repositoryRoot),
		".coding-ethos",
		"code-intel-v2",
		"lanes",
		strings.TrimPrefix(worktreeID, "worktree:sha256:"),
	)

	return sourceV2Layout{
		repositoryRoot: repositoryRoot,
		sharedRoot:     sharedRoot,
		laneRoot:       laneRoot,
		commonGitDir:   commonGitDir,
		repositoryID:   repositoryID,
		worktreeID:     worktreeID,
	}, nil
}

func sourceV2RepositoryID(
	ctx context.Context,
	repositoryRoot string,
	commonGitDir string,
) (RepositoryID, error) {
	shallow, err := sourceV2GitOutput(
		ctx,
		repositoryRoot,
		"rev-parse",
		"--is-shallow-repository",
	)
	if err != nil {
		return "", fmt.Errorf("inspect repository history depth: %w", err)
	}

	if shallow == "true" {
		return "", errShallowSourceV2Repository
	}

	rootOutput, err := sourceV2GitOutput(
		ctx,
		repositoryRoot,
		"rev-list",
		"--max-parents=0",
		"HEAD",
	)
	if err != nil {
		return "", fmt.Errorf("resolve repository root commit: %w", err)
	}

	rootCommits := strings.Fields(rootOutput)
	if len(rootCommits) == 0 {
		return "", errMissingSourceV2RootCommit
	}

	slices.Sort(rootCommits)
	rootCommits = slices.Compact(rootCommits)

	origin, originErr := sourceV2GitOutput(
		ctx,
		repositoryRoot,
		"remote",
		"get-url",
		"origin",
	)

	var authority string

	if originErr == nil {
		authority, err = normalizeSourceV2Origin(repositoryRoot, origin)
		if err != nil {
			return "", fmt.Errorf("normalize repository origin: %w", err)
		}

		authority = "origin:" + authority
	} else {
		// A repository without an origin has no cross-clone authority. Keep it
		// supported with an explicit path-local fallback that cannot collide with
		// an origin-backed repository identity.
		canonicalCommonDir := commonGitDir

		resolved, resolveErr := filepath.EvalSymlinks(commonGitDir)
		if resolveErr == nil {
			canonicalCommonDir = resolved
		}

		authority = "local-common-dir:" + filepath.ToSlash(filepath.Clean(canonicalCommonDir))
	}

	payload := authority + "\x00" + strings.Join(rootCommits, "\n")

	return RepositoryID(sourceV2Digest("repository", payload)), nil
}

func normalizeSourceV2Origin(repositoryRoot, rawOrigin string) (string, error) {
	rawOrigin = strings.TrimSpace(rawOrigin)
	if rawOrigin == "" {
		return "", errEmptySourceV2Origin
	}

	if scpOrigin, ok := sourceV2SCPOrigin(rawOrigin); ok {
		rawOrigin = scpOrigin
	}

	parsed, err := url.Parse(rawOrigin)
	if err != nil {
		return "", fmt.Errorf("parse repository origin URL: %w", err)
	}

	if parsed.Scheme == "" {
		return normalizeSourceV2LocalOrigin(repositoryRoot, rawOrigin)
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme == "file" && parsed.Hostname() == "" {
		path, unescapeErr := url.PathUnescape(parsed.Path)
		if unescapeErr != nil {
			return "", fmt.Errorf("unescape local repository origin path: %w", unescapeErr)
		}

		return normalizeSourceV2LocalOrigin(repositoryRoot, path)
	}

	return normalizeSourceV2RemoteOrigin(parsed, rawOrigin)
}

func normalizeSourceV2RemoteOrigin(parsed *url.URL, rawOrigin string) (string, error) {
	if parsed.Opaque != "" || parsed.Hostname() == "" {
		return "", fmt.Errorf(
			"%w: origin URL %q has no host",
			errInvalidSourceV2Origin,
			rawOrigin,
		)
	}

	hostname := strings.ToLower(parsed.Hostname())

	port := parsed.Port()
	if (parsed.Scheme == "ssh" && port == "22") ||
		(parsed.Scheme == "https" && port == "443") ||
		(parsed.Scheme == "http" && port == "80") {
		port = ""
	}

	if port == "" {
		parsed.Host = hostname
		if strings.Contains(hostname, ":") {
			parsed.Host = "[" + hostname + "]"
		}
	} else {
		parsed.Host = net.JoinHostPort(hostname, port)
	}

	parsed.User = nil
	parsed.Path = normalizeSourceV2OriginPath(parsed.Path)
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""

	return parsed.String(), nil
}

func normalizeSourceV2LocalOrigin(repositoryRoot, rawPath string) (string, error) {
	path := filepath.FromSlash(strings.TrimSpace(rawPath))
	if !filepath.IsAbs(path) {
		path = filepath.Join(repositoryRoot, path)
	}

	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve local repository origin path: %w", err)
	}

	resolved, resolveErr := filepath.EvalSymlinks(absolute)
	if resolveErr == nil {
		absolute = resolved
	}

	absolute = filepath.FromSlash(normalizeSourceV2OriginPath(filepath.ToSlash(absolute)))

	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}).String(), nil
}

func normalizeSourceV2OriginPath(rawPath string) string {
	normalized := pathpkg.Clean("/" + strings.TrimLeft(rawPath, "/"))
	if normalized != "/" {
		normalized = strings.TrimSuffix(normalized, "/")
		normalized = strings.TrimSuffix(normalized, ".git")
	}

	return normalized
}

func sourceV2SCPOrigin(rawOrigin string) (string, bool) {
	if strings.Contains(rawOrigin, "://") || filepath.IsAbs(rawOrigin) {
		return "", false
	}

	colon := strings.IndexByte(rawOrigin, ':')
	if colon <= 0 || colon == len(rawOrigin)-1 ||
		strings.Contains(rawOrigin[:colon], "/") {
		return "", false
	}

	return "ssh://" + rawOrigin[:colon] + "/" + strings.TrimLeft(
		rawOrigin[colon+1:],
		"/",
	), true
}

func sourceV2GitOutput(
	ctx context.Context,
	root string,
	arguments ...string,
) (string, error) {
	command := realgit.Command(ctx, false, arguments...)
	command.Dir = root
	command.Env = realgit.CleanGitLocalEnv(os.Environ())

	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("run Git %s: %w", strings.Join(arguments, " "), err)
	}

	return strings.TrimSpace(string(output)), nil
}

func sourceV2Digest(namespace, payload string) string {
	digest := sha256.Sum256([]byte(payload))

	return namespace + ":sha256:" + hex.EncodeToString(digest[:])
}

func sourceV2ContentID(namespace string, value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal %s identity: %w", namespace, err)
	}

	return sourceV2Digest(namespace, string(payload)), nil
}

func (layout sourceV2Layout) fragmentPath(fragmentID string) string {
	digest := strings.TrimPrefix(fragmentID, "fragment:sha256:")

	prefix := digest
	if len(prefix) > sourceV2FragmentPrefixLength {
		prefix = prefix[:sourceV2FragmentPrefixLength]
	}

	return filepath.Join(layout.sharedRoot, "fragments", prefix, digest+".json")
}

func (layout sourceV2Layout) baseManifestPath(manifestID string) string {
	digest := strings.TrimPrefix(manifestID, "base:sha256:")

	return filepath.Join(layout.sharedRoot, "bases", digest+".json")
}

func (layout sourceV2Layout) deltaManifestPath(manifestID string) string {
	digest := strings.TrimPrefix(manifestID, "delta:sha256:")

	return filepath.Join(layout.laneRoot, "deltas", digest+".json")
}

func (layout sourceV2Layout) statusPath() string {
	return filepath.Join(layout.laneRoot, "current.json")
}

func writeImmutableSourceV2JSON(path string, value any) (bool, error) {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return false, fmt.Errorf("marshal immutable code-intel object: %w", err)
	}

	payload = append(payload, '\n')

	existing, readErr := os.ReadFile(path)
	if readErr == nil {
		if !bytes.Equal(existing, payload) {
			return false, fmt.Errorf(
				"%w at %s",
				errImmutableSourceV2Conflict,
				path,
			)
		}

		return false, nil
	}

	if !errors.Is(readErr, os.ErrNotExist) {
		return false, fmt.Errorf("read immutable code-intel object %s: %w", path, readErr)
	}

	err = os.MkdirAll(filepath.Dir(path), sourceV2DirMode)
	if err != nil {
		return false, fmt.Errorf("create immutable code-intel directory: %w", err)
	}

	temporary, err := os.CreateTemp(filepath.Dir(path), ".code-intel-v2-*")
	if err != nil {
		return false, fmt.Errorf("create immutable code-intel temporary file: %w", err)
	}

	temporaryPath := temporary.Name()

	defer func() { _ = os.Remove(temporaryPath) }()

	err = temporary.Chmod(sourceV2FileMode)
	if err != nil {
		_ = temporary.Close()

		return false, fmt.Errorf("chmod immutable code-intel object: %w", err)
	}

	_, err = temporary.Write(payload)
	if err != nil {
		_ = temporary.Close()

		return false, fmt.Errorf("write immutable code-intel object: %w", err)
	}

	err = temporary.Close()
	if err != nil {
		return false, fmt.Errorf("close immutable code-intel object: %w", err)
	}

	err = os.Link(temporaryPath, path)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return writeImmutableSourceV2JSON(path, value)
		}

		return false, fmt.Errorf("publish immutable code-intel object: %w", err)
	}

	return true, nil
}

func writeCurrentSourceV2Receipt(path string, receipt SourceStatusReceipt) error {
	payload, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal code-intel status receipt: %w", err)
	}

	payload = append(payload, '\n')

	err = os.MkdirAll(filepath.Dir(path), sourceV2DirMode)
	if err != nil {
		return fmt.Errorf("create code-intel receipt directory: %w", err)
	}

	temporary, err := os.CreateTemp(filepath.Dir(path), ".current-*")
	if err != nil {
		return fmt.Errorf("create code-intel receipt temporary file: %w", err)
	}

	temporaryPath := temporary.Name()

	defer func() { _ = os.Remove(temporaryPath) }()

	err = temporary.Chmod(sourceV2FileMode)
	if err != nil {
		_ = temporary.Close()

		return fmt.Errorf("chmod code-intel receipt: %w", err)
	}

	_, err = temporary.Write(payload)
	if err != nil {
		_ = temporary.Close()

		return fmt.Errorf("write code-intel receipt: %w", err)
	}

	err = temporary.Close()
	if err != nil {
		return fmt.Errorf("close code-intel receipt: %w", err)
	}

	err = os.Rename(temporaryPath, path)
	if err != nil {
		return fmt.Errorf("publish code-intel receipt: %w", err)
	}

	return nil
}

func loadCurrentSourceV2Receipt(path string) (SourceStatusReceipt, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return SourceStatusReceipt{}, fmt.Errorf("read code-intel v2 status receipt: %w", err)
	}

	var receipt SourceStatusReceipt

	err = json.Unmarshal(payload, &receipt)
	if err != nil {
		return SourceStatusReceipt{}, fmt.Errorf(
			"decode code-intel v2 status receipt: %w",
			err,
		)
	}

	if receipt.Contract != SourceV2Contract || receipt.Kind != sourceV2StatusKind {
		return SourceStatusReceipt{}, fmt.Errorf(
			"%w: unexpected code-intel v2 status receipt contract",
			errInvalidSourceV2Receipt,
		)
	}

	return receipt, nil
}
