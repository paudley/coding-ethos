// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lintcapture

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

var errLintSourceRootEscapesRepo = errors.New("configured lint source root escapes repo")

type TargetResolver struct {
	InvocationCwd string
	ConsumerRoot  string
	SourceRoots   []string
}

func NewTargetResolver(
	consumerRoot string,
	invocationCwd string,
	sourceRoots []string,
) (TargetResolver, error) {
	root, err := filepath.Abs(consumerRoot)
	if err != nil {
		return TargetResolver{}, fmt.Errorf("resolve consumer root: %w", err)
	}
	root = filepath.Clean(root)
	cwd := strings.TrimSpace(invocationCwd)
	if cwd == "" {
		cwd = root
	}
	if !filepath.IsAbs(cwd) {
		cwd = filepath.Join(root, cwd)
	}
	cwd = filepath.Clean(cwd)

	roots, err := containedSourceRoots(root, sourceRoots)
	if err != nil {
		return TargetResolver{}, err
	}

	return TargetResolver{InvocationCwd: cwd, ConsumerRoot: root, SourceRoots: roots}, nil
}

func (resolver TargetResolver) ResolveArgs(args []string) ([]string, error) {
	resolved := make([]string, 0, len(args))
	for _, arg := range args {
		targets, err := resolver.resolveArg(arg)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, targets...)
	}

	return resolved, nil
}

func (resolver TargetResolver) resolveArg(arg string) ([]string, error) {
	if passthroughArg(arg) {
		return []string{arg}, nil
	}
	if hasGlob(arg) {
		matches, err := resolver.resolveGlob(arg)
		if err != nil {
			return nil, err
		}
		if len(matches) > 0 {
			return matches, nil
		}

		return []string{arg}, nil
	}

	return []string{resolver.ResolvePath(arg)}, nil
}

func (resolver TargetResolver) ResolvePath(arg string) string {
	if filepath.IsAbs(arg) {
		return filepath.Clean(arg)
	}

	for _, base := range resolver.searchBases() {
		candidate := filepath.Join(base, arg)
		if pathExists(candidate) {
			return filepath.Clean(candidate)
		}
	}

	if strings.Contains(arg, string(filepath.Separator)) || strings.Contains(arg, "/") {
		return filepath.Clean(filepath.Join(resolver.InvocationCwd, arg))
	}

	return arg
}

func (resolver TargetResolver) resolveGlob(pattern string) ([]string, error) {
	for _, base := range resolver.searchBases() {
		matches, err := filepath.Glob(filepath.Join(base, filepath.FromSlash(pattern)))
		if err != nil {
			return nil, fmt.Errorf("resolve lint target glob %q: %w", pattern, err)
		}
		if len(matches) == 0 {
			continue
		}
		slices.Sort(matches)
		for index := range matches {
			matches[index] = filepath.Clean(matches[index])
		}

		return matches, nil
	}

	return nil, nil
}

func (resolver TargetResolver) searchBases() []string {
	bases := []string{resolver.InvocationCwd, resolver.ConsumerRoot}
	for _, root := range resolver.SourceRoots {
		bases = append(bases, filepath.Join(resolver.ConsumerRoot, root))
	}

	return bases
}

func containedSourceRoots(repoRoot string, roots []string) ([]string, error) {
	contained := make([]string, 0, len(roots))
	for _, root := range roots {
		text := strings.TrimSpace(root)
		if text == "" {
			continue
		}
		if filepath.IsAbs(text) {
			return nil, fmt.Errorf("%w: %s", errLintSourceRootEscapesRepo, root)
		}
		resolved, err := filepath.Abs(filepath.Join(repoRoot, text))
		if err != nil {
			return nil, fmt.Errorf("resolve lint source root %q: %w", root, err)
		}
		resolved = filepath.Clean(resolved)
		rel, err := filepath.Rel(repoRoot, resolved)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("%w: %s", errLintSourceRootEscapesRepo, root)
		}
		contained = append(contained, filepath.ToSlash(rel))
	}

	return slices.Compact(contained), nil
}

func passthroughArg(arg string) bool {
	return strings.HasPrefix(arg, "-") || strings.Contains(arg, "=") ||
		arg == "." || arg == "./..." || arg == "..."
}

func hasGlob(arg string) bool {
	return strings.ContainsAny(arg, "*?[")
}

func pathExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}
