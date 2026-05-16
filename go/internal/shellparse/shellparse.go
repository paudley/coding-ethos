// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package shellparse

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"mvdan.cc/sh/v3/syntax"
)

const (
	ansiByteMax        = 255
	ansiHexByteWidth   = 2
	ansiOctalBase      = 8
	ansiOctalMaxWidth  = 3
	ansiUnicode16Width = 4
	ansiUnicode32Width = 8
	hexBase            = 16
)

type Command struct {
	Command                string
	Name                   string
	Argv                   []string
	Assignments            []string
	Redirects              []string
	Column                 int
	Line                   int
	Background             bool
	HasCommandSubstitution bool
	HasDynamicExpansion    bool
	HasHeredoc             bool
	HasProcessSubstitution bool
	HasSubshell            bool
	IsFunctionDeclaration  bool
}

type Error struct {
	Err    error
	Line   int
	Column int
}

func (err Error) Error() string {
	return err.Err.Error()
}

func (err Error) Unwrap() error {
	return err.Err
}

func Fields(command string) ([]string, error) {
	tokens, err := ControlFields(command)
	if err != nil {
		return nil, err
	}

	fields := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if isControlToken(token) {
			continue
		}

		fields = append(fields, token)
	}

	return fields, nil
}

func ControlFields(command string) ([]string, error) {
	commands, tokens, err := parse(command)
	if err != nil {
		return nil, err
	}

	if len(tokens) > 0 {
		return tokens, nil
	}

	fields := []string{}

	for index, command := range commands {
		if index > 0 {
			fields = append(fields, ";")
		}

		fields = append(fields, command.Assignments...)
		fields = append(fields, command.Argv...)

		fields = append(fields, command.Redirects...)
		if command.Background {
			fields = append(fields, "&")
		}
	}

	return fields, nil
}

func Commands(command string) ([]Command, error) {
	commands, _, err := parse(command)

	return commands, err
}

func CatHeredocCommandSubstitution(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "$(") || !strings.HasSuffix(trimmed, ")") {
		return "", false
	}

	inner := strings.TrimSpace(
		strings.TrimSuffix(strings.TrimPrefix(trimmed, "$("), ")"),
	)

	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).
		Parse(strings.NewReader(inner), "")
	if err != nil {
		return "", false
	}

	return singleCatHeredoc(file)
}

func singleCatHeredoc(file *syntax.File) (string, bool) {
	if len(file.Stmts) != 1 {
		return "", false
	}

	stmt := file.Stmts[0]
	if !isBareCatCall(stmt) {
		return "", false
	}

	for _, redir := range stmt.Redirs {
		if isHeredocRedirect(redir) {
			return wordString(redir.Hdoc), true
		}
	}

	return "", false
}

func isBareCatCall(stmt *syntax.Stmt) bool {
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}

	return wordString(call.Args[0]) == "cat"
}

func isHeredocRedirect(redir *syntax.Redirect) bool {
	return redir != nil && redir.Hdoc != nil &&
		strings.Contains(redir.Op.String(), "<<")
}

func parse(command string) ([]Command, []string, error) {
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).
		Parse(strings.NewReader(command), "")
	if err != nil {
		return nil, nil, parseError(err)
	}

	commands := []Command{}
	controlFields := []string{}

	for index, stmt := range file.Stmts {
		if index > 0 {
			controlFields = append(controlFields, ";")
		}

		walkStatement(stmt, &commands, &controlFields)
	}

	return commands, controlFields, nil
}

func parseError(err error) error {
	wrapped := fmt.Errorf("parse shell command: %w", err)

	var syntaxErr syntax.ParseError
	if errors.As(err, &syntaxErr) {
		return Error{
			Err:    wrapped,
			Line:   lineNumber(syntaxErr.Pos),
			Column: columnNumber(syntaxErr.Pos),
		}
	}

	return wrapped
}

