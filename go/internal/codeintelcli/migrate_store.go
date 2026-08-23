// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintelcli

import (
	"context"
	"flag"
	"fmt"
	"os"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
)

func migrateStore(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("migrate-store", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root owning the legacy store")
	source := flags.String(
		"source",
		"",
		"Legacy source store; defaults to the repository-local DuckDB",
	)
	destination := flags.String(
		"destination",
		"",
		"Private destination store; defaults through CODE_ETHOS_STATE_ROOT",
	)
	managedDestination := flags.String(
		"db",
		"",
		"Private destination injected by the managed code-intel runner",
	)
	manifest := flags.String(
		"manifest",
		"",
		"New audit manifest path; defaults beside the destination",
	)

	err := parseCommandFlags(flags, args, "migrate-store")
	if err != nil {
		return err
	}

	destinationPath := *destination
	if destinationPath == "" {
		destinationPath = *managedDestination
	}

	result, err := codeintel.MigrateStore(ctx, codeintel.StoreMigrationOptions{
		RepositoryRoot:  *root,
		SourcePath:      *source,
		DestinationPath: destinationPath,
		ManifestPath:    *manifest,
	})
	if err != nil {
		return fmt.Errorf("migrate code-intel store: %w", err)
	}

	return encodeJSON(os.Stdout, result)
}
