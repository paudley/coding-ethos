// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentproxy

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	// dlpSecret marks a payload containing a known credential shape.
	dlpSecret = "secret"
	// dlpCredentialFile marks a target path whose basename names a secret file.
	dlpCredentialFile = "credential_file"
	// dlpProtectedPath marks a target path referencing a protected secret area.
	dlpProtectedPath = "protected_path"
	// dlpBinaryPayload marks a payload that is binary or invalid UTF-8.
	dlpBinaryPayload = "binary_payload"

	// confidenceHigh labels a deterministic, low-false-positive detection.
	confidenceHigh = "high"
	// confidenceMedium labels a structural detection with broader scope.
	confidenceMedium = "medium"

	// reasonOpenAIKey labels an OpenAI-style API key prefix match.
	reasonOpenAIKey = "openai_api_key_prefix"
	// reasonAWSKey labels an AWS access-key-id prefix match.
	reasonAWSKey = "aws_access_key_id_prefix"
	// reasonGitHubToken labels a GitHub personal/app token prefix match.
	reasonGitHubToken = "github_token_prefix"
	// reasonSlackToken labels a Slack token prefix match.
	reasonSlackToken = "slack_token_prefix"
	// reasonStripeKey labels a Stripe live secret-key prefix match.
	reasonStripeKey = "stripe_live_secret_key_prefix"
	// reasonPEMKey labels a PEM private-key header match.
	reasonPEMKey = "pem_private_key_header"
	// reasonCredentialFile labels a secret-file basename match.
	reasonCredentialFile = "credential_file_basename"
	// reasonProtectedPath labels a protected secret-area path match.
	reasonProtectedPath = "protected_path_segment"
	// reasonCredentialFileContent labels a credential-file path reference found in
	// the decoded body rather than the request target path.
	reasonCredentialFileContent = "credential_file_content_path"
	// reasonProtectedPathContent labels a protected-path reference found in the
	// decoded body rather than the request target path.
	reasonProtectedPathContent = "protected_path_content_segment"
	// reasonNULByte labels a payload containing a NUL byte.
	reasonNULByte = "nul_byte"
	// reasonInvalidUTF8 labels a payload that is not valid UTF-8.
	reasonInvalidUTF8 = "invalid_utf8"

	// dotEnvBase is the exact basename of an environment-secrets file.
	dotEnvBase = ".env"
	// dotEnvPrefix matches dotenv variants such as .env.production.
	dotEnvPrefix = ".env."
	// pemSuffix marks PEM certificate and key files by extension.
	pemSuffix = ".pem"
)

