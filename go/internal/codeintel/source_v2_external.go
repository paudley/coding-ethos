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
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/astfacts"
	"blackcat.ca/coding-ethos/go/internal/safeexec"
)

const (
	externalExtractorParserRDF                = "purrdf-rdf"
	externalExtractorParserSPARQL             = "purrdf-sparql-algebra"
	externalExtractorSpanFidelityNone         = "none"
	externalExtractorSpanFidelitySubjectStart = "subject_start"
	externalExtractorStatusError              = "error"
	externalExtractorStatusOK                 = "ok"
	purrdfExtractorBinary                     = "coding-ethos-purrdf-extractor"
)

var errExternalExtractorUnavailable = errors.New(
	"PurRDF code-intel extractor is unavailable",
)

var errInvalidExternalExtractorResponse = errors.New(
	"invalid PurRDF code-intel extractor response",
)

// ErrExternalExtractorRequired means an eligible RDF or SPARQL document was
// discovered but the pinned PurRDF extractor could not be used. Callers must
// not turn this into partial coverage or silently select another parser.
var ErrExternalExtractorRequired = errors.New(
	"pinned PurRDF code-intel extractor is required",
)

// ExternalBatchExtractor is the process-neutral boundary for semantic extractors.
type ExternalBatchExtractor interface {
	Validate(ctx context.Context) error
	Extract(
		ctx context.Context,
		files []ExternalExtractorRequestFile,
	) (ExternalExtractorResponse, error)
}

// CommandBatchExtractor invokes one trusted, one-shot extractor process.
type CommandBatchExtractor struct {
	executable string
}

// NewCommandBatchExtractor constructs an adapter for a previously resolved binary.
func NewCommandBatchExtractor(executable string) (CommandBatchExtractor, error) {
	executable = strings.TrimSpace(executable)
	if executable == "" {
		return CommandBatchExtractor{}, errExternalExtractorUnavailable
	}

	resolved, err := exec.LookPath(executable)
	if err != nil {
		return CommandBatchExtractor{}, fmt.Errorf(
			"%w: %s",
			errExternalExtractorUnavailable,
			executable,
		)
	}

	return CommandBatchExtractor{executable: resolved}, nil
}

// DefaultExternalBatchExtractor resolves the helper beside the current
// coding-ethos executable first, then through PATH. This follows the existing
// binary bootstrap model.
func DefaultExternalBatchExtractor() (CommandBatchExtractor, error) {
	executable, executableErr := os.Executable()
	if executableErr == nil {
		candidate := filepath.Join(filepath.Dir(executable), purrdfExtractorBinary)

		extractor, resolveErr := NewCommandBatchExtractor(candidate)
		if resolveErr == nil {
			return extractor, nil
		}
	}

	return NewCommandBatchExtractor(purrdfExtractorBinary)
}

// Validate performs an empty protocol exchange so a cached generation still
// proves that the executable is the exact pinned PurRDF helper.
func (extractor CommandBatchExtractor) Validate(ctx context.Context) error {
	_, err := extractor.Extract(ctx, []ExternalExtractorRequestFile{})
	if err != nil {
		return fmt.Errorf("validate PurRDF extractor identity: %w", err)
	}

	return nil
}

// Extract sends one deterministic request and validates the complete response envelope.
func (extractor CommandBatchExtractor) Extract(
	ctx context.Context,
	files []ExternalExtractorRequestFile,
) (ExternalExtractorResponse, error) {
	orderedFiles := slices.Clone(files)
	slices.SortFunc(orderedFiles, func(left, right ExternalExtractorRequestFile) int {
		return strings.Compare(left.Path, right.Path)
	})
	request := ExternalExtractorRequest{
		Protocol:  astfacts.PurrdfExtractorProtocol,
		RequestID: externalExtractorRequestID(orderedFiles),
		Files:     orderedFiles,
	}

	payload, err := json.Marshal(request)
	if err != nil {
		return ExternalExtractorResponse{}, fmt.Errorf(
			"marshal external extractor request: %w",
			err,
		)
	}

	command := safeexec.CommandContext(ctx, extractor.executable)
	command.Stdin = bytes.NewReader(payload)

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)

	command.Stdout = &stdout
	command.Stderr = &stderr

	err = command.Run()
	if err != nil {
		return ExternalExtractorResponse{}, fmt.Errorf(
			"run PurRDF extractor: %w: %s",
			err,
			strings.TrimSpace(stderr.String()),
		)
	}

	var response ExternalExtractorResponse

	err = json.Unmarshal(stdout.Bytes(), &response)
	if err != nil {
		return ExternalExtractorResponse{}, fmt.Errorf(
			"decode PurRDF extractor response: %w",
			err,
		)
	}

	err = validateExternalExtractorResponse(request, response)
	if err != nil {
		return ExternalExtractorResponse{}, err
	}

	slices.SortFunc(response.Results, func(left, right ExternalExtractorResult) int {
		return strings.Compare(left.Path, right.Path)
	})

	for index := range response.Results {
		slices.SortFunc(
			response.Results[index].Facts,
			func(left, right ExternalExtractorFact) int {
				return strings.Compare(left.ID, right.ID)
			},
		)
	}

	return response, nil
}