func walkStatement(stmt *syntax.Stmt, commands *[]Command, fields *[]string) {
	if stmt == nil {
		return
	}

	switch cmd := stmt.Cmd.(type) {
	case *syntax.BinaryCmd:
		walkStatement(cmd.X, commands, fields)
		*fields = append(*fields, cmd.Op.String())
		walkStatement(cmd.Y, commands, fields)
	case *syntax.CallExpr:
		command := commandFromCall(cmd)
		*commands = append(*commands, command)
		*fields = append(*fields, command.Assignments...)
		*fields = append(*fields, command.Argv...)
	case *syntax.FuncDecl:
		command := commandFromFuncDecl(cmd)

		*commands = append(*commands, command)
		if command.Command != "" {
			*fields = append(*fields, command.Command)
		}

		walkStatement(cmd.Body, commands, fields)
	case *syntax.Block:
		walkStatements(cmd.Stmts, commands, fields)
	case *syntax.Subshell:
		walkStatements(cmd.Stmts, commands, fields)
	default:
		rendered := renderNode(cmd)
		if rendered != "" {
			command := Command{
				Argv:    []string{rendered},
				Command: rendered,
				Name:    commandName(rendered),
				Line:    lineNumber(stmt.Pos()),
				Column:  columnNumber(stmt.Pos()),
			}
			applyNodeFlags(cmd, &command)
			*commands = append(*commands, command)
			*fields = append(*fields, rendered)
		}
	}

	appendRedirects(stmt.Redirs, commands, fields)

	if stmt.Background {
		if len(*commands) > 0 {
			(*commands)[len(*commands)-1].Background = true
		}

		*fields = append(*fields, "&")
	}
}

func walkStatements(stmts []*syntax.Stmt, commands *[]Command, fields *[]string) {
	for index, stmt := range stmts {
		if index > 0 {
			*fields = append(*fields, ";")
		}

		walkStatement(stmt, commands, fields)
	}
}

func commandFromFuncDecl(decl *syntax.FuncDecl) Command {
	rendered := renderNode(decl)

	command := Command{
		Argv:                  []string{rendered},
		Command:               rendered,
		IsFunctionDeclaration: true,
		Line:                  lineNumber(decl.Pos()),
		Column:                columnNumber(decl.Pos()),
	}
	if decl != nil && decl.Name != nil {
		command.Name = decl.Name.Value
	}

	applyNodeFlags(decl, &command)

	return command
}

func appendRedirects(
	redirs []*syntax.Redirect,
	commands *[]Command,
	fields *[]string,
) {
	if len(redirs) == 0 {
		return
	}

	for _, redir := range redirs {
		rendered := redirectString(redir)
		if rendered == "" {
			continue
		}

		if len(*commands) > 0 {
			last := &(*commands)[len(*commands)-1]
			last.Redirects = append(last.Redirects, rendered)
			last.HasHeredoc = last.HasHeredoc || redir.Hdoc != nil
		}

		*fields = append(*fields, rendered)
	}
}

func redirectString(redir *syntax.Redirect) string {
	if redir == nil {
		return ""
	}

	var builder strings.Builder
	if redir.N != nil {
		builder.WriteString(redir.N.Value)
	}

	builder.WriteString(redir.Op.String())

	if redir.Word != nil {
		builder.WriteString(wordString(redir.Word))
	}

	return builder.String()
}

func commandFromCall(call *syntax.CallExpr) Command {
	command := Command{
		Argv:        make([]string, 0, len(call.Args)),
		Assignments: make([]string, 0, len(call.Assigns)),
		Line:        lineNumber(call.Pos()),
		Column:      columnNumber(call.Pos()),
	}

	for _, assign := range call.Assigns {
		if rendered := assignmentString(assign); rendered != "" {
			command.Assignments = append(command.Assignments, rendered)
		}

		if assign != nil && assign.Value != nil {
			mergeWordInfo(&command, wordInfoFor(assign.Value))
		}
	}

	for _, arg := range call.Args {
		info := wordInfoFor(arg)
		command.Argv = append(command.Argv, info.Text)
		mergeWordInfo(&command, info)
	}

	if len(command.Argv) > 0 {
		command.Name = commandName(command.Argv[0])
		command.Command = strings.Join(command.Argv, " ")
	}

	applyNodeFlags(call, &command)

	return command
}

func assignmentString(assign *syntax.Assign) string {
	if assign == nil || assign.Name == nil {
		return renderNode(assign)
	}

	if assign.Naked {
		return assign.Name.Value
	}

	if assign.Value == nil {
		return assign.Name.Value + "="
	}

	return assign.Name.Value + "=" + wordString(assign.Value)
}

