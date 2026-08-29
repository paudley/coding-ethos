// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"blackcat.ca/coding-ethos/go/internal/astfacts"
	"blackcat.ca/coding-ethos/go/internal/realgit"
)

const (
	sourceV2GitTreeMetadataFields = 3
	sourceV2RepairCommand         = "coding-ethos-code-intel sync --root ."
)

var errInvalidSourceV2Input = errors.New("invalid code-intel v2 source input")

type sourceV2Input struct {
	path        string
	contentHash string
	contents    []byte
	descriptor  astfacts.LanguageDescriptor
	oversized   bool
}

type sourceV2Scan struct {
	layout              sourceV2Layout
	current             map[string]sourceV2Input
	head                map[string]sourceV2Input
	excluded            map[string]int
	headOID             string
	configHash          string
	extractorSetHash    string
	worktreeFingerprint string
	sourceSnapshotID    SourceSnapshotID
}

type sourceV2BuildCounters struct {
	cacheHits        map[string]bool
	fragmentsReused  int
	fragmentsWritten int
}

type sourceV2BuiltManifests struct {
	base     sourceV2BaseManifest
	delta    sourceV2DeltaManifest
	warnings []string
}

// SyncSourceIndex materializes an immutable shared base and a lane-local delta.
// Existing v1 stores are deliberately neither removed nor rewritten.
func SyncSourceIndex(
	ctx context.Context,
	root string,
	external ExternalBatchExtractor,
) (SourceSyncReceipt, error) {
	scan, err := inspectSourceV2Repository(ctx, root, true)
	if err != nil {
		return SourceSyncReceipt{}, err
	}

	err = validateSourceV2ExternalExtractor(ctx, scan, external)
	if err != nil {
		return SourceSyncReceipt{}, err
	}

	counters := sourceV2BuildCounters{cacheHits: map[string]bool{}}

	manifests, err := buildSourceV2Manifests(ctx, scan, external, &counters)
	if err != nil {
		return SourceSyncReceipt{}, err
	}

	return publishSourceV2Generation(scan, manifests, counters)
}

func buildSourceV2Manifests(
	ctx context.Context,
	scan sourceV2Scan,
	external ExternalBatchExtractor,
	counters *sourceV2BuildCounters,
) (sourceV2BuiltManifests, error) {
	baseInputs, deltaInputs, tombstones := splitSourceV2Inputs(scan.head, scan.current)
	knownFragments := knownSourceV2Fragments(scan)

	base, baseWarnings, err := buildSourceV2BaseManifest(
		ctx,
		scan,
		baseInputs,
		external,
		knownFragments,
		counters,
	)
	if err != nil {
		return sourceV2BuiltManifests{}, err
	}

	delta, deltaWarnings, err := buildSourceV2DeltaManifest(
		ctx,
		scan,
		base.ManifestID,
		deltaInputs,
		tombstones,
		external,
		knownFragments,
		counters,
	)
	if err != nil {
		return sourceV2BuiltManifests{}, err
	}

	warnings := slices.Concat(baseWarnings, deltaWarnings)
	slices.Sort(warnings)
	warnings = slices.Compact(warnings)

	return sourceV2BuiltManifests{base: base, delta: delta, warnings: warnings}, nil
}

func buildSourceV2BaseManifest(
	ctx context.Context,
	scan sourceV2Scan,
	inputs []sourceV2Input,
	external ExternalBatchExtractor,
	knownFragments map[string]string,
	counters *sourceV2BuildCounters,
) (sourceV2BaseManifest, []string, error) {
	entries, warnings, err := buildSourceV2Entries(
		ctx, scan.layout, inputs, external, knownFragments, counters,
	)
	if err != nil {
		return sourceV2BaseManifest{}, nil, fmt.Errorf(
			"build code-intel base fragments: %w",
			err,
		)
	}

	manifest := sourceV2BaseManifest{
		Contract:         SourceV2Contract,
		Kind:             sourceV2BaseManifestKind,
		RepositoryID:     scan.layout.repositoryID,
		HeadOID:          scan.headOID,
		ConfigHash:       scan.configHash,
		ExtractorSetHash: scan.extractorSetHash,
		Entries:          entries,
	}

	manifest.ManifestID, err = sourceV2ManifestID("base", manifest)
	if err != nil {
		return sourceV2BaseManifest{}, nil, err
	}

	_, err = writeImmutableSourceV2JSON(
		scan.layout.baseManifestPath(manifest.ManifestID),
		manifest,
	)
	if err != nil {
		return sourceV2BaseManifest{}, nil, err
	}

	return manifest, warnings, nil
}

