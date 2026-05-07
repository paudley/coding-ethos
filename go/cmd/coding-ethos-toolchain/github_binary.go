// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

func installGitHubBinaryCommand(args []string) error {
	flags := flag.NewFlagSet("install-github-binary", flag.ExitOnError)
	repo := flags.String("repo", "", "GitHub repository in owner/name form")
	tag := flags.String("tag", "", "Release tag")
	assetSubstring := flags.String(
		"asset-substring",
		"",
		"Release asset name substring",
	)
	binary := flags.String("binary", "", "Binary name to install")
	destDir := flags.String("dest-dir", "", "Destination directory")

	expectedSHA256 := flags.String("sha256", "", "Expected release asset SHA-256")

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse install-github-binary flags: %w", err)
	}

	switch {
	case strings.TrimSpace(*repo) == "":
		return errRepoRequired
	case strings.TrimSpace(*tag) == "":
		return errTagRequired
	case strings.TrimSpace(*assetSubstring) == "":
		return errAssetRequired
	case strings.TrimSpace(*binary) == "":
		return errBinaryRequired
	case strings.TrimSpace(*destDir) == "":
		return errDestRequired
	case strings.TrimSpace(*expectedSHA256) == "":
		return errHashRequired
	}

	return installGitHubBinary(
		http.DefaultClient,
		*repo,
		*tag,
		*assetSubstring,
		*binary,
		*destDir,
		*expectedSHA256,
		os.Getenv("GITHUB_TOKEN"),
	)
}

func installGitHubBinary(
	client *http.Client,
	repo string,
	tag string,
	assetSubstring string,
	binary string,
	destDir string,
	expectedSHA256 string,
	token string,
) error {
	url, err := releaseAssetURL(client, repo, tag, assetSubstring, token)
	if err != nil {
		return err
	}

	inlineErr7 := os.MkdirAll(destDir, directoryMode)
	if inlineErr7 != nil {
		return fmt.Errorf(
			"create managed binary destination %s: %w",
			destDir,
			inlineErr7,
		)
	}

	workDir, err := os.MkdirTemp(destDir, ".coding-ethos-download.")
	if err != nil {
		return fmt.Errorf("create download workspace in %s: %w", destDir, err)
	}
	defer os.RemoveAll(workDir)

	archivePath := filepath.Join(workDir, downloadedFileName(url))

	inlineErr8 := downloadGitHubAsset(client, url, archivePath, token)
	if inlineErr8 != nil {
		return inlineErr8
	}

	actualSHA256, err := sha256File(archivePath)
	if err != nil {
		return err
	}

	if actualSHA256 != expectedSHA256 {
		return apperror.Wrapf(
			apperror.StaticError("SHA-256 mismatch for %s: expected %s, actual %s"),
			"SHA-256 mismatch for %s: expected %s, actual %s",
			archivePath,
			expectedSHA256,
			actualSHA256,
		)
	}

	return installDownloadedAsset(
		archivePath,
		binary,
		destDir,
		filepath.Join(workDir, "extract"),
	)
}

func downloadedFileName(rawURL string) string {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "asset"
	}

	name := filepath.Base(parsedURL.Path)
	if name == "." || name == "/" || name == "" {
		return "asset"
	}

	return name
}

func downloadGitHubAsset(client *http.Client, rawURL, outputPath, token string) error {
	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		rawURL,
		nil,
	)
	if err != nil {
		return fmt.Errorf("create GitHub asset request: %w", err)
	}

	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-Github-Api-Version", githubAPIVersion)
	}

	httpClient := clientWithTimeout(client)

	response, err := httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("download GitHub asset %s: %w", rawURL, err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK ||
		response.StatusCode >= http.StatusMultipleChoices {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, githubErrorBodySize))
		if readErr != nil {
			return fmt.Errorf("read GitHub asset error response: %w", readErr)
		}

		return apperror.Wrapf(
			apperror.StaticError("download GitHub asset %s: status %d: %s"),
			"download GitHub asset %s: status %d: %s",
			rawURL,
			response.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}

	output, err := os.OpenFile(
		outputPath,
		os.O_CREATE|os.O_EXCL|os.O_WRONLY,
		privateFileMode,
	)
	if err != nil {
		return fmt.Errorf("create downloaded asset %s: %w", outputPath, err)
	}

	_, inlineErrB := io.Copy(output, response.Body)
	if inlineErrB != nil {
		_ = output.Close()

		return fmt.Errorf("write downloaded asset %s: %w", outputPath, inlineErrB)
	}

	inlineErr9 := output.Close()
	if inlineErr9 != nil {
		return fmt.Errorf("close downloaded asset %s: %w", outputPath, inlineErr9)
	}

	return nil
}

