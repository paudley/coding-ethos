// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package toolconfigs

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/configdata"
)

type configMap = configdata.Map

const trueString = "true"

var errInvalidConfigChoice = errors.New("invalid configured choice")

func getPath(config configMap, path string, fallback any) any {
	return configdata.GetPath(config, path, fallback)
}

func stringList(value any) []string {
	return configdata.StringList(value)
}

func configuredList(config configMap, path string, fallback []string) []string {
	values := stringList(getPath(config, path, []any{}))
	if len(values) == 0 {
		return append([]string(nil), fallback...)
	}

	return values
}

func truthyString(value any) string {
	return strings.TrimSpace(fmt.Sprint(value))
}

func configuredString(config configMap, path, fallback string) string {
	configured := truthyString(getPath(config, path, ""))
	if configured == "" {
		return fallback
	}

	return configured
}

func configuredChoice(
	config configMap,
	path string,
	fallback string,
	choices map[string]struct{},
) (string, error) {
	configured := configuredString(config, path, fallback)
	if _, ok := choices[configured]; !ok {
		allowed := make([]string, 0, len(choices))
		for choice := range choices {
			allowed = append(allowed, choice)
		}

		return "", fmt.Errorf(
			"%w: %s=%s; allowed=%s",
			errInvalidConfigChoice,
			path,
			configured,
			strings.Join(allowed, ", "),
		)
	}

	return configured, nil
}

func configuredBool(config configMap, path string, fallback bool) bool {
	configured := getPath(config, path, fallback)
	if value, ok := configured.(bool); ok {
		return value
	}

	if value, ok := configured.(string); ok {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", trueString, "yes", "on":
			return true
		default:
			return false
		}
	}

	return configured != nil
}

func configuredInt(config configMap, path string, fallback int) int {
	configured := getPath(config, path, fallback)
	switch value := configured.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil {
			return parsed
		}
	}

	return fallback
}