func buildSourceV2DeltaManifest(
	ctx context.Context,
	scan sourceV2Scan,
	baseManifestID string,
	inputs []sourceV2Input,
	tombstones []string,
	external ExternalBatchExtractor,
	knownFragments map[string]string,
	counters *sourceV2BuildCounters,
) (sourceV2DeltaManifest, []string, error) {
	entries, warnings, err := buildSourceV2Entries(
		ctx, scan.layout, inputs, external, knownFragments, counters,
	)
	if err != nil {
		return sourceV2DeltaManifest{}, nil, fmt.Errorf(
			"build code-intel lane fragments: %w",
			err,
		)
	}

	manifest := sourceV2DeltaManifest{
		Contract:         SourceV2Contract,
		Kind:             sourceV2DeltaManifestKind,
		RepositoryID:     scan.layout.repositoryID,
		WorktreeID:       scan.layout.worktreeID,
		HeadOID:          scan.headOID,
		BaseManifestID:   baseManifestID,
		ConfigHash:       scan.configHash,
		ExtractorSetHash: scan.extractorSetHash,
		Entries:          entries,
		Tombstones:       tombstones,
	}

	manifest.ManifestID, err = sourceV2DeltaManifestID(manifest)
	if err != nil {
		return sourceV2DeltaManifest{}, nil, err
	}

	_, err = writeImmutableSourceV2JSON(
		scan.layout.deltaManifestPath(manifest.ManifestID),
		manifest,
	)
	if err != nil {
		return sourceV2DeltaManifest{}, nil, err
	}

	return manifest, warnings, nil
}

func publishSourceV2Generation(
	scan sourceV2Scan,
	manifests sourceV2BuiltManifests,
	counters sourceV2BuildCounters,
) (SourceSyncReceipt, error) {
	mergedEntries := mergeSourceV2Entries(
		manifests.base.Entries,
		manifests.delta.Entries,
		manifests.delta.Tombstones,
	)
	coverage := sourceV2Coverage(
		scan.current,
		mergedEntries,
		scan.excluded,
		counters.cacheHits,
	)
	readiness := sourceV2Readiness(coverage)
	generationID := sourceV2Digest(
		"generation",
		string(scan.sourceSnapshotID)+"\x00"+
			manifests.base.ManifestID+"\x00"+manifests.delta.ManifestID,
	)
	readiness.Identity = sourceV2Identity(scan, GenerationID(generationID))

	generatedAt := time.Now().UTC().Format(time.RFC3339Nano)
	storage := SourceV2Storage{
		SharedRoot:      scan.layout.sharedRoot,
		LaneRoot:        scan.layout.laneRoot,
		BaseManifestID:  manifests.base.ManifestID,
		DeltaManifestID: manifests.delta.ManifestID,
	}

	statusReceipt := SourceStatusReceipt{
		Contract:        SourceV2Contract,
		Kind:            sourceV2StatusKind,
		GeneratedAtUTC:  generatedAt,
		Repair:          sourceV2RepairCommand,
		SourceReadiness: readiness,
		VectorReadiness: unevaluatedVectorReadiness(),
		Storage:         storage,
	}

	err := writeCurrentSourceV2Receipt(
		scan.layout.statusPath(),
		statusReceipt,
	)
	if err != nil {
		return SourceSyncReceipt{}, err
	}

	return SourceSyncReceipt{
		Contract:        SourceV2Contract,
		Kind:            sourceV2SyncKind,
		GeneratedAtUTC:  generatedAt,
		Repair:          sourceV2RepairCommand,
		SourceReadiness: readiness,
		VectorReadiness: unevaluatedVectorReadiness(),
		Storage:         storage,
		Sync: SourceSyncSummary{
			FilesIndexed:     sourceV2IndexedCount(coverage),
			FilesFailed:      sourceV2FailedCount(coverage),
			FragmentsReused:  counters.fragmentsReused,
			FragmentsWritten: counters.fragmentsWritten,
		},
		Warnings: manifests.warnings,
	}, nil
}

func validateSourceV2ExternalExtractor(
	ctx context.Context,
	scan sourceV2Scan,
	external ExternalBatchExtractor,
) error {
	if !sourceV2InputsNeedExternalExtractor(scan.current) &&
		!sourceV2InputsNeedExternalExtractor(scan.head) {
		return nil
	}

	if external == nil {
		return ErrExternalExtractorRequired
	}

	err := external.Validate(ctx)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrExternalExtractorRequired, err)
	}

	return nil
}

func sourceV2InputsNeedExternalExtractor(inputs map[string]sourceV2Input) bool {
	for _, input := range inputs {
		if !input.descriptor.BuiltIn {
			return true
		}
	}

	return false
}

// SourceIndexStatus compares the persisted generation to current source bytes.
func SourceIndexStatus(ctx context.Context, root string) (SourceStatusReceipt, error) {
	scan, err := inspectSourceV2Repository(ctx, root, false)
	if err != nil {
		return SourceStatusReceipt{}, err
	}

	stored, err := loadCurrentSourceV2Receipt(scan.layout.statusPath())
	if errors.Is(err, os.ErrNotExist) {
		return missingSourceV2Status(scan), nil
	}

	if err != nil {
		return SourceStatusReceipt{}, err
	}

	stored.GeneratedAtUTC = time.Now().UTC().Format(time.RFC3339Nano)

	storedIdentity := stored.SourceReadiness.Identity
	if storedIdentity.RepositoryID != scan.layout.repositoryID ||
		storedIdentity.SourceSnapshotID != scan.sourceSnapshotID {
		stored.SourceReadiness.Status = SourceStatusStale
		stored.SourceReadiness.Reasons = []string{
			"persisted generation does not match current source bytes, " +
				"HEAD, config, or extractors",
		}
		stored.SourceReadiness.Coverage = staleSourceV2Coverage(scan.current, scan.excluded)

		return stored, nil
	}

	verifyErr := verifySourceV2Generation(scan.layout, stored)
	if verifyErr != nil {
		stored.SourceReadiness.Status = SourceStatusFailed
		stored.SourceReadiness.Reasons = []string{verifyErr.Error()}
	}

	return stored, nil
}