func installDownloadedAsset(archivePath, binary, destDir, extractDir string) error {
	switch {
	case strings.HasSuffix(archivePath, ".tar.gz"),
		strings.HasSuffix(archivePath, ".tgz"):
		err := extractTarGzip(archivePath, extractDir)
		if err != nil {
			return err
		}
	case strings.HasSuffix(archivePath, ".zip"):
		err := extractZip(archivePath, extractDir)
		if err != nil {
			return err
		}
	case strings.HasSuffix(archivePath, ".tar.xz"),
		strings.HasSuffix(archivePath, ".txz"):
		err := extractTarXZ(archivePath, extractDir)
		if err != nil {
			return err
		}
	default:
		return installBinaryFile(archivePath, filepath.Join(destDir, binary))
	}

	found, err := findExecutableNamed(extractDir, binary)
	if err != nil {
		return err
	}

	return installBinaryFile(found, filepath.Join(destDir, binary))
}

func extractTarGzip(archivePath, destDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open tar.gz asset %s: %w", archivePath, err)
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("read gzip asset %s: %w", archivePath, err)
	}
	defer gzipReader.Close()

	return extractTar(tar.NewReader(gzipReader), destDir)
}

func extractTar(tarReader *tar.Reader, destDir string) error {
	err := os.MkdirAll(destDir, directoryMode)
	if err != nil {
		return fmt.Errorf("create extract dir %s: %w", destDir, err)
	}

	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("read tar asset: %w", err)
		}

		target, err := safeExtractPath(destDir, header.Name)
		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			err = createExtractDir(target, "tar")
		case tar.TypeReg:
			err = extractTarFile(tarReader, header, target)
		default:
			err = nil
		}

		if err != nil {
			return err
		}
	}
}

func createExtractDir(target, archiveType string) error {
	err := os.MkdirAll(target, directoryMode)
	if err != nil {
		return fmt.Errorf("create %s directory %s: %w", archiveType, target, err)
	}

	return nil
}

func extractTarFile(tarReader *tar.Reader, header *tar.Header, target string) error {
	err := createExtractParent(target, "tar")
	if err != nil {
		return err
	}

	output, err := os.OpenFile(
		target,
		os.O_CREATE|os.O_EXCL|os.O_WRONLY,
		header.FileInfo().Mode(),
	)
	if err != nil {
		return fmt.Errorf("create tar file %s: %w", target, err)
	}

	size, err := tarArchiveMemberSize(header)
	if err != nil {
		_ = output.Close()

		return err
	}

	err = copyBoundedArchiveMember(output, tarReader, size)
	if err != nil {
		_ = output.Close()

		return fmt.Errorf("write tar file %s: %w", target, err)
	}

	err = output.Close()
	if err != nil {
		return fmt.Errorf("close tar file %s: %w", target, err)
	}

	return nil
}

func extractZip(archivePath, destDir string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open zip asset %s: %w", archivePath, err)
	}
	defer reader.Close()

	inlineErr10 := os.MkdirAll(destDir, directoryMode)
	if inlineErr10 != nil {
		return fmt.Errorf("create extract dir %s: %w", destDir, inlineErr10)
	}

	for _, file := range reader.File {
		err = extractZipMember(destDir, file)
		if err != nil {
			return err
		}
	}

	return nil
}

func extractZipMember(destDir string, file *zip.File) error {
	target, err := safeExtractPath(destDir, file.Name)
	if err != nil {
		return err
	}

	if file.FileInfo().IsDir() {
		return createExtractDir(target, "zip")
	}

	err = createExtractParent(target, "zip")
	if err != nil {
		return err
	}

	source, err := file.Open()
	if err != nil {
		return fmt.Errorf("open zip member %s: %w", file.Name, err)
	}

	output, err := os.OpenFile(
		target,
		os.O_CREATE|os.O_EXCL|os.O_WRONLY,
		file.FileInfo().Mode(),
	)
	if err != nil {
		_ = source.Close()

		return fmt.Errorf("create zip file %s: %w", target, err)
	}

	err = copyBoundedArchiveMember(output, source, file.UncompressedSize64)
	if err != nil {
		_ = source.Close()
		_ = output.Close()

		return fmt.Errorf("write zip file %s: %w", target, err)
	}

	err = source.Close()
	if err != nil {
		_ = output.Close()

		return fmt.Errorf("close zip member %s: %w", file.Name, err)
	}

	err = output.Close()
	if err != nil {
		return fmt.Errorf("close zip file %s: %w", target, err)
	}

	return nil
}

