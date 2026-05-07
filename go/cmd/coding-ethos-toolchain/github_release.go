// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

const defaultHTTPTimeout = 30 * time.Second

func githubAssetURL(args []string) error {
	flags := flag.NewFlagSet("github-asset-url", flag.ExitOnError)
	repo := flags.String("repo", "", "GitHub repository in owner/name form")
	tag := flags.String("tag", "", "Release tag")
	assetSubstring := flags.String(
		"asset-substring",
		"",
		"Release asset name substring",
	)

	inlineErr21 := flags.Parse(args)
	if inlineErr21 != nil {
		return fmt.Errorf("parse github-asset-url flags: %w", inlineErr21)
	}

	if strings.TrimSpace(*repo) == "" {
		return errRepoRequired
	}

	if strings.TrimSpace(*tag) == "" {
		return errTagRequired
	}

	if strings.TrimSpace(*assetSubstring) == "" {
		return errAssetRequired
	}

	url, err := releaseAssetURL(
		http.DefaultClient,
		*repo,
		*tag,
		*assetSubstring,
		os.Getenv("GITHUB_TOKEN"),
	)
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, url)

	return nil
}

func releaseAssetURL(
	client *http.Client,
	repo string,
	tag string,
	assetSubstring string,
	token string,
) (string, error) {
	requestURL := fmt.Sprintf(
		"https://api.github.com/repos/%s/releases/tags/%s",
		repo,
		tag,
	)

	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		requestURL,
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("create GitHub release request: %w", err)
	}

	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-Github-Api-Version", githubAPIVersion)

	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := clientWithTimeout(client).Do(request)
	if err != nil {
		return "", fmt.Errorf("fetch GitHub release %s@%s: %w", repo, tag, err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK ||
		response.StatusCode >= http.StatusMultipleChoices {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, githubErrorBodySize))
		if readErr != nil {
			return "", fmt.Errorf("read GitHub release error response: %w", readErr)
		}

		return "", apperror.Wrapf(
			apperror.StaticError("fetch GitHub release %s@%s: status %d: %s"),
			"fetch GitHub release %s@%s: status %d: %s",
			repo,
			tag,
			response.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}

	var payload release

	inlineErr22 := json.NewDecoder(response.Body).Decode(&payload)
	if inlineErr22 != nil {
		return "", fmt.Errorf("decode GitHub release %s@%s: %w", repo, tag, inlineErr22)
	}

	for _, asset := range payload.Assets {
		if strings.Contains(asset.Name, assetSubstring) {
			return asset.BrowserDownloadURL, nil
		}
	}

	return "", fmt.Errorf(
		"%w: no release asset for %s@%s contains %q",
		errAssetNotFound,
		repo,
		tag,
		assetSubstring,
	)
}

func clientWithTimeout(client *http.Client) *http.Client {
	if client == nil {
		return &http.Client{Timeout: defaultHTTPTimeout}
	}

	if client.Timeout != 0 {
		return client
	}

	copyClient := *client
	copyClient.Timeout = defaultHTTPTimeout

	return &copyClient
}

func printSHA256(args []string) error {
	flags := flag.NewFlagSet("sha256", flag.ExitOnError)

	path := flags.String("file", "", "File to hash")

	inlineErr23 := flags.Parse(args)
	if inlineErr23 != nil {
		return fmt.Errorf("parse sha256 flags: %w", inlineErr23)
	}

	if strings.TrimSpace(*path) == "" {
		return errFileRequired
	}

	sum, err := sha256File(*path)
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, sum)

	return nil
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	hash := sha256.New()

	_, inlineErrE := io.Copy(hash, file)
	if inlineErrE != nil {
		return "", fmt.Errorf("hash %s: %w", path, inlineErrE)
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}