func inspectSourceV2Repository(
	ctx context.Context,
	root string,
	includeHead bool,
) (sourceV2Scan, error) {
	layout, err := resolveSourceV2Layout(ctx, root)
	if err != nil {
		return sourceV2Scan{}, err
	}

	options, err := LoadIndexOptions(layout.repositoryRoot)
	if err != nil {
		return sourceV2Scan{}, err
	}

	configHash, err := sourceV2ContentID("config", options)
	if err != nil {
		return sourceV2Scan{}, err
	}

	extractorHash, err := sourceV2ContentID("extractors", astfacts.LanguageDescriptors())
	if err != nil {
		return sourceV2Scan{}, err
	}

	headOID, err := sourceV2GitOutput(
		ctx,
		layout.repositoryRoot,
		"rev-parse",
		"--verify",
		"HEAD",
	)
	if err != nil {
		return sourceV2Scan{}, fmt.Errorf("resolve code-intel HEAD: %w", err)
	}

	current, excluded, err := currentSourceV2Inputs(ctx, layout.repositoryRoot, options)
	if err != nil {
		return sourceV2Scan{}, err
	}

	head := map[string]sourceV2Input{}
	if includeHead {
		head, err = headSourceV2Inputs(ctx, layout.repositoryRoot, options)
		if err != nil {
			return sourceV2Scan{}, err
		}
	}

	fingerprint, err := sourceV2WorktreeFingerprint(current)
	if err != nil {
		return sourceV2Scan{}, err
	}

	snapshotID := SourceSnapshotID(sourceV2Digest(
		"snapshot",
		string(layout.repositoryID)+"\x00"+headOID+"\x00"+fingerprint+"\x00"+
			configHash+"\x00"+extractorHash,
	))

	return sourceV2Scan{
		layout:              layout,
		current:             current,
		head:                head,
		excluded:            excluded,
		headOID:             headOID,
		configHash:          configHash,
		extractorSetHash:    extractorHash,
		worktreeFingerprint: fingerprint,
		sourceSnapshotID:    snapshotID,
	}, nil
}

func currentSourceV2Inputs(
	ctx context.Context,
	root string,
	options IndexOptions,
) (map[string]sourceV2Input, map[string]int, error) {
	walker := sourceV2CurrentWalker{
		root:          root,
		options:       options,
		ignoreMatcher: newGitIgnoreMatcher(ctx, root),
		inputs:        map[string]sourceV2Input{},
		excluded:      map[string]int{},
	}

	err := filepath.WalkDir(
		root,
		func(path string, entry os.DirEntry, walkErr error) error {
			return walker.visit(ctx, path, entry, walkErr)
		},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("walk code-intel v2 source tree: %w", err)
	}

	return walker.inputs, walker.excluded, nil
}

type sourceV2CurrentWalker struct {
	ignoreMatcher gitIgnoreMatcher
	inputs        map[string]sourceV2Input
	excluded      map[string]int
	root          string
	options       IndexOptions
}

func (walker sourceV2CurrentWalker) visit(
	ctx context.Context,
	path string,
	entry os.DirEntry,
	walkErr error,
) error {
	ctxErr := ctx.Err()
	if ctxErr != nil {
		return fmt.Errorf("scan code-intel source tree: %w", ctxErr)
	}

	if walkErr != nil {
		return fmt.Errorf("visit code-intel source path: %w", walkErr)
	}

	relative, err := filepath.Rel(walker.root, path)
	if err != nil {
		return fmt.Errorf("resolve code-intel source path: %w", err)
	}

	relative = filepath.ToSlash(relative)
	if entry.IsDir() {
		return walker.visitDirectory(ctx, path, relative)
	}

	return walker.visitFile(ctx, path, relative, entry)
}

func (walker sourceV2CurrentWalker) visitDirectory(
	ctx context.Context,
	path string,
	relative string,
) error {
	if relative != "." && (pathHasSkippedDir(relative) ||
		excludedByConfig(relative, walker.options.ExcludePatterns) ||
		walker.ignoreMatcher.ignoredDir(ctx, path)) {
		return filepath.SkipDir
	}

	return nil
}

func (walker sourceV2CurrentWalker) visitFile(
	ctx context.Context,
	path string,
	relative string,
	entry os.DirEntry,
) error {
	descriptor, recognized := astfacts.SourceLanguageForPath(relative)
	if !recognized {
		return nil
	}

	if pathHasSkippedDir(relative) ||
		excludedByConfig(relative, walker.options.ExcludePatterns) ||
		walker.ignoreMatcher.ignoredFile(ctx, path) {
		walker.excluded[descriptor.Language]++

		return nil
	}

	info, err := entry.Info()
	if err != nil {
		return fmt.Errorf("inspect code-intel source path: %w", err)
	}

	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		walker.excluded[descriptor.Language]++

		return nil
	}

	input, err := sourceV2InputFromBytes(path, relative, descriptor, nil)
	if err != nil {
		return err
	}

	walker.inputs[relative] = input

	return nil
}