func externalExtractorRequestID(files []ExternalExtractorRequestFile) string {
	ordered := slices.Clone(files)
	slices.SortFunc(ordered, func(left, right ExternalExtractorRequestFile) int {
		return strings.Compare(left.Path, right.Path)
	})

	hash := sha256.New()

	_, _ = hash.Write([]byte(astfacts.PurrdfExtractorProtocol))
	for _, file := range ordered {
		_, _ = hash.Write([]byte(
			"\x00" + file.Path + "\x00" + file.ContentSHA256 + "\x00" + file.BaseIRI,
		))
	}

	return "request:sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func validateExternalExtractorResponse(
	request ExternalExtractorRequest,
	response ExternalExtractorResponse,
) error {
	err := validateExternalExtractorEnvelope(request, response)
	if err != nil {
		return err
	}

	wanted, err := externalExtractorWantedFiles(request.Files)
	if err != nil {
		return err
	}

	seen := make(map[string]bool, len(response.Results))
	for _, result := range response.Results {
		err = validateExternalExtractorResult(result, wanted, seen)
		if err != nil {
			return err
		}
	}

	if len(seen) != len(wanted) {
		return fmt.Errorf(
			"%w: external extractor returned %d of %d requested files",
			errInvalidExternalExtractorResponse,
			len(seen),
			len(wanted),
		)
	}

	return nil
}

func validateExternalExtractorEnvelope(
	request ExternalExtractorRequest,
	response ExternalExtractorResponse,
) error {
	if response.Protocol != astfacts.PurrdfExtractorProtocol {
		return fmt.Errorf(
			"%w: unexpected external extractor protocol %q",
			errInvalidExternalExtractorResponse,
			response.Protocol,
		)
	}

	if response.RequestID != request.RequestID {
		return fmt.Errorf(
			"%w: external extractor request ID mismatch: %q",
			errInvalidExternalExtractorResponse,
			response.RequestID,
		)
	}

	if response.Extractor.Name != astfacts.PurrdfExtractorName ||
		response.Extractor.Version != "1" ||
		response.Extractor.PurrdfRevision != astfacts.PurrdfExtractorRevision {
		return fmt.Errorf(
			"%w: unexpected PurRDF extractor identity: %+v",
			errInvalidExternalExtractorResponse,
			response.Extractor,
		)
	}

	return nil
}

func externalExtractorWantedFiles(
	files []ExternalExtractorRequestFile,
) (map[string]string, error) {
	wanted := make(map[string]string, len(files))
	for _, file := range files {
		if _, exists := wanted[file.Path]; exists {
			return nil, fmt.Errorf(
				"%w: duplicate external extractor request path %q",
				errInvalidExternalExtractorResponse,
				file.Path,
			)
		}

		wanted[file.Path] = file.ContentSHA256
	}

	return wanted, nil
}

