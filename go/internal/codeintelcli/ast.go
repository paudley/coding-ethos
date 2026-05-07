// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintelcli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
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
