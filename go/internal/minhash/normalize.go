// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package minhash

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
)

const (
	PlaceholderID     = "$ID"
	PlaceholderString = "$STR"
	PlaceholderNumber = "$NUM"

	// numericPrefixLen is the length of numeric base prefixes (0x, 0o, 0b).
	numericPrefixLen = 2
)

func NormalizeTokens(rawText, language string) []string {
	tokens := tokenize(rawText, language)
	normalized := make([]string, 0, len(tokens))

	for _, tok := range tokens {
		norm := normalizeToken(tok, language)
		if norm != "" {
			normalized = append(normalized, norm)
		}
	}

	return normalized
}

func NormalizedHash(tokens []string) string {
	joined := strings.Join(tokens, " ")
	sum := sha256.Sum256([]byte(joined))

	return hex.EncodeToString(sum[:])
}

func tokenize(rawText, language string) []string {
	var tokens []string

	runes := []rune(rawText)
	pos := 0

	for pos < len(runes) {
		tok, next := tokenizeOne(runes, pos, language)
		if tok != "" {
			tokens = append(tokens, tok)
		}

		pos = next
	}

	return tokens
}

func tokenizeOne(runes []rune, pos int, language string) (string, int) {
	char := runes[pos]

	if unicode.IsSpace(char) {
		return "", pos + 1
	}

	if next, skipped := skipComment(runes, pos, char, language); skipped {
		return "", next
	}

	return tokenizeValue(runes, pos, char)
}

func skipComment(
	runes []rune,
	pos int,
	char rune,
	language string,
) (int, bool) {
	if char == '/' && pos+1 < len(runes) {
		if runes[pos+1] == '/' {
			return skipToEndOfLine(runes, pos), true
		}

		if runes[pos+1] == '*' {
			return skipBlockComment(runes, pos), true
		}
	}

	if char == '#' && isLineCommentLanguage(language) {
		return skipToEndOfLine(runes, pos), true
	}

	return 0, false
}

func tokenizeValue(runes []rune, pos int, char rune) (string, int) {
	if char == '"' || char == '\'' || char == '`' {
		return scanString(runes, pos, char)
	}

	if unicode.IsDigit(char) ||
		(char == '.' && pos+1 < len(runes) && unicode.IsDigit(runes[pos+1])) {
		return scanNumber(runes, pos)
	}

	if unicode.IsLetter(char) || char == '_' {
		return scanIdentifier(runes, pos)
	}

	return string(char), pos + 1
}

func normalizeToken(token, language string) string {
	if isKeyword(token, language) {
		return token
	}

	if isOperatorOrPunctuation(token) {
		return token
	}

	if len(token) == 0 {
		return token
	}

	return classifyLiteral(token)
}

func classifyLiteral(token string) string {
	first := rune(token[0])

	if first == '"' || first == '\'' || first == '`' {
		return PlaceholderString
	}

	if unicode.IsDigit(first) || (first == '.' && len(token) > 1) {
		return PlaceholderNumber
	}

	if unicode.IsLetter(first) || first == '_' {
		return PlaceholderID
	}

	return token
}

func isLineCommentLanguage(language string) bool {
	return language == "python" || language == "shell"
}

func skipToEndOfLine(runes []rune, start int) int {
	pos := start

	for pos < len(runes) && runes[pos] != '\n' {
		pos++
	}

	return pos
}

const blockCommentStartLen = 2

func skipBlockComment(runes []rune, start int) int {
	pos := start + blockCommentStartLen

	for pos+1 < len(runes) {
		if runes[pos] == '*' && runes[pos+1] == '/' {
			return pos + blockCommentStartLen
		}

		pos++
	}

	return len(runes)
}

func scanString(runes []rune, start int, quote rune) (string, int) {
	pos := start + 1

	for pos < len(runes) {
		if runes[pos] == '\\' {
			pos += 2

			continue
		}

		if runes[pos] == quote {
			return string(runes[start : pos+1]), pos + 1
		}

		pos++
	}

	return string(runes[start:]), len(runes)
}

func scanNumber(runes []rune, start int) (string, int) {
	if tok, end, ok := scanPrefixedNumber(runes, start); ok {
		return tok, end
	}

	return scanDecimalNumber(runes, start)
}

func scanPrefixedNumber(runes []rune, start int) (string, int, bool) {
	if start >= len(runes) || runes[start] != '0' || start+1 >= len(runes) {
		return "", 0, false
	}

	next := unicode.ToLower(runes[start+1])
	if next != 'x' && next != 'o' && next != 'b' {
		return "", 0, false
	}

	pos := start + numericPrefixLen

	for pos < len(runes) &&
		(unicode.IsDigit(runes[pos]) || isHexLetter(runes[pos]) || runes[pos] == '_') {
		pos++
	}

	return string(runes[start:pos]), pos, true
}