func headSourceV2Inputs(
	ctx context.Context,
	root string,
	options IndexOptions,
) (map[string]sourceV2Input, error) {
	command := realgit.Command(ctx, false, "ls-tree", "-r", "-z", "HEAD")
	command.Dir = root
	command.Env = realgit.CleanGitLocalEnv(os.Environ())

	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("list HEAD source files: %w", err)
	}

	inputs := map[string]sourceV2Input{}

	for rawPath := range bytes.SplitSeq(output, []byte{0}) {
		ctxErr := ctx.Err()
		if ctxErr != nil {
			return nil, fmt.Errorf("scan HEAD source tree: %w", ctxErr)
		}

		if len(rawPath) == 0 {
			continue
		}

		input, included, inputErr := headSourceV2Input(ctx, root, options, rawPath)
		if inputErr != nil {
			return nil, inputErr
		}

		if included {
			inputs[input.path] = input
		}
	}

	return inputs, nil
}

func headSourceV2Input(
	ctx context.Context,
	root string,
	options IndexOptions,
	rawPath []byte,
) (sourceV2Input, bool, error) {
	metadata, rawName, found := bytes.Cut(rawPath, []byte{'\t'})
	if !found {
		return sourceV2Input{}, false, fmt.Errorf(
			"%w: invalid Git tree entry %q",
			errInvalidSourceV2Input,
			rawPath,
		)
	}

	fields := strings.Fields(string(metadata))
	if len(fields) != sourceV2GitTreeMetadataFields {
		return sourceV2Input{}, false, fmt.Errorf(
			"%w: invalid Git tree metadata %q",
			errInvalidSourceV2Input,
			metadata,
		)
	}

	if fields[0] != "100644" && fields[0] != "100755" {
		return sourceV2Input{}, false, nil
	}

	path := filepath.ToSlash(string(rawName))

	descriptor, recognized := astfacts.SourceLanguageForPath(path)
	if !recognized || pathHasSkippedDir(path) ||
		excludedByConfig(path, options.ExcludePatterns) {
		return sourceV2Input{}, false, nil
	}

	contents, err := sourceV2GitBytes(ctx, root, "cat-file", "blob", fields[2])
	if err != nil {
		return sourceV2Input{}, false, fmt.Errorf("read HEAD source %q: %w", path, err)
	}

	input, err := sourceV2InputFromBytes("", path, descriptor, contents)
	if err != nil {
		return sourceV2Input{}, false, err
	}

	return input, true, nil
}

func sourceV2GitBytes(
	ctx context.Context,
	root string,
	arguments ...string,
) ([]byte, error) {
	command := realgit.Command(ctx, false, arguments...)
	command.Dir = root
	command.Env = realgit.CleanGitLocalEnv(os.Environ())

	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("run Git %s: %w", strings.Join(arguments, " "), err)
	}

	return output, nil
}

func sourceV2InputFromBytes(
	absPath string,
	path string,
	descriptor astfacts.LanguageDescriptor,
	contents []byte,
) (sourceV2Input, error) {
	if contents == nil {
		var err error

		contents, err = os.ReadFile(absPath)
		if err != nil {
			return sourceV2Input{}, fmt.Errorf("read code-intel v2 source %q: %w", path, err)
		}
	}

	return sourceV2Input{
		descriptor:  descriptor,
		path:        path,
		contentHash: astfacts.ContentHash(contents),
		contents:    contents,
		oversized: len(contents) > maxIndexedSourceBytes ||
			astfacts.LineCount(contents) > maxIndexedSourceLines,
	}, nil
}

func splitSourceV2Inputs(
	head map[string]sourceV2Input,
	current map[string]sourceV2Input,
) ([]sourceV2Input, []sourceV2Input, []string) {
	base := sourceV2SortedInputs(head)
	delta := []sourceV2Input{}
	tombstones := []string{}

	for path, input := range current {
		headInput, exists := head[path]

		extractorChanged := headInput.descriptor.Extractor.Fingerprint !=
			input.descriptor.Extractor.Fingerprint
		if !exists || headInput.contentHash != input.contentHash || extractorChanged {
			delta = append(delta, input)
		}
	}

	for path := range head {
		if _, exists := current[path]; !exists {
			tombstones = append(tombstones, path)
		}
	}

	slices.SortFunc(delta, compareSourceV2Input)
	slices.Sort(tombstones)

	return base, delta, tombstones
}

func sourceV2SortedInputs(inputs map[string]sourceV2Input) []sourceV2Input {
	ordered := make([]sourceV2Input, 0, len(inputs))
	for _, input := range inputs {
		ordered = append(ordered, input)
	}

	slices.SortFunc(ordered, compareSourceV2Input)

	return ordered
}

func compareSourceV2Input(left, right sourceV2Input) int {
	return strings.Compare(left.path, right.path)
}