// Compiled credential-shape patterns. Each matches only the deterministic
// prefix/format of a known credential class so false positives stay low. They
// are package-level so they compile exactly once at load time rather than per
// scan; the match index is used only to locate the finding, never to retain it.
var (
	patternOpenAIKey   = regexp.MustCompile(`sk-[A-Za-z0-9_-]{17,}`)
	patternAWSKey      = regexp.MustCompile(`AKIA[0-9A-Z]{16}`)
	patternGitHubToken = regexp.MustCompile(`gh[opsur]_[A-Za-z0-9]{33,}`)
	patternSlackToken  = regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`)
	patternStripeKey   = regexp.MustCompile(`(?:sk|rk)_live_[A-Za-z0-9]{10,}`)
	patternPEMKey      = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)
)

// Content-path detectors fire when the decoded body embeds a path-shaped
// reference to a protected secret area or a credential file. They are
// case-insensitive ((?i)) and anchored to a path-token boundary — a string
// start, whitespace, quote, or slash — so a bare word in prose (".env",
// "secrets") does not match while a real path token (".../secrets/",
// ".../id_rsa") does. They are package-level so they compile once at load time;
// only the match location is retained in a fact, never the matched content.
var (
	patternContentProtectedPath = regexp.MustCompile(
		`(?i)(?:^|[\s"'/])[^\s"']*/(?:\.ssh|\.gnupg|\.aws|\.config/gcloud|secrets)/`,
	)
	patternContentCredentialFile = regexp.MustCompile(
		`(?i)(?:^|[\s"'/])[^\s"']*` +
			`(?:/(?:\.env(?:\.[\w-]+)?|id_rsa|id_ed25519|id_dsa|credentials|` +
			`\.npmrc|\.netrc|\.pgpass)|\.pem)(?:\b|$)`,
	)
)

// secretDetector pairs a compiled credential-shape pattern with the stable
// detector label and confidence reported for a match. The label never contains
// any matched payload content; it identifies only which detector fired.
type secretDetector struct {
	pattern    *regexp.Regexp
	reason     string
	confidence string
}

// ScanRequest returns deterministic DLP facts for an already-bounded decoded
// payload and its target path. Facts carry only detector labels, confidence,
// and match locations; no matched secret value or payload content is ever
// retained in any returned fact field.
func ScanRequest(decoded []byte, targetPath string) []DLPFact {
	facts := make([]DLPFact, 0, len(secretDetectorList()))
	facts = append(facts, scanSecrets(decoded)...)
	facts = append(facts, scanCredentialFile(targetPath)...)
	facts = append(facts, scanProtectedPath(targetPath)...)
	facts = append(facts, scanContentProtectedPath(decoded)...)
	facts = append(facts, scanContentCredentialFile(decoded)...)
	facts = append(facts, scanBinary(decoded)...)

	return facts
}

// secretDetectorList returns the ordered credential-shape detectors. It binds
// the package-level compiled patterns to their stable labels without holding
// any additional mutable package state.
func secretDetectorList() []secretDetector {
	return []secretDetector{
		{patternOpenAIKey, reasonOpenAIKey, confidenceHigh},
		{patternAWSKey, reasonAWSKey, confidenceHigh},
		{patternGitHubToken, reasonGitHubToken, confidenceHigh},
		{patternSlackToken, reasonSlackToken, confidenceHigh},
		{patternStripeKey, reasonStripeKey, confidenceHigh},
		{patternPEMKey, reasonPEMKey, confidenceHigh},
	}
}

// scanSecrets reports credential-shape matches by detector label only. The
// matched substring is used solely to compute a 1-based line and byte column;
// it is never copied into a fact field.
func scanSecrets(decoded []byte) []DLPFact {
	detectors := secretDetectorList()
	facts := make([]DLPFact, 0, len(detectors))

	for _, detector := range detectors {
		location := detector.pattern.FindIndex(decoded)
		if location == nil {
			continue
		}

		line, column := lineColumn(decoded, location[0])
		facts = append(facts, DLPFact{
			Type:       dlpSecret,
			Reason:     detector.reason,
			Confidence: detector.confidence,
			Line:       line,
			Column:     column,
		})
	}

	return facts
}

// scanCredentialFile reports a credential-file fact when the target path's
// basename names a known secret file or carries a .pem suffix. Only the
// basename is retained, never the payload content.
func scanCredentialFile(targetPath string) []DLPFact {
	normalized := strings.ReplaceAll(strings.TrimSpace(targetPath), "\\", "/")
	base := filepath.Base(normalized)

	if base == "." || base == string(filepath.Separator) || base == "" {
		return nil
	}

	if !isCredentialBasename(base) {
		return nil
	}

	return []DLPFact{{
		Type:       dlpCredentialFile,
		Path:       base,
		Reason:     reasonCredentialFile,
		Confidence: confidenceHigh,
	}}
}

// isCredentialBasename reports whether base names a known secret file by exact
// name, dotenv shape, or PEM extension. Matching is case-insensitive so a
// Windows-cased or upper-cased basename (e.g. ID_RSA, .PEM) is still detected.
func isCredentialBasename(base string) bool {
	lower := strings.ToLower(base)

	names := map[string]struct{}{
		"id_rsa":      {},
		"id_ed25519":  {},
		"id_dsa":      {},
		"credentials": {},
		".npmrc":      {},
		".netrc":      {},
		".pgpass":     {},
	}

	if _, named := names[lower]; named {
		return true
	}

	return lower == dotEnvBase ||
		strings.HasPrefix(lower, dotEnvPrefix) ||
		strings.HasSuffix(lower, pemSuffix)
}

// scanProtectedPath reports a protected-path fact when the target path contains
// a known protected secret-area segment. Only the basename is retained.
func scanProtectedPath(targetPath string) []DLPFact {
	normalized := strings.ReplaceAll(strings.TrimSpace(targetPath), "\\", "/")
	if normalized == "" {
		return nil
	}

	if !containsProtectedSegment(normalized) {
		return nil
	}

	return []DLPFact{{
		Type:       dlpProtectedPath,
		Path:       filepath.Base(normalized),
		Reason:     reasonProtectedPath,
		Confidence: confidenceMedium,
	}}
}

// protectedPathSegments returns the secret-area path segments scanned for in
// both target paths and decoded content. Each entry is a full slash-bounded
// segment (".ssh", ".config/gcloud", "secrets") matched at a path boundary. It
// is a function rather than a package var so it holds no mutable global state.
func protectedPathSegments() []string {
	return []string{
		".ssh",
		".gnupg",
		".aws",
		".config/gcloud",
		"secrets",
	}
}

// containsProtectedSegment reports whether a forward-slash path contains a known
// protected segment at a path boundary. Matching is case-insensitive and
// segment-aware: each segment must be preceded by a slash or the string start
// and followed by a slash, so "mysecrets/" does not match "secrets/" while
// "Secrets/" and ".SSH/" do.
func containsProtectedSegment(normalized string) bool {
	lower := strings.ToLower(normalized)
	for _, segment := range protectedPathSegments() {
		if pathContainsSegment(lower, segment) {
			return true
		}
	}

	return false
}

// pathContainsSegment reports whether lower (already lower-cased, forward-slash)
// contains segment bounded by slashes or the string start on the left and a
// slash on the right. Segment is supplied lower-cased without surrounding
// slashes; a multi-part segment such as ".config/gcloud" is matched verbatim.
func pathContainsSegment(lower, segment string) bool {
	for searchFrom := 0; searchFrom < len(lower); {
		index := strings.Index(lower[searchFrom:], segment)
		if index < 0 {
			return false
		}

		start := searchFrom + index
		end := start + len(segment)
		leftBoundary := start == 0 || lower[start-1] == '/'
		rightBoundary := end < len(lower) && lower[end] == '/'

		if leftBoundary && rightBoundary {
			return true
		}

		searchFrom = start + 1
	}

	return false
}

// scanContentProtectedPath reports a protected-path fact when the decoded body
// embeds a path-shaped reference to a known protected secret area. The match
// index locates a 1-based line and column; the matched text is never retained.
// This complements scanProtectedPath, which inspects the request target path,
// so an outbound request that carries a protected path in its body is flagged.
func scanContentProtectedPath(decoded []byte) []DLPFact {
	location := patternContentProtectedPath.FindIndex(decoded)
	if location == nil {
		return nil
	}

	line, column := lineColumn(decoded, location[0])

	return []DLPFact{{
		Type:       dlpProtectedPath,
		Reason:     reasonProtectedPathContent,
		Confidence: confidenceMedium,
		Line:       line,
		Column:     column,
	}}
}

// scanContentCredentialFile reports a credential-file fact when the decoded body
// embeds a path-shaped reference whose final component names a known credential
// file (e.g. .env, id_rsa, *.pem). Only the match location is retained, never
// the matched text. This complements scanCredentialFile, which inspects the
// request target path, so exfiltrating a credential-file path in the body is
// flagged even when no file target is present.
func scanContentCredentialFile(decoded []byte) []DLPFact {
	location := patternContentCredentialFile.FindIndex(decoded)
	if location == nil {
		return nil
	}

	line, column := lineColumn(decoded, location[0])

	return []DLPFact{{
		Type:       dlpCredentialFile,
		Reason:     reasonCredentialFileContent,
		Confidence: confidenceHigh,
		Line:       line,
		Column:     column,
	}}
}

// scanBinary reports a binary-payload fact when the payload contains a NUL byte
// or is not valid UTF-8. No payload bytes are retained in the fact.
func scanBinary(decoded []byte) []DLPFact {
	if bytes.IndexByte(decoded, 0) >= 0 {
		return []DLPFact{{
			Type:       dlpBinaryPayload,
			Reason:     reasonNULByte,
			Confidence: confidenceHigh,
		}}
	}

	if !utf8.Valid(decoded) {
		return []DLPFact{{
			Type:       dlpBinaryPayload,
			Reason:     reasonInvalidUTF8,
			Confidence: confidenceHigh,
		}}
	}

	return nil
}

// lineColumn converts a byte offset into a 1-based line number and 1-based byte
// column within that line. It inspects only positions, never copying matched
// content into any returned value.
func lineColumn(decoded []byte, offset int) (int, int) {
	prefix := decoded[:offset]
	line := bytes.Count(prefix, []byte{'\n'}) + 1

	lastNewline := bytes.LastIndexByte(prefix, '\n')
	column := offset - lastNewline

	return line, column
}