func wordString(word *syntax.Word) string {
	return wordInfoFor(word).Text
}

type wordInfo struct {
	Text                   string
	HasCommandSubstitution bool
	HasDynamicExpansion    bool
	HasProcessSubstitution bool
}

func wordInfoFor(word *syntax.Word) wordInfo {
	if word == nil {
		return wordInfo{}
	}

	var builder strings.Builder

	info := wordInfo{}

	for _, part := range word.Parts {
		partInfo, ok := wordPartInfo(part)
		if !ok {
			info.Text = renderNode(word)
			info.HasDynamicExpansion = true

			return info
		}

		builder.WriteString(partInfo.Text)
		info.HasCommandSubstitution = info.HasCommandSubstitution ||
			partInfo.HasCommandSubstitution
		info.HasDynamicExpansion = info.HasDynamicExpansion ||
			partInfo.HasDynamicExpansion
		info.HasProcessSubstitution = info.HasProcessSubstitution ||
			partInfo.HasProcessSubstitution
	}

	info.Text = builder.String()

	return info
}

func wordPartInfo(part syntax.WordPart) (wordInfo, bool) {
	switch typed := part.(type) {
	case *syntax.Lit:
		return wordInfo{Text: typed.Value}, true
	case *syntax.SglQuoted:
		if typed.Dollar {
			return wordInfo{Text: decodeANSICQuoted(typed.Value)}, true
		}

		return wordInfo{Text: typed.Value}, true
	case *syntax.DblQuoted:
		return doubleQuotedWordInfo(typed)
	case *syntax.ParamExp:
		return wordInfo{
			Text:                renderNode(typed),
			HasDynamicExpansion: true,
		}, true
	case *syntax.CmdSubst:
		return wordInfo{
			Text:                   renderNode(typed),
			HasCommandSubstitution: true,
			HasDynamicExpansion:    true,
		}, true
	case *syntax.ProcSubst:
		return wordInfo{
			Text:                   renderNode(typed),
			HasDynamicExpansion:    true,
			HasProcessSubstitution: true,
		}, true
	default:
		return wordInfo{}, false
	}
}

func doubleQuotedWordInfo(quoted *syntax.DblQuoted) (wordInfo, bool) {
	var builder strings.Builder

	info := wordInfo{}

	for _, nested := range quoted.Parts {
		nestedInfo, ok := wordPartInfo(nested)
		if !ok {
			return wordInfo{}, false
		}

		builder.WriteString(nestedInfo.Text)
		info.HasCommandSubstitution = info.HasCommandSubstitution ||
			nestedInfo.HasCommandSubstitution
		info.HasDynamicExpansion = info.HasDynamicExpansion ||
			nestedInfo.HasDynamicExpansion
		info.HasProcessSubstitution = info.HasProcessSubstitution ||
			nestedInfo.HasProcessSubstitution
	}

	info.Text = builder.String()

	return info, true
}

func decodeANSICQuoted(value string) string {
	var builder strings.Builder

	for index := 0; index < len(value); index++ {
		if value[index] != '\\' || index+1 >= len(value) {
			builder.WriteByte(value[index])

			continue
		}

		decoded, nextIndex, ok := decodeANSIEscape(value, index+1)
		if !ok {
			builder.WriteByte(value[index])

			continue
		}

		builder.WriteString(decoded)

		index = nextIndex
	}

	return builder.String()
}

func decodeANSIEscape(value string, index int) (string, int, bool) {
	decoded, ok := simpleANSIEscape(value[index])
	if ok {
		return decoded, index, true
	}

	switch value[index] {
	case 'x':
		return decodeVariableWidthEscape(
			value,
			index+1,
			ansiHexByteWidth,
			hexBase,
		)
	case 'u':
		return decodeFixedWidthEscape(
			value,
			index+1,
			ansiUnicode16Width,
			hexBase,
		)
	case 'U':
		return decodeFixedWidthEscape(
			value,
			index+1,
			ansiUnicode32Width,
			hexBase,
		)
	}

	if isOctalDigit(value[index]) {
		return decodeVariableWidthEscape(
			value,
			index,
			ansiOctalMaxWidth,
			ansiOctalBase,
		)
	}

	return "", index, false
}