func buildSourceV2Entries(
	ctx context.Context,
	layout sourceV2Layout,
	inputs []sourceV2Input,
	external ExternalBatchExtractor,
	knownFragments map[string]string,
	counters *sourceV2BuildCounters,
) ([]sourceV2ManifestEntry, []string, error) {
	entries := make([]sourceV2ManifestEntry, 0, len(inputs))
	pendingExternal := []sourceV2Input{}

	for _, input := range inputs {
		entry, pending, err := buildSourceV2Entry(
			layout,
			input,
			external,
			knownFragments,
			counters,
		)
		if err != nil {
			return nil, nil, err
		}

		entries = append(entries, entry)
		if pending {
			pendingExternal = append(pendingExternal, input)
		}
	}

	warnings, err := completeExternalSourceV2Entries(
		ctx,
		layout,
		external,
		pendingExternal,
		entries,
		knownFragments,
		counters,
	)
	if err != nil {
		return nil, nil, err
	}

	slices.SortFunc(entries, func(left, right sourceV2ManifestEntry) int {
		return strings.Compare(left.Path, right.Path)
	})

	return entries, warnings, nil
}

func buildSourceV2Entry(
	layout sourceV2Layout,
	input sourceV2Input,
	external ExternalBatchExtractor,
	knownFragments map[string]string,
	counters *sourceV2BuildCounters,
) (sourceV2ManifestEntry, bool, error) {
	entry := sourceV2Entry(input)
	if input.oversized {
		entry.Status = "oversized"

		return entry, false, nil
	}

	cacheKey := sourceV2InputCacheKey(input)

	entry.FragmentID = knownFragments[cacheKey]
	if entry.FragmentID != "" &&
		sourceV2FragmentReady(layout.fragmentPath(entry.FragmentID), entry) {
		entry.Status = sourceV2EntryStatusIndexed
		counters.fragmentsReused++
		counters.cacheHits[cacheKey] = true

		return entry, false, nil
	}

	if !input.descriptor.BuiltIn {
		if external == nil {
			return sourceV2ManifestEntry{}, false, ErrExternalExtractorRequired
		}

		if !utf8.Valid(input.contents) {
			entry.FragmentID = ""
			entry.Status = SourceStatusFailed

			return entry, false, nil
		}

		return entry, true, nil
	}

	fragment, err := builtInSourceV2Fragment(input, entry.FragmentID)
	if err != nil {
		return failedSourceV2Entry(entry)
	}

	err = publishSourceV2Fragment(
		layout,
		input,
		&entry,
		fragment,
		knownFragments,
		counters,
	)
	if err != nil {
		return sourceV2ManifestEntry{}, false, err
	}

	return entry, false, nil
}

func failedSourceV2Entry(
	entry sourceV2ManifestEntry,
) (sourceV2ManifestEntry, bool, error) {
	entry.FragmentID = ""
	entry.Status = SourceStatusFailed

	return entry, false, nil
}

func completeExternalSourceV2Entries(
	ctx context.Context,
	layout sourceV2Layout,
	external ExternalBatchExtractor,
	inputs []sourceV2Input,
	entries []sourceV2ManifestEntry,
	knownFragments map[string]string,
	counters *sourceV2BuildCounters,
) ([]string, error) {
	if len(inputs) == 0 {
		return nil, nil
	}

	results, err := extractSourceV2Batch(ctx, external, inputs)
	if err != nil {
		return nil, err
	}

	warnings := []string{}

	for index := range entries {
		entry := &entries[index]

		result, exists := results[entry.Path]
		if !exists {
			continue
		}

		if result.Status == externalExtractorStatusError {
			entry.FragmentID = ""
			entry.Status = SourceStatusFailed
			warnings = append(
				warnings,
				"PurRDF extraction failed for "+entry.Path+": "+result.Error,
			)

			continue
		}

		input := sourceV2InputByPath(inputs, entry.Path)
		fragment := externalSourceV2Fragment(input, "", result)

		err = publishSourceV2Fragment(
			layout,
			input,
			entry,
			fragment,
			knownFragments,
			counters,
		)
		if err != nil {
			return nil, err
		}
	}

	return warnings, nil
}

func publishSourceV2Fragment(
	layout sourceV2Layout,
	input sourceV2Input,
	entry *sourceV2ManifestEntry,
	fragment sourceV2Fragment,
	knownFragments map[string]string,
	counters *sourceV2BuildCounters,
) error {
	fragmentID, err := sourceV2FragmentContentID(fragment)
	if err != nil {
		return err
	}

	entry.FragmentID = fragmentID
	fragment.FragmentID = fragmentID

	written, err := writeImmutableSourceV2JSON(
		layout.fragmentPath(fragmentID),
		fragment,
	)
	if err != nil {
		return err
	}

	if written {
		counters.fragmentsWritten++
	} else {
		counters.fragmentsReused++
	}

	entry.Status = sourceV2EntryStatusIndexed
	knownFragments[sourceV2InputCacheKey(input)] = fragmentID

	return nil
}

func sourceV2Entry(input sourceV2Input) sourceV2ManifestEntry {
	return sourceV2ManifestEntry{
		Path:                 input.path,
		Language:             input.descriptor.Language,
		ContentSHA256:        input.contentHash,
		ExtractorFingerprint: input.descriptor.Extractor.Fingerprint,
	}
}

