// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package webguidance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/configdata"
)

const (
	hoursPerDay     = 24
	DefaultCacheTTL = hoursPerDay * time.Hour
)

var (
	errWebGuidanceConfigMustBeTable = errors.New("web_guidance must be a table")
	errModernWebConfigMustBeTable   = errors.New(
		"web_guidance.modern_web must be a table",
	)
	errUnknownWebGuidanceConfigPath = errors.New(
		"unknown web guidance TOML config path",
	)
	errInvalidWebGuidanceDuration = errors.New(
		"invalid web guidance duration",
	)
)

// Settings contains all web guidance provider settings.
type Settings struct {
	ModernWeb ModernWebSettings `json:"modern_web"`
}

// ModernWebSettings controls the Modern Web Guidance integration.
type ModernWebSettings struct {
	CacheTTLText        string        `json:"cache_ttl"`
	BrowserPolicy       string        `json:"browser_policy"`
	CacheTTL            time.Duration `json:"-"`
	Enabled             bool          `json:"enabled"`
	AllowNetworkRefresh bool          `json:"allow_network_refresh"`
}

// DefaultSettings returns the default web guidance settings.
func DefaultSettings() Settings {
	return Settings{
		ModernWeb: ModernWebSettings{
			Enabled:             true,
			CacheTTL:            DefaultCacheTTL,
			CacheTTLText:        "24h",
			AllowNetworkRefresh: true,
		},
	}
}

// LoadSettings reads web guidance settings from config.toml and
// repo_config.toml under root. Missing files keep compiled defaults.
func LoadSettings(root string) (Settings, error) {
	settings := DefaultSettings()

	cleanRoot, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return Settings{}, fmt.Errorf("resolve web guidance settings root: %w", err)
	}

	for _, name := range []string{"config.toml", "repo_config.toml"} {
		path := filepath.Join(cleanRoot, name)

		config, err := configdata.LoadTOMLMap(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}

			return Settings{}, fmt.Errorf("load web guidance settings %s: %w", path, err)
		}

		err = applySettingsMap(&settings, config, name)
		if err != nil {
			return Settings{}, err
		}
	}

	return settings, nil
}

func applySettingsMap(
	settings *Settings,
	config configdata.Map,
	file string,
) error {
	webGuidance, found := config["web_guidance"]
	if !found {
		return nil
	}

	values := configdata.MapValue(webGuidance)
	if values == nil {
		return fmt.Errorf("%w: %s", errWebGuidanceConfigMustBeTable, file)
	}

	for key, value := range values {
		switch key {
		case "modern_web":
			modernWeb := configdata.MapValue(value)
			if modernWeb == nil {
				return fmt.Errorf("%w: %s", errModernWebConfigMustBeTable, file)
			}

			err := applyModernWebSettings(&settings.ModernWeb, modernWeb, file)
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf(
				"%w: web_guidance.%s in %s",
				errUnknownWebGuidanceConfigPath,
				key,
				file,
			)
		}
	}

	return nil
}

func applyModernWebSettings(
	settings *ModernWebSettings,
	values configdata.Map,
	file string,
) error {
	for key, value := range values {
		switch key {
		case "enabled":
			settings.Enabled = boolValue(value)
		case "cache_ttl":
			duration, err := ParseDuration(strings.TrimSpace(fmt.Sprint(value)))
			if err != nil {
				return fmt.Errorf(
					"%w: web_guidance.modern_web.cache_ttl in %s",
					errInvalidWebGuidanceDuration,
					file,
				)
			}

			settings.CacheTTL = duration
			settings.CacheTTLText = durationText(duration)
		case "allow_network_refresh":
			settings.AllowNetworkRefresh = boolValue(value)
		case "browser_policy":
			settings.BrowserPolicy = strings.TrimSpace(fmt.Sprint(value))
		default:
			return fmt.Errorf(
				"%w: web_guidance.modern_web.%s in %s",
				errUnknownWebGuidanceConfigPath,
				key,
				file,
			)
		}
	}

	return nil
}

// ParseDuration parses compact duration strings used in repo TOML settings.
func ParseDuration(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}

	dayText, found := strings.CutSuffix(value, "d")
	if found {
		days, err := strconv.Atoi(dayText)
		if err != nil || days < 0 {
			return 0, fmt.Errorf("%w: %q", errInvalidWebGuidanceDuration, value)
		}

		return time.Duration(days) * hoursPerDay * time.Hour, nil
	}

	duration, err := time.ParseDuration(value)
	if err != nil || duration < 0 {
		return 0, fmt.Errorf("%w: %q", errInvalidWebGuidanceDuration, value)
	}

	return duration, nil
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return fmt.Sprint(value) == "true"
	}
}

func durationText(duration time.Duration) string {
	if duration == 0 {
		return ""
	}

	if duration%(hoursPerDay*time.Hour) == 0 {
		return fmt.Sprintf("%dd", int(duration/(hoursPerDay*time.Hour)))
	}

	return duration.String()
}