func simpleANSIEscape(char byte) (string, bool) {
	switch char {
	case 'a':
		return "\a", true
	case 'b':
		return "\b", true
	case 'e', 'E':
		return "\x1b", true
	case 'f':
		return "\f", true
	case 'n':
		return "\n", true
	case 'r':
		return "\r", true
	case 't':
		return "\t", true
	case 'v':
		return "\v", true
	case '\\':
		return "\\", true
	case '\'':
		return "'", true
	case '"':
		return "\"", true
	default:
		return "", false
	}
}

func decodeFixedWidthEscape(
	value string,
	start int,
	width int,
	base int,
) (string, int, bool) {
	if start+width > len(value) {
		return "", start, false
	}

	return decodeNumericEscape(value, start, start+width, base)
}

func decodeVariableWidthEscape(
	value string,
	start int,
	maxWidth int,
	base int,
) (string, int, bool) {
	end := start
	for end < len(value) && end-start < maxWidth &&
		validEscapeDigit(value[end], base) {
		end++
	}

	if end == start {
		return "", start, false
	}

	return decodeNumericEscape(value, start, end, base)
}

func validEscapeDigit(char byte, base int) bool {
	if base == ansiOctalBase {
		return isOctalDigit(char)
	}

	return isHexDigit(char)
}

func isOctalDigit(char byte) bool {
	return char >= '0' && char <= '7'
}

func isHexDigit(char byte) bool {
	return (char >= '0' && char <= '9') ||
		(char >= 'a' && char <= 'f') ||
		(char >= 'A' && char <= 'F')
}

func decodeNumericEscape(
	value string,
	start int,
	end int,
	base int,
) (string, int, bool) {
	parsed, err := strconv.ParseUint(value[start:end], base, 32)
	if err != nil {
		return "", start, false
	}

	if base != ansiOctalBase && end-start > ansiHexByteWidth {
		if parsed > utf8.MaxRune {
			return "", start, false
		}

		decoded := rune(parsed)
		if !utf8.ValidRune(decoded) {
			return "", start, false
		}

		return string(decoded), end - 1, true
	}

	if parsed > ansiByteMax {
		return "", start, false
	}

	return string([]byte{byte(parsed)}), end - 1, true
}

func mergeWordInfo(command *Command, info wordInfo) {
	command.HasCommandSubstitution = command.HasCommandSubstitution ||
		info.HasCommandSubstitution
	command.HasDynamicExpansion = command.HasDynamicExpansion ||
		info.HasDynamicExpansion
	command.HasProcessSubstitution = command.HasProcessSubstitution ||
		info.HasProcessSubstitution
}

func applyNodeFlags(node syntax.Node, command *Command) {
	syntax.Walk(node, func(current syntax.Node) bool {
		if current == nil {
			return true
		}

		switch current.(type) {
		case *syntax.CmdSubst:
			command.HasCommandSubstitution = true
			command.HasDynamicExpansion = true
		case *syntax.ProcSubst:
			command.HasProcessSubstitution = true
			command.HasDynamicExpansion = true
		case *syntax.ParamExp:
			command.HasDynamicExpansion = true
		case *syntax.Subshell:
			command.HasSubshell = true
		case *syntax.FuncDecl:
			command.IsFunctionDeclaration = true
		}

		return true
	})
}

func commandName(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}

	return fields[0]
}

func lineNumber(pos syntax.Pos) int {
	if !pos.IsValid() {
		return 0
	}

	return syntaxPositionInt(pos.Line())
}

func columnNumber(pos syntax.Pos) int {
	if !pos.IsValid() {
		return 0
	}

	return syntaxPositionInt(pos.Col())
}

func syntaxPositionInt(value uint) int {
	converted, err := strconv.Atoi(strconv.FormatUint(uint64(value), 10))
	if err != nil {
		return 0
	}

	return converted
}

func renderNode(node syntax.Node) string {
	if node == nil {
		return ""
	}

	var output bytes.Buffer

	err := syntax.NewPrinter().Print(&output, node)
	if err != nil && !errors.Is(err, io.EOF) {
		return ""
	}

	return strings.TrimSpace(output.String())
}

func isControlToken(token string) bool {
	switch token {
	case "&&", "||", ";", "|", "|&", "&":
		return true
	default:
		return false
	}
}
