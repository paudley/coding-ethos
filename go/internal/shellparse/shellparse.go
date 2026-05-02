// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package shellparse

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

type Command struct {
	Argv        []string
	Assignments []string
	Redirects   []string
	Background  bool
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

func parse(command string) ([]Command, []string, error) {
	file, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		return nil, nil, fmt.Errorf("parse shell command: %w", err)
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
	default:
		rendered := renderNode(cmd)
		if rendered != "" {
			command := Command{Argv: []string{rendered}}
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
	}

	for _, assign := range call.Assigns {
		if rendered := assignmentString(assign); rendered != "" {
			command.Assignments = append(command.Assignments, rendered)
		}
	}
	for _, arg := range call.Args {
		command.Argv = append(command.Argv, wordString(arg))
	}

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
	if word == nil {
		return ""
	}

	var builder strings.Builder
	for _, part := range word.Parts {
		text, ok := wordPartString(part)
		if !ok {
			return renderNode(word)
		}
		builder.WriteString(text)
	}

	return builder.String()
}

func wordPartString(part syntax.WordPart) (string, bool) {
	switch typed := part.(type) {
	case *syntax.Lit:
		return typed.Value, true
	case *syntax.SglQuoted:
		return typed.Value, true
	case *syntax.DblQuoted:
		var builder strings.Builder
		for _, nested := range typed.Parts {
			text, ok := wordPartString(nested)
			if !ok {
				return "", false
			}
			builder.WriteString(text)
		}

		return builder.String(), true
	default:
		return "", false
	}
}

func renderNode(node syntax.Node) string {
	if node == nil {
		return ""
	}

	var output bytes.Buffer
	if err := syntax.NewPrinter().Print(&output, node); err != nil && err != io.EOF {
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