func sourceV2InputCacheKey(input sourceV2Input) string {
	pathComponent := ""
	if !input.descriptor.BuiltIn {
		pathComponent = input.path
	}

	return input.descriptor.Language + "\x00" + input.descriptor.Variant + "\x00" +
		input.descriptor.Extractor.Fingerprint + "\x00" + input.contentHash + "\x00" +
		pathComponent
}

func knownSourceV2Fragments(scan sourceV2Scan) map[string]string {
	known := map[string]string{}
	addSharedSourceV2BaseFragments(scan, known)

	receipt, err := loadCurrentSourceV2Receipt(scan.layout.statusPath())
	if err != nil {
		return known
	}

	base, err := loadSourceV2BaseManifest(scan.layout, receipt.Storage.BaseManifestID)
	if err != nil {
		return known
	}

	delta, err := loadSourceV2DeltaManifest(scan.layout, receipt.Storage.DeltaManifestID)
	if err != nil {
		return known
	}

	addKnownSourceV2Entries(scan.layout, known, base.Entries)
	addKnownSourceV2Entries(scan.layout, known, delta.Entries)

	return known
}

func addSharedSourceV2BaseFragments(
	scan sourceV2Scan,
	known map[string]string,
) {
	// Reuse the immutable HEAD base produced by another lane when its exact
	// repository/config/extractor inputs match this scan. Cache discovery is an
	// optimization, so malformed or stale candidates are ignored here and are
	// still rejected when referenced by a current receipt.
	baseDirectory := filepath.Join(scan.layout.sharedRoot, "bases")

	baseFiles, readErr := os.ReadDir(baseDirectory)
	if readErr != nil {
		baseFiles = nil
	}

	for _, file := range baseFiles {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}

		digest := strings.TrimSuffix(file.Name(), ".json")

		manifest, err := loadSourceV2BaseManifest(
			scan.layout,
			"base:sha256:"+digest,
		)
		if err != nil || manifest.RepositoryID != scan.layout.repositoryID ||
			manifest.HeadOID != scan.headOID || manifest.ConfigHash != scan.configHash ||
			manifest.ExtractorSetHash != scan.extractorSetHash {
			continue
		}

		addKnownSourceV2Entries(scan.layout, known, manifest.Entries)
	}
}

func addKnownSourceV2Entries(
	layout sourceV2Layout,
	known map[string]string,
	entries []sourceV2ManifestEntry,
) {
	for _, entry := range entries {
		if entry.Status != sourceV2EntryStatusIndexed ||
			!sourceV2FragmentReady(layout.fragmentPath(entry.FragmentID), entry) {
			continue
		}

		descriptor, ok := astfacts.SourceLanguageForPath(entry.Path)
		if !ok || descriptor.Language != entry.Language ||
			descriptor.Extractor.Fingerprint != entry.ExtractorFingerprint {
			continue
		}

		input := sourceV2Input{
			descriptor:  descriptor,
			path:        entry.Path,
			contentHash: entry.ContentSHA256,
		}
		known[sourceV2InputCacheKey(input)] = entry.FragmentID
	}
}

func sourceV2FragmentReady(path string, entry sourceV2ManifestEntry) bool {
	payload, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	var fragment sourceV2Fragment
	if json.Unmarshal(payload, &fragment) != nil {
		return false
	}

	computedID, err := sourceV2FragmentContentID(fragment)
	if err != nil {
		return false
	}

	return fragment.Contract == SourceV2Contract &&
		fragment.Kind == sourceV2FragmentKind &&
		fragment.FragmentID == entry.FragmentID &&
		computedID == entry.FragmentID &&
		fragment.ContentSHA256 == entry.ContentSHA256 &&
		fragment.Extractor.Fingerprint == entry.ExtractorFingerprint
}

func sourceV2FragmentContentID(fragment sourceV2Fragment) (string, error) {
	fragment.FragmentID = ""

	return sourceV2ContentID("fragment", fragment)
}

