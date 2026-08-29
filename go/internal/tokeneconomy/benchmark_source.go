// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//nolint:cyclop,funlen,gocyclo,lll,mnd,noinlineerr,wsl_v5 // Archive stages remain reviewable.
package tokeneconomy

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/safeexec"
)

const (
	maximumArchiveFileBytes  = 1 << 30
	maximumArchiveTotalBytes = 4 << 30
	workspaceDirMode         = 0o700
)

var errBenchmarkSource = errors.New("token-economy benchmark source error")

type preparedWorkspace struct {
	Path           string
	BaselineCommit string
}

func prepareBenchmarkWorkspace(
	ctx context.Context,
	task BenchmarkTask,
	workspacePath string,
) (preparedWorkspace, error) {
	if _, err := os.Stat(workspacePath); err == nil {
		return preparedWorkspace{}, fmt.Errorf(
			"%w: workspace already exists: %s",
			errBenchmarkSource,
			workspacePath,
		)
	} else if !errors.Is(err, os.ErrNotExist) {
		return preparedWorkspace{}, fmt.Errorf("inspect benchmark workspace: %w", err)
	}

	err := os.MkdirAll(workspacePath, workspaceDirMode)
	if err != nil {
		return preparedWorkspace{}, fmt.Errorf("create benchmark workspace: %w", err)
	}

	err = extractBenchmarkArchive(ctx, task, workspacePath)
	if err != nil {
		return preparedWorkspace{}, err
	}

	git, err := exec.LookPath("git")
	if err != nil {
		return preparedWorkspace{}, fmt.Errorf("resolve git for benchmark workspace: %w", err)
	}

	for _, arguments := range [][]string{
		{"init", "--quiet", "--initial-branch=benchmark"},
		{"add", "--all", "--force"},
		{
			"-c", "user.name=Coding Ethos Benchmark",
			"-c", "user.email=benchmark@invalid",
			"-c", "commit.gpgSign=false",
			"commit", "--quiet", "--no-gpg-sign", "-m", "frozen benchmark baseline",
		},
	} {
		if err = runBenchmarkGit(ctx, git, workspacePath, arguments...); err != nil {
			return preparedWorkspace{}, err
		}
	}

	baseline, err := benchmarkGitOutput(ctx, git, workspacePath, "rev-parse", "HEAD")
	if err != nil {
		return preparedWorkspace{}, err
	}
	remotes, err := benchmarkGitOutput(ctx, git, workspacePath, "remote")
	if err != nil {
		return preparedWorkspace{}, err
	}
	if strings.TrimSpace(remotes) != "" {
		return preparedWorkspace{}, fmt.Errorf(
			"%w: workspace unexpectedly has a Git remote",
			errBenchmarkSource,
		)
	}

	return preparedWorkspace{
		Path:           workspacePath,
		BaselineCommit: strings.TrimSpace(baseline),
	}, nil
}

func extractBenchmarkArchive(
	ctx context.Context,
	task BenchmarkTask,
	workspacePath string,
) error {
	git, err := exec.LookPath("git")
	if err != nil {
		return fmt.Errorf("resolve git for benchmark archive: %w", err)
	}

	command := safeexec.CommandContext(
		ctx,
		git,
		"-C",
		task.RepositoryPath,
		"archive",
		"--format=tar",
		task.Commit,
	)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open benchmark archive stream: %w", err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr

	archive, err := os.CreateTemp(filepath.Dir(workspacePath), ".benchmark-source-*.tar")
	if err != nil {
		return fmt.Errorf("create benchmark archive staging file: %w", err)
	}
	archivePath := archive.Name()
	defer func() {
		_ = archive.Close()
		_ = os.Remove(archivePath)
	}()

	if err = command.Start(); err != nil {
		return fmt.Errorf("start benchmark archive extraction: %w", err)
	}

	digest := sha256.New()
	written, copyErr := io.Copy(
		io.MultiWriter(archive, digest),
		io.LimitReader(stdout, maximumArchiveTotalBytes+1),
	)
	oversized := written > maximumArchiveTotalBytes
	var killErr error
	if copyErr != nil || oversized {
		closePipeErr := stdout.Close()
		killErr = command.Process.Kill()
		if errors.Is(killErr, os.ErrProcessDone) {
			killErr = nil
		}
		copyErr = errors.Join(copyErr, closePipeErr)
	}
	waitErr := command.Wait()
	if oversized {
		return errors.Join(
			fmt.Errorf("%w: source archive exceeds size limit", errBenchmarkSource),
			copyErr,
			killErr,
			waitErr,
		)
	}
	if err = errors.Join(copyErr, killErr, waitErr); err != nil {
		return fmt.Errorf(
			"capture benchmark source archive: %w: %s",
			err,
			strings.TrimSpace(stderr.String()),
		)
	}
	actual := hex.EncodeToString(digest.Sum(nil))
	if actual != task.SourceArchiveSHA256 {
		return fmt.Errorf(
			"%w: source archive changed: got %s, expected %s",
			errBenchmarkSource,
			actual,
			task.SourceArchiveSHA256,
		)
	}
	if _, err = archive.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind benchmark source archive: %w", err)
	}
	if err = extractTarEntries(tar.NewReader(archive), workspacePath); err != nil {
		return fmt.Errorf("extract benchmark source archive: %w", err)
	}

	return nil
}

