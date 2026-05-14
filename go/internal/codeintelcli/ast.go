// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintelcli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/shellparse"
)

const defaultAnatomyMapSymbolsPerFile = 6

var (
	errUnsupportedAnatomyMapFormat = apperror.StaticError(
		"unsupported anatomy-map format",
	)
	errListingPathRequired = apperror.StaticError(
		"listing path or listing command is required",
	)
	errListingCommandUnsupported = apperror.StaticError(
		"listing command is not a supported single-target ls/tree invocation",
	)
)

func indexCode(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("index-code", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root to index")

	err := parseCommandFlags(flags, args, "index-code")
	if err != nil {
		return err
	}

	store, err := openStore(ctx, *storeFlags.root, *storeFlags.dbPath)
	if err != nil {
		return fmt.Errorf("index code paths: %w", err)
	}
	defer store.Close()

	summary, err := codeintel.NewASTIndexer(store).
		IndexPaths(ctx, *storeFlags.root, flags.Args())
	if err != nil {
		return fmt.Errorf("index code paths: %w", err)
	}

	return encodeJSON(os.Stdout, summary)
}

func printCodeChunks(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("code-chunks", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	path := flags.String("path", "", "Filter by source path")
	language := flags.String("language", "", "Filter by language")
	symbolKind := flags.String("symbol-kind", "", "Filter by symbol kind")
	symbolName := flags.String("symbol-name", "", "Filter by symbol name")
	symbolPath := flags.String("symbol-path", "", "Filter by symbol path")
	limit := addResultLimit(flags)

	return parseAndPrintStoreJSON(
		ctx,
		args,
		"code-chunks",
		flags,
		storeFlags,
		func(store *codeintel.Store) (any, error) {
			return store.CodeChunks(ctx, codeintel.CodeChunkQuery{
				Path:       *path,
				Language:   *language,
				SymbolKind: *symbolKind,
				SymbolName: *symbolName,
				SymbolPath: *symbolPath,
				Limit:      *limit,
			})
		},
	)
}

func printAnatomyMap(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("anatomy-map", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root to inspect")
	path := flags.String("path", ".", "Directory path to summarize")
	language := flags.String("language", "", "Filter by language")
	outputFormat := flags.String("format", "json", "Output format: json or toon")
	symbolsPerFile := flags.Int(
		"symbols-per-file",
		defaultAnatomyMapSymbolsPerFile,
		"Maximum symbols per file",
	)
	limit := addResultLimit(flags)

	err := parseCommandFlags(flags, args, "anatomy-map")
	if err != nil {
		return err
	}

	store, err := openStore(ctx, *storeFlags.root, *storeFlags.dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	_, err = codeintel.NewASTIndexer(store).IndexPaths(ctx, *storeFlags.root, []string{
		*path,
	})
	if err != nil {
		return fmt.Errorf("refresh anatomy map index: %w", err)
	}

	anatomy, err := store.DirectoryAnatomy(ctx, codeintel.DirectoryAnatomyQuery{
		Path:           *path,
		Language:       *language,
		Limit:          *limit,
		SymbolsPerFile: *symbolsPerFile,
	})
	if err != nil {
		return fmt.Errorf("read anatomy map: %w", err)
	}

	switch strings.ToLower(strings.TrimSpace(*outputFormat)) {
	case "", "json":
		return encodeJSON(os.Stdout, anatomy)
	case "toon":
		return printDirectoryAnatomyTOON(anatomy)
	default:
		return fmt.Errorf("%w: %s", errUnsupportedAnatomyMapFormat, *outputFormat)
	}
}

func enrichDirectoryListing(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("enrich-listing", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root to inspect")
	path := flags.String("path", "", "Directory path to summarize")
	command := flags.String("command", "", "Original listing command")
	listingFile := flags.String("listing-file", "", "File containing raw listing output")
	language := flags.String("language", "", "Filter by language")
	symbolsPerFile := flags.Int(
		"symbols-per-file",
		defaultAnatomyMapSymbolsPerFile,
		"Maximum symbols per file",
	)
	limit := addResultLimit(flags)

	err := parseCommandFlags(flags, args, "enrich-listing")
	if err != nil {
		return err
	}

	targetPath, err := listingTargetPath(*path, *command)
	if err != nil {
		return err
	}

	listing, err := readListingInput(*listingFile)
	if err != nil {
		return err
	}

	store, err := openStore(ctx, *storeFlags.root, *storeFlags.dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	_, err = codeintel.NewASTIndexer(store).IndexPaths(ctx, *storeFlags.root, []string{
		targetPath,
	})
	if err != nil {
		return fmt.Errorf("refresh listing anatomy index: %w", err)
	}

	output, err := store.EnrichDirectoryListing(ctx, codeintel.DirectoryAnatomyQuery{
		Path:           targetPath,
		Language:       *language,
		Limit:          *limit,
		SymbolsPerFile: *symbolsPerFile,
	}, listing)
	if err != nil {
		return fmt.Errorf("enrich directory listing: %w", err)
	}

	_, err = fmt.Fprint(os.Stdout, output.Text)
	if err != nil {
		return fmt.Errorf("write enriched listing: %w", err)
	}

	return nil
}

func listingTargetPath(path, command string) (string, error) {
	path = strings.TrimSpace(path)
	if path != "" {
		return path, nil
	}

	command = strings.TrimSpace(command)
	if command == "" {
		return "", errListingPathRequired
	}

	commands, err := shellparse.Commands(command)
	if err != nil {
		return "", fmt.Errorf("parse listing command: %w", err)
	}

	if len(commands) != 1 {
		return "", errListingCommandUnsupported
	}

	invocation, ok := agentproxy.DetectDirectoryListingInvocation(commands[0].Argv)
	if !ok {
		return "", errListingCommandUnsupported
	}

	return invocation.Path, nil
}

func readListingInput(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		payload, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read listing from stdin: %w", err)
		}

		return string(payload), nil
	}

	payload, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read listing file %q: %w", path, err)
	}

	return string(payload), nil
}

func printDirectoryAnatomyTOON(anatomy codeintel.DirectoryAnatomy) error {
	output := codeintel.RenderDirectoryAnatomyTOON(anatomy)
	if output == "" {
		return nil
	}

	_, err := fmt.Fprintln(os.Stdout, output)
	if err != nil {
		return fmt.Errorf("write anatomy map TOON: %w", err)
	}

	return nil
}

func printCodeContext(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("code-context", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	chunkID := flags.String("chunk-id", "", "Code chunk ID")
	path := flags.String("path", "", "Filter by source path")
	symbolPath := flags.String("symbol-path", "", "Symbol path")
	line := flags.Int("line", 0, "One-based source line for nearest context lookup")
	limit := addRelatedLimit(flags)

	err := parseCommandFlags(flags, args, "code-context")
	if err != nil {
		return err
	}

	if strings.TrimSpace(*chunkID) == "" &&
		((strings.TrimSpace(*path) == "" || strings.TrimSpace(*symbolPath) == "") &&
			(strings.TrimSpace(*path) == "" || *line <= 0)) {
		return fmt.Errorf(
			"%w: %s",
			errCodeContextTarget,
			"--chunk-id, both --path and --symbol-path, or --path and --line are required",
		)
	}

	store, err := openStore(ctx, *storeFlags.root, *storeFlags.dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	context, err := store.CodeContext(ctx, codeintel.CodeContextQuery{
		ChunkID:    *chunkID,
		Path:       *path,
		SymbolPath: *symbolPath,
		Line:       *line,
		Limit:      *limit,
	})
	if err != nil {
		return fmt.Errorf("read code context: %w", err)
	}

	return encodeJSON(os.Stdout, context)
}

func printRepoMap(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("repo-map", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	path := flags.String("path", "", "Filter by source path")
	language := flags.String("language", "", "Filter by language")
	limit := addResultLimit(flags)

	return parseAndPrintStoreJSON(
		ctx,
		args,
		"repo-map",
		flags,
		storeFlags,
		func(store *codeintel.Store) (any, error) {
			return store.RepoMap(ctx, codeintel.CompactCodeContextQuery{
				Path:     *path,
				Language: *language,
				Limit:    *limit,
			})
		},
	)
}

func printCompactContext(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("compact-context", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	path := flags.String("path", "", "Filter by source path")
	language := flags.String("language", "", "Filter by language")
	limit := addResultLimit(flags)

	return parseAndPrintStoreJSON(
		ctx,
		args,
		"compact-context",
		flags,
		storeFlags,
		func(store *codeintel.Store) (any, error) {
			return store.CompactCodeContext(ctx, codeintel.CompactCodeContextQuery{
				Path:     *path,
				Language: *language,
				Limit:    *limit,
			})
		},
	)
}