func scanDecimalNumber(runes []rune, start int) (string, int) {
	pos := start
	hasDot := false

	for pos < len(runes) {
		char := runes[pos]

		if unicode.IsDigit(char) || char == '_' {
			pos++

			continue
		}

		if char == '.' && !hasDot {
			hasDot = true
			pos++

			continue
		}

		if char == 'e' || char == 'E' {
			pos = scanExponentSuffix(runes, pos)

			continue
		}

		break
	}

	return string(runes[start:pos]), pos
}

func scanExponentSuffix(runes []rune, pos int) int {
	next := pos + 1
	if next < len(runes) && (runes[next] == '+' || runes[next] == '-') {
		next++
	}

	return next
}

func isHexLetter(ch rune) bool {
	lower := unicode.ToLower(ch)

	return lower >= 'a' && lower <= 'f'
}

func scanIdentifier(runes []rune, start int) (string, int) {
	pos := start

	for pos < len(runes) && isIdentRune(runes[pos]) {
		pos++
	}

	return string(runes[start:pos]), pos
}

func isIdentRune(ch rune) bool {
	return unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_'
}

func isOperatorOrPunctuation(token string) bool {
	if len(token) == 0 {
		return false
	}

	switch token {
	case "(", ")", "{", "}", "[", "]",
		".", ",", ";", ":", "::",
		"+", "-", "*", "/", "%",
		"=", "==", "!=", "<", ">", "<=", ">=",
		"&&", "||", "!",
		"&", "|", "^", "~", "<<", ">>",
		"+=", "-=", "*=", "/=", "%=",
		"&=", "|=", "^=", "<<=", ">>=",
		"++", "--",
		"->", "=>", "...", "..",
		"?", "??", "?.", ":=":
		return true
	default:
		return false
	}
}

func isKeyword(token, language string) bool {
	switch language {
	case "go":
		return goKeywords[token]
	case "python":
		return pythonKeywords[token]
	case "javascript":
		return javaScriptKeywords[token]
	case "shell":
		return shellKeywords[token]
	default:
		return false
	}
}

//nolint:gochecknoglobals // Immutable language keyword lookup tables.
var goKeywords = buildKeywordSet([]string{
	"break", "case", "chan", "const", "continue",
	"default", "defer", "else", "fallthrough", "for",
	"func", "go", "goto", "if", "import",
	"interface", "map", "package", "range", "return",
	"select", "struct", "switch", "type", "var",
	"true", "false", "nil",
	"bool", "byte", "int", "int8", "int16", "int32", "int64",
	"uint", "uint8", "uint16", "uint32", "uint64",
	"float32", "float64", "complex64", "complex128",
	"string", "error", "rune", "uintptr",
	"append", "cap", "close", "copy", "delete",
	"len", "make", "new", "panic", "print", "println", "recover",
})

//nolint:gochecknoglobals // Immutable language keyword lookup tables.
var pythonKeywords = buildKeywordSet([]string{
	"False", "None", "True", "and", "as", "assert",
	"async", "await", "break", "class", "continue",
	"def", "del", "elif", "else", "except",
	"finally", "for", "from", "global", "if",
	"import", "in", "is", "lambda", "nonlocal",
	"not", "or", "pass", "raise", "return",
	"try", "while", "with", "yield",
	"self", "cls",
	"int", "str", "float", "bool", "list", "dict", "set", "tuple",
	"len", "range", "print", "type", "isinstance",
	"super", "property", "staticmethod", "classmethod",
})

//nolint:gochecknoglobals // Immutable language keyword lookup tables.
var javaScriptKeywords = buildKeywordSet([]string{
	"break", "case", "catch", "class", "const", "continue",
	"debugger", "default", "delete", "do", "else",
	"export", "extends", "false", "finally", "for",
	"function", "if", "import", "in", "instanceof",
	"let", "new", "null", "of", "return",
	"super", "switch", "this", "throw", "true",
	"try", "typeof", "undefined", "var", "void",
	"while", "with", "yield",
	"async", "await",
	"console", "require", "module", "exports",
})

//nolint:gochecknoglobals // Immutable language keyword lookup tables.
var shellKeywords = buildKeywordSet([]string{
	"if", "then", "else", "elif", "fi",
	"case", "esac", "for", "while", "until",
	"do", "done", "in", "function",
	"return", "exit", "break", "continue",
	"local", "export", "readonly", "declare",
	"echo", "printf", "read", "set", "unset",
	"true", "false", "test",
})

func buildKeywordSet(keywords []string) map[string]bool {
	set := make(map[string]bool, len(keywords))
	for _, kw := range keywords {
		set[kw] = true
	}

	return set
}