func builtInSourceV2Fragment(
	input sourceV2Input,
	fragmentID string,
) (sourceV2Fragment, error) {
	file, supported, err := astfacts.Analyze(input.path, input.contents)
	if err != nil {
		return sourceV2Fragment{}, fmt.Errorf(
			"analyze code-intel source %q: %w",
			input.path,
			err,
		)
	}

	if !supported {
		return sourceV2Fragment{}, fmt.Errorf(
			"%w: no built-in extractor for %q",
			errInvalidSourceV2Input,
			input.path,
		)
	}

	if file.HasParseError {
		return sourceV2Fragment{}, fmt.Errorf(
			"%w: parser reported an error for %q",
			errInvalidSourceV2Input,
			input.path,
		)
	}

	fragment := sourceV2Fragment{
		Contract:      SourceV2Contract,
		Kind:          sourceV2FragmentKind,
		FragmentID:    fragmentID,
		Language:      input.descriptor.Language,
		ContentSHA256: input.contentHash,
		Extractor:     input.descriptor.Extractor,
		LineCount:     file.LineCount,
		Symbols:       make([]SourceSymbolFact, 0, len(file.Symbols)),
		Imports:       make([]SourceImportFact, 0, len(file.Imports)),
	}
	for _, symbol := range file.Symbols {
		fragment.Symbols = append(fragment.Symbols, SourceSymbolFact{
			SymbolKind:      symbol.SymbolKind,
			SymbolName:      symbol.SymbolName,
			SymbolPath:      symbol.SymbolPath,
			NodeKind:        symbol.NodeKind,
			ReferencedNames: slices.Clone(symbol.ReferencedNames),
			CallNames:       slices.Clone(symbol.CallNames),
			BaseNames:       slices.Clone(symbol.BaseNames),
			StartByte:       symbol.StartByte,
			EndByte:         symbol.EndByte,
			StartLine:       symbol.StartLine,
			EndLine:         symbol.EndLine,
			Confidence:      "deterministic",
			Provenance: SourceFactProvenance{
				Class:          "EXTRACTED",
				Parser:         input.descriptor.Extractor.Name,
				ParserRevision: input.descriptor.Extractor.Fingerprint,
				SpanFidelity:   "exact",
			},
		})
	}

	for _, imported := range file.Imports {
		fragment.Imports = append(fragment.Imports, SourceImportFact{
			Target:     imported.Target,
			RawText:    imported.RawText,
			Confidence: "deterministic",
			Provenance: SourceFactProvenance{
				Class:          "EXTRACTED",
				Parser:         input.descriptor.Extractor.Name,
				ParserRevision: input.descriptor.Extractor.Fingerprint,
				SpanFidelity:   "none",
			},
		})
	}

	return fragment, nil
}

func extractSourceV2Batch(
	ctx context.Context,
	external ExternalBatchExtractor,
	inputs []sourceV2Input,
) (map[string]ExternalExtractorResult, error) {
	files := make([]ExternalExtractorRequestFile, 0, len(inputs))
	for _, input := range inputs {
		files = append(files, ExternalExtractorRequestFile{
			Path:          input.path,
			ContentSHA256: input.contentHash,
			Content:       string(input.contents),
		})
	}

	response, err := external.Extract(ctx, files)
	if err != nil {
		return nil, fmt.Errorf("extract PurRDF code-intel facts: %w", err)
	}

	results := make(map[string]ExternalExtractorResult, len(response.Results))
	for _, result := range response.Results {
		input := sourceV2InputByPath(inputs, result.Path)
		if input.path == "" {
			return nil, fmt.Errorf(
				"%w: external extractor returned unknown path %q",
				errInvalidSourceV2Input,
				result.Path,
			)
		}

		if result.Language != input.descriptor.Language {
			return nil, fmt.Errorf(
				"%w: external extractor language mismatch for %q: %q",
				errInvalidSourceV2Input,
				result.Path,
				result.Language,
			)
		}

		results[result.Path] = result
	}

	return results, nil
}

func sourceV2InputByPath(inputs []sourceV2Input, path string) sourceV2Input {
	for _, input := range inputs {
		if input.path == path {
			return input
		}
	}

	return sourceV2Input{}
}

func externalSourceV2Fragment(
	input sourceV2Input,
	fragmentID string,
	result ExternalExtractorResult,
) sourceV2Fragment {
	facts := slices.Clone(result.Facts)
	slices.SortFunc(facts, func(left, right ExternalExtractorFact) int {
		return strings.Compare(left.ID, right.ID)
	})

	return sourceV2Fragment{
		Contract:      SourceV2Contract,
		Kind:          sourceV2FragmentKind,
		FragmentID:    fragmentID,
		Language:      input.descriptor.Language,
		ContentSHA256: input.contentHash,
		Extractor:     input.descriptor.Extractor,
		LineCount:     astfacts.LineCount(input.contents),
		Facts:         facts,
	}
}

func sourceV2ManifestID(
	namespace string,
	manifest sourceV2BaseManifest,
) (string, error) {
	manifest.ManifestID = ""

	return sourceV2ContentID(namespace, manifest)
}

func sourceV2DeltaManifestID(manifest sourceV2DeltaManifest) (string, error) {
	manifest.ManifestID = ""

	return sourceV2ContentID("delta", manifest)
}

func mergeSourceV2Entries(
	base []sourceV2ManifestEntry,
	delta []sourceV2ManifestEntry,
	tombstones []string,
) map[string]sourceV2ManifestEntry {
	merged := make(map[string]sourceV2ManifestEntry, len(base)+len(delta))
	for _, entry := range base {
		merged[entry.Path] = entry
	}

	for _, path := range tombstones {
		delete(merged, path)
	}

	for _, entry := range delta {
		merged[entry.Path] = entry
	}

	return merged
}

func sourceV2Coverage(
	current map[string]sourceV2Input,
	entries map[string]sourceV2ManifestEntry,
	excluded map[string]int,
	cacheHits map[string]bool,
) []LanguageCoverage {
	coverage := sourceV2CoverageMap(excluded)
	for path, input := range current {
		item := coverage[input.descriptor.Language]

		item.Eligible++
		if cacheHits[sourceV2InputCacheKey(input)] {
			item.CacheHits++
		}

		entry := entries[path]
		switch entry.Status {
		case sourceV2EntryStatusIndexed:
			item.Indexed++
		case "unsupported":
			item.Unsupported++
		case "oversized":
			item.Oversized++
		default:
			item.Failed++
		}

		coverage[input.descriptor.Language] = item
	}

	return sourceV2SortedCoverage(coverage)
}