func extractTarEntries(reader *tar.Reader, workspacePath string) error {
	var total int64

	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read benchmark archive entry: %w", err)
		}
		if header.Size < 0 || header.Size > maximumArchiveFileBytes {
			return fmt.Errorf(
				"%w: archive entry %q has unsafe size",
				errBenchmarkSource,
				header.Name,
			)
		}
		total += header.Size
		if total > maximumArchiveTotalBytes {
			return fmt.Errorf("%w: archive exceeds extraction size limit", errBenchmarkSource)
		}

		target, err := safeArchiveTarget(workspacePath, header.Name)
		if err != nil {
			return err
		}
		if err = ensureArchivePathSafe(workspacePath, target); err != nil {
			return fmt.Errorf("benchmark archive path %q is unsafe: %w", header.Name, err)
		}
		if err = extractTarEntry(reader, header, workspacePath, target); err != nil {
			return err
		}
	}
}

func ensureArchivePathSafe(root, target string) error {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return fmt.Errorf("resolve safe benchmark archive path: %w", err)
	}

	current := root
	for component := range strings.SplitSeq(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
		if statErr != nil {
			return fmt.Errorf("inspect benchmark archive path component: %w", statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: path traverses an extracted symlink", errBenchmarkSource)
		}
	}

	return nil
}

func safeArchiveTarget(root, name string) (string, error) {
	if filepath.IsAbs(name) {
		return "", fmt.Errorf(
			"%w: archive contains absolute path %q",
			errBenchmarkSource,
			name,
		)
	}

	target := filepath.Clean(filepath.Join(root, filepath.FromSlash(name)))
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return "", fmt.Errorf("resolve benchmark archive path %q: %w", name, err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf(
			"%w: archive path escapes workspace: %q",
			errBenchmarkSource,
			name,
		)
	}

	return target, nil
}

func extractTarEntry(
	reader *tar.Reader,
	header *tar.Header,
	root string,
	target string,
) error {
	switch header.Typeflag {
	case tar.TypeDir:
		if err := os.MkdirAll(target, 0o755); err != nil {
			return fmt.Errorf("create benchmark archive directory %q: %w", header.Name, err)
		}
	case tar.TypeReg:
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create benchmark archive parent %q: %w", header.Name, err)
		}
		mode := os.FileMode(0o644)
		if header.FileInfo().Mode()&0o111 != 0 {
			mode = 0o755
		}
		if err := writeArchiveFile(target, reader, mode); err != nil {
			return fmt.Errorf("write benchmark archive file %q: %w", header.Name, err)
		}
	case tar.TypeSymlink:
		if err := createSafeArchiveSymlink(root, target, header.Linkname); err != nil {
			return fmt.Errorf("create benchmark archive symlink %q: %w", header.Name, err)
		}
	case tar.TypeXHeader, tar.TypeXGlobalHeader:
		// Archive metadata carries no filesystem payload of its own.
		return nil
	default:
		return fmt.Errorf(
			"%w: archive entry %q has unsupported type %d",
			errBenchmarkSource,
			header.Name,
			header.Typeflag,
		)
	}

	return nil
}

func writeArchiveFile(path string, reader io.Reader, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("create benchmark archive file: %w", err)
	}

	_, copyErr := io.Copy(file, reader)
	closeErr := file.Close()

	return errors.Join(copyErr, closeErr)
}

func createSafeArchiveSymlink(root, target, link string) error {
	if filepath.IsAbs(link) {
		return fmt.Errorf("%w: absolute symlink target is forbidden", errBenchmarkSource)
	}

	resolved := filepath.Clean(
		filepath.Join(filepath.Dir(target), filepath.FromSlash(link)),
	)
	relative, err := filepath.Rel(root, resolved)
	if err != nil {
		return fmt.Errorf("resolve benchmark symlink target: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: symlink target escapes the workspace", errBenchmarkSource)
	}
	if err = os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create benchmark symlink parent: %w", err)
	}

	if err = os.Symlink(filepath.FromSlash(link), target); err != nil {
		return fmt.Errorf("create benchmark symlink: %w", err)
	}

	return nil
}

func runBenchmarkGit(ctx context.Context, git, root string, args ...string) error {
	command := safeexec.CommandContext(ctx, git, append([]string{"-C", root}, args...)...)
	command.Env = benchmarkGitEnvironment()

	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"run benchmark Git command %q: %w: %s",
			strings.Join(args, " "),
			err,
			strings.TrimSpace(string(output)),
		)
	}

	return nil
}

func benchmarkGitOutput(
	ctx context.Context,
	git string,
	root string,
	args ...string,
) (string, error) {
	command := safeexec.CommandContext(ctx, git, append([]string{"-C", root}, args...)...)
	command.Env = benchmarkGitEnvironment()

	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("query benchmark Git state: %w", err)
	}

	return string(output), nil
}

func benchmarkGitEnvironment() []string {
	environment := scrubBenchmarkEnvironment(os.Environ())
	environment = append(environment,
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)

	return environment
}
