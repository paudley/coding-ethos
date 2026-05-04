// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
)

func indexCode(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("index-code", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root to index")
	dbPath := flags.String("db", "", "SQLite code intelligence database path")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse index-code flags: %w", err)
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	summary, err := codeintel.NewASTIndexer(store).IndexPaths(ctx, *root, flags.Args())
	if err != nil {
		return err
	}

	return encodeJSON(os.Stdout, summary)
}

func printCodeChunks(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("code-chunks", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "SQLite code intelligence database path")
	path := flags.String("path", "", "Filter by source path")
	language := flags.String("language", "", "Filter by language")
	symbolKind := flags.String("symbol-kind", "", "Filter by symbol kind")
	symbolName := flags.String("symbol-name", "", "Filter by symbol name")
	limit := flags.Int("limit", 20, "Maximum result count")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse code-chunks flags: %w", err)
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	chunks, err := store.CodeChunks(ctx, codeintel.CodeChunkQuery{
		Path:       *path,
		Language:   *language,
		SymbolKind: *symbolKind,
		SymbolName: *symbolName,
		Limit:      *limit,
	})
	if err != nil {
		return err
	}

	return encodeJSON(os.Stdout, chunks)
}