func staleSourceV2Coverage(
	current map[string]sourceV2Input,
	excluded map[string]int,
) []LanguageCoverage {
	coverage := sourceV2CoverageMap(excluded)
	for _, input := range current {
		item := coverage[input.descriptor.Language]
		item.Eligible++
		item.Stale++
		coverage[input.descriptor.Language] = item
	}

	return sourceV2SortedCoverage(coverage)
}

func sourceV2CoverageMap(excluded map[string]int) map[string]LanguageCoverage {
	coverage := map[string]LanguageCoverage{}
	for _, descriptor := range astfacts.LanguageDescriptors() {
		coverage[descriptor.Language] = LanguageCoverage{
			Language: descriptor.Language,
			Excluded: excluded[descriptor.Language],
		}
	}

	return coverage
}

func sourceV2SortedCoverage(coverage map[string]LanguageCoverage) []LanguageCoverage {
	result := make([]LanguageCoverage, 0, len(coverage))
	for _, item := range coverage {
		result = append(result, item)
	}

	slices.SortFunc(result, func(left, right LanguageCoverage) int {
		return strings.Compare(left.Language, right.Language)
	})

	return result
}

func sourceV2Readiness(coverage []LanguageCoverage) SourceReadiness {
	readiness := SourceReadiness{Coverage: coverage, Status: SourceStatusExact}
	eligible := 0
	indexed := 0
	failed := 0

	for _, item := range coverage {
		eligible += item.Eligible
		indexed += item.Indexed

		failed += item.Failed
		if item.Unsupported > 0 {
			readiness.Reasons = append(
				readiness.Reasons,
				item.Language+": extractor unavailable",
			)
		}

		if item.Oversized > 0 {
			readiness.Reasons = append(
				readiness.Reasons,
				item.Language+": oversized source files",
			)
		}

		if item.Failed > 0 {
			readiness.Reasons = append(readiness.Reasons, item.Language+": extraction failures")
		}
	}

	if indexed != eligible {
		readiness.Status = SourceStatusPartial
	}

	if eligible > 0 && indexed == 0 && failed > 0 {
		readiness.Status = SourceStatusFailed
	}

	return readiness
}

func sourceV2Identity(scan sourceV2Scan, generationID GenerationID) SourceIdentity {
	return SourceIdentity{
		RepositoryID:        scan.layout.repositoryID,
		SourceSnapshotID:    scan.sourceSnapshotID,
		GenerationID:        generationID,
		HeadOID:             scan.headOID,
		WorktreeID:          scan.layout.worktreeID,
		WorktreeFingerprint: scan.worktreeFingerprint,
		ConfigHash:          scan.configHash,
		ExtractorSetHash:    scan.extractorSetHash,
	}
}

func sourceV2WorktreeFingerprint(inputs map[string]sourceV2Input) (string, error) {
	type fingerprintEntry struct {
		Path                 string `json:"path"`
		ContentSHA256        string `json:"content_sha256"`
		ExtractorFingerprint string `json:"extractor_fingerprint"`
	}

	entries := make([]fingerprintEntry, 0, len(inputs))
	for _, input := range inputs {
		entries = append(entries, fingerprintEntry{
			Path:                 input.path,
			ContentSHA256:        input.contentHash,
			ExtractorFingerprint: input.descriptor.Extractor.Fingerprint,
		})
	}

	slices.SortFunc(entries, func(left, right fingerprintEntry) int {
		return strings.Compare(left.Path, right.Path)
	})

	return sourceV2ContentID("worktree-content", entries)
}

func missingSourceV2Status(scan sourceV2Scan) SourceStatusReceipt {
	coverage := staleSourceV2Coverage(scan.current, scan.excluded)
	for index := range coverage {
		coverage[index].Stale = 0
	}

	return SourceStatusReceipt{
		Contract:       SourceV2Contract,
		Kind:           sourceV2StatusKind,
		GeneratedAtUTC: time.Now().UTC().Format(time.RFC3339Nano),
		Repair:         sourceV2RepairCommand,
		SourceReadiness: SourceReadiness{
			Identity: sourceV2Identity(scan, ""),
			Coverage: coverage,
			Reasons:  []string{"no code-intel v2 generation exists for this worktree"},
			Status:   SourceStatusMissing,
		},
		VectorReadiness: unevaluatedVectorReadiness(),
		Storage: SourceV2Storage{
			SharedRoot: scan.layout.sharedRoot,
			LaneRoot:   scan.layout.laneRoot,
		},
	}
}

func unevaluatedVectorReadiness() VectorReadiness {
	return VectorReadiness{Status: VectorStatusNotEvaluated}
}

func sourceV2IndexedCount(coverage []LanguageCoverage) int {
	total := 0
	for _, item := range coverage {
		total += item.Indexed
	}

	return total
}

func sourceV2FailedCount(coverage []LanguageCoverage) int {
	total := 0
	for _, item := range coverage {
		total += item.Failed + item.Unsupported + item.Oversized
	}

	return total
}