func validateExternalExtractorResult(
	result ExternalExtractorResult,
	wanted map[string]string,
	seen map[string]bool,
) error {
	contentHash, exists := wanted[result.Path]
	if !exists {
		return fmt.Errorf(
			"%w: unexpected external extractor result path %q",
			errInvalidExternalExtractorResponse,
			result.Path,
		)
	}

	if seen[result.Path] {
		return fmt.Errorf(
			"%w: duplicate external extractor result path %q",
			errInvalidExternalExtractorResponse,
			result.Path,
		)
	}

	seen[result.Path] = true
	if result.ContentSHA256 != contentHash {
		return fmt.Errorf(
			"%w: external extractor content hash mismatch for %q",
			errInvalidExternalExtractorResponse,
			result.Path,
		)
	}

	if result.Status != externalExtractorStatusOK &&
		result.Status != externalExtractorStatusError {
		return fmt.Errorf(
			"%w: invalid external extractor status %q for %q",
			errInvalidExternalExtractorResponse,
			result.Status,
			result.Path,
		)
	}

	if result.Status == externalExtractorStatusError &&
		strings.TrimSpace(result.Error) == "" {
		return fmt.Errorf(
			"%w: external extractor omitted error for %q",
			errInvalidExternalExtractorResponse,
			result.Path,
		)
	}

	if result.Status == externalExtractorStatusOK &&
		strings.TrimSpace(result.DocumentKind) == "" {
		return fmt.Errorf(
			"%w: external extractor omitted document kind for %q",
			errInvalidExternalExtractorResponse,
			result.Path,
		)
	}

	return validateExternalExtractorFacts(result)
}

func externalFactContract(language string) (string, string, error) {
	switch language {
	case astfacts.LanguageTurtle,
		astfacts.LanguageTriG,
		astfacts.LanguageNTriples,
		astfacts.LanguageNQuads:
		return externalExtractorParserRDF,
			externalExtractorSpanFidelitySubjectStart,
			nil
	case astfacts.LanguageSPARQL:
		return externalExtractorParserSPARQL, externalExtractorSpanFidelityNone, nil
	default:
		return "", "", fmt.Errorf(
			"%w: unexpected external extractor language %q",
			errInvalidExternalExtractorResponse,
			language,
		)
	}
}

func validateExternalExtractorFacts(result ExternalExtractorResult) error {
	expectedParser, expectedSpanFidelity, err := externalFactContract(result.Language)
	if err != nil {
		return err
	}

	seen := make(map[string]bool, len(result.Facts))
	for _, fact := range result.Facts {
		err = validateExternalExtractorFact(
			result,
			fact,
			expectedParser,
			expectedSpanFidelity,
			seen,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

func validateExternalExtractorFact(
	result ExternalExtractorResult,
	fact ExternalExtractorFact,
	expectedParser string,
	expectedSpanFidelity string,
	seen map[string]bool,
) error {
	if strings.TrimSpace(fact.ID) == "" || strings.TrimSpace(fact.Kind) == "" {
		return fmt.Errorf(
			"%w: external extractor returned an unidentified fact for %q",
			errInvalidExternalExtractorResponse,
			result.Path,
		)
	}

	if seen[fact.ID] {
		return fmt.Errorf(
			"%w: external extractor returned duplicate fact %q",
			errInvalidExternalExtractorResponse,
			fact.ID,
		)
	}

	seen[fact.ID] = true

	provenance := fact.Provenance

	err := validateExternalFactProvenance(
		result.Path,
		provenance,
		expectedParser,
		expectedSpanFidelity,
	)
	if err != nil {
		return err
	}

	return validateExternalFactStart(result.Path, provenance)
}

func validateExternalFactProvenance(
	path string,
	provenance ExternalExtractorProvenance,
	expectedParser string,
	expectedSpanFidelity string,
) error {
	if provenance.Class != "EXTRACTED" ||
		provenance.Parser != expectedParser ||
		provenance.ParserRevision != astfacts.PurrdfExtractorRevision ||
		provenance.SourcePath != path {
		return fmt.Errorf(
			"%w: invalid external fact provenance for %q",
			errInvalidExternalExtractorResponse,
			path,
		)
	}

	if provenance.SpanFidelity != expectedSpanFidelity {
		return fmt.Errorf(
			"%w: invalid external fact span fidelity %q for %q",
			errInvalidExternalExtractorResponse,
			provenance.SpanFidelity,
			path,
		)
	}

	return nil
}

func validateExternalFactStart(
	path string,
	provenance ExternalExtractorProvenance,
) error {
	if provenance.SpanFidelity == externalExtractorSpanFidelityNone &&
		provenance.Start != nil {
		return fmt.Errorf(
			"%w: span-free external fact has a start position for %q",
			errInvalidExternalExtractorResponse,
			path,
		)
	}

	if provenance.SpanFidelity == externalExtractorSpanFidelitySubjectStart &&
		provenance.Start != nil &&
		(provenance.Start.ByteOffset < 0 || provenance.Start.Line < 1 ||
			provenance.Start.Column < 1) {
		return fmt.Errorf(
			"%w: invalid external fact start position for %q",
			errInvalidExternalExtractorResponse,
			path,
		)
	}

	return nil
}