func createExtractParent(target, archiveType string) error {
	err := os.MkdirAll(filepath.Dir(target), directoryMode)
	if err != nil {
		return fmt.Errorf(
			"create %s parent directory %s: %w",
			archiveType,
			filepath.Dir(target),
			err,
		)
	}

	return nil
}

func tarArchiveMemberSize(header *tar.Header) (uint64, error) {
	if header.Size < 0 {
		return 0, apperror.Wrapf(
			errNegativeTarMember,
			"tar member %s has negative size",
			header.Name,
		)
	}

	return uint64(header.Size), nil
}

func copyBoundedArchiveMember(
	output io.Writer,
	source io.Reader,
	declaredSize uint64,
) error {
	if declaredSize > maxExtractedArchiveMemberBytes {
		return apperror.Wrapf(
			errArchiveTooLarge,
			"archive member exceeds %d byte limit",
			maxExtractedArchiveMemberBytes,
		)
	}

	_, err := io.CopyN(output, source, int64(declaredSize))
	if errors.Is(err, io.EOF) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("copy archive member: %w", err)
	}

	return nil
}

func extractTarXZ(archivePath, destDir string) error {
	inlineErr14 := os.MkdirAll(destDir, directoryMode)
	if inlineErr14 != nil {
		return fmt.Errorf("create extract dir %s: %w", destDir, inlineErr14)
	}

	command := exec.CommandContext(
		context.Background(),
		"tar",
		"-xJf",
		archivePath,
		"-C",
		destDir,
	)

	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"extract tar.xz asset %s: %w: %s",
			archivePath,
			err,
			strings.TrimSpace(string(output)),
		)
	}

	return nil
}

func safeExtractPath(destDir, memberName string) (string, error) {
	cleanName := filepath.Clean(memberName)
	if filepath.IsAbs(cleanName) || cleanName == ".." ||
		strings.HasPrefix(cleanName, ".."+string(os.PathSeparator)) {
		return "", apperror.Wrapf(
			apperror.StaticError("archive member escapes extract dir: %s"),
			"archive member escapes extract dir: %s",
			memberName,
		)
	}

	target := filepath.Join(destDir, cleanName)

	rel, err := filepath.Rel(destDir, target)
	if err != nil {
		return "", fmt.Errorf("resolve archive member %s: %w", memberName, err)
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", apperror.Wrapf(
			apperror.StaticError("archive member escapes extract dir: %s"),
			"archive member escapes extract dir: %s",
			memberName,
		)
	}

	return target, nil
}

func findExecutableNamed(root, binary string) (string, error) {
	var found string

	err := filepath.WalkDir(
		root,
		func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return fmt.Errorf("scan extracted asset path %s: %w", path, walkErr)
			}

			if entry.IsDir() || entry.Name() != binary || found != "" {
				return nil
			}

			info, err := entry.Info()
			if err != nil {
				return fmt.Errorf("inspect extracted asset path %s: %w", path, err)
			}

			if info.Mode()&0o111 != 0 {
				found = path

				return filepath.SkipAll
			}

			return nil
		},
	)
	if err != nil {
		return "", fmt.Errorf("scan extracted asset %s: %w", root, err)
	}

	if found == "" {
		return "", apperror.Wrapf(
			apperror.StaticError(
				"%s not found as executable in downloaded GitHub asset",
			),
			"%s not found as executable in downloaded GitHub asset",
			binary,
		)
	}

	return found, nil
}

func installBinaryFile(source, target string) error {
	payload, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read binary %s: %w", source, err)
	}

	inlineErr15 := os.MkdirAll(filepath.Dir(target), directoryMode)
	if inlineErr15 != nil {
		return fmt.Errorf(
			"create binary destination dir %s: %w",
			filepath.Dir(target),
			inlineErr15,
		)
	}

	return writeExecutableFile(target, payload)
}
