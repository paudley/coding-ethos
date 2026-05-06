// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package toolconfigs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

const yamlIndentSpaces = 2

type yamlPair struct {
	Value any
	Key   string
}

type orderedMap []yamlPair

func jsonString(value string) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}

	return string(data)
}

func tomlList(values []string) string {
	if len(values) == 0 {
		return "[]"
	}

	rendered := make([]string, 0, len(values))
	for _, value := range values {
		rendered = append(rendered, jsonString(value))
	}

	return "[" + strings.Join(rendered, ", ") + "]"
}

func titleBool(value bool) string {
	if value {
		return "True"
	}

	return "False"
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}

	return "no"
}

func writeINISection(
	builder *strings.Builder,
	name string,
	values map[string]string,
	order []string,
) {
	builder.WriteString("[")
	builder.WriteString(name)
	builder.WriteString("]\n")

	for _, key := range order {
		value, ok := values[key]
		if !ok || value == "" {
			continue
		}

		builder.WriteString(key)
		builder.WriteString(" = ")
		builder.WriteString(value)
		builder.WriteString("\n")
	}

	builder.WriteString("\n")
}

func finishINI(builder *strings.Builder) string {
	return strings.TrimRight(builder.String(), "\n") + "\n"
}

func containsString(values []string, target string) bool {
	return slices.Contains(values, target)
}

func sortedKeys(values map[string][]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

func sortedKeysAny(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

func mappingValue(config configMap, dottedPath string) map[string]any {
	return mapValue(getPath(config, dottedPath, map[string]any{}))
}

func mapValue(value any) map[string]any {
	if mapping, ok := value.(map[string]any); ok {
		return mapping
	}

	return nil
}

func cloneMap(values map[string]any) map[string]any {
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		if nested, ok := value.(map[string]any); ok {
			cloned[key] = cloneMap(nested)
		} else {
			cloned[key] = value
		}
	}

	return cloned
}

func valueOr(value, fallback any) any {
	if value == nil || truthyString(value) == "" {
		return fallback
	}

	return value
}

func renderYAML(value any) string {
	node := &yaml.Node{}
	buildYAMLNode(node, value)

	var buffer bytes.Buffer

	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(yamlIndentSpaces)

	err := encoder.Encode(node)
	if err != nil {
		panic(fmt.Errorf("render yaml: %w", err))
	}

	err = encoder.Close()
	if err != nil {
		panic(fmt.Errorf("close yaml encoder: %w", err))
	}

	return buffer.String()
}

func buildYAMLNode(node *yaml.Node, value any) {
	switch typed := value.(type) {
	case orderedMap:
		buildOrderedMapYAMLNode(node, typed)
	case map[string]any:
		buildMapYAMLNode(node, typed)
	case []string:
		buildStringSequenceYAMLNode(node, typed)
	case []any:
		buildAnySequenceYAMLNode(node, typed)
	case bool:
		node.Kind = yaml.ScalarNode
		node.Tag = "!!bool"
		node.Value = strconv.FormatBool(typed)
	case int:
		node.Kind = yaml.ScalarNode
		node.Tag = "!!int"
		node.Value = strconv.Itoa(typed)
	default:
		node.Kind = yaml.ScalarNode
		node.Tag = "!!str"

		node.Value = fmt.Sprint(typed)
		if node.Value == "true" || node.Value == "false" || node.Value == "2" {
			node.Style = yaml.SingleQuotedStyle
		}
	}
}

func buildOrderedMapYAMLNode(node *yaml.Node, values orderedMap) {
	node.Kind = yaml.MappingNode
	for _, pair := range values {
		keyNode := yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: pair.Key}
		valueNode := yaml.Node{}
		buildYAMLNode(&valueNode, pair.Value)
		node.Content = append(node.Content, &keyNode, &valueNode)
	}
}

func buildMapYAMLNode(node *yaml.Node, values map[string]any) {
	node.Kind = yaml.MappingNode

	for _, key := range orderedYAMLKeys(values) {
		keyNode := yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
		valueNode := yaml.Node{}
		buildYAMLNode(&valueNode, values[key])
		node.Content = append(node.Content, &keyNode, &valueNode)
	}
}

func buildStringSequenceYAMLNode(node *yaml.Node, values []string) {
	node.Kind = yaml.SequenceNode
	for _, item := range values {
		valueNode := yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: item}
		if item == trueString || item == "false" {
			valueNode.Style = yaml.SingleQuotedStyle
		}

		node.Content = append(node.Content, &valueNode)
	}
}

func buildAnySequenceYAMLNode(node *yaml.Node, values []any) {
	node.Kind = yaml.SequenceNode

	for _, item := range values {
		valueNode := yaml.Node{}
		buildYAMLNode(&valueNode, item)
		node.Content = append(node.Content, &valueNode)
	}
}

func orderedYAMLKeys(values map[string]any) []string {
	keys := sortedKeysAny(values)

	priority := yamlKeyPriority(values)
	if len(priority) == 0 {
		return keys
	}

	rank := make(map[string]int, len(priority))
	for index, key := range priority {
		rank[key] = index
	}

	sort.SliceStable(keys, func(left, right int) bool {
		leftRank, leftOK := rank[keys[left]]

		rightRank, rightOK := rank[keys[right]]
		if leftOK && rightOK {
			return leftRank < rightRank
		}

		if leftOK {
			return true
		}

		if rightOK {
			return false
		}

		return keys[left] < keys[right]
	})

	return keys
}

func yamlKeyPriority(values map[string]any) []string {
	switch {
	case hasKeys(values, "version", "run", "issues", "linters"):
		return []string{"version", "run", "issues", "linters"}
	case hasKeys(values, "default", "enable", "exclusions", "settings"):
		return []string{"default", "enable", "exclusions", "settings"}
	case hasKeys(values, "generated", "warn-unused", "presets"):
		return []string{"generated", "warn-unused", "presets"}
	case hasKeys(values, "pkg", "desc"):
		return []string{"pkg", "desc"}
	case hasKeys(
		values,
		"replace-allow-list",
		"retract-allow-no-explanation",
		"exclude-forbidden",
	):
		return []string{
			"replace-allow-list",
			"retract-allow-no-explanation",
			"exclude-forbidden",
		}
	case hasKeys(values, "tab-width", "line-length"):
		return []string{"tab-width", "line-length"}
	case hasKeys(values, "cyclop", "depguard", "errcheck", "funlen"):
		return []string{
			"cyclop",
			"depguard",
			"errcheck",
			"funlen",
			"gocognit",
			"gocyclo",
			"gosec",
			"govet",
			"gomoddirectives",
			"lll",
			"nestif",
			"revive",
			"tagliatelle",
			"testifylint",
		}
	default:
		return nil
	}
}

func hasKeys(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := values[key]; !ok {
			return false
		}
	}

	return true
}
