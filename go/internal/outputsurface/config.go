// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package outputsurface

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
	defaultReportFormat = "toon"
	defaultMaxBytes     = int64(0)
	hoursPerDay         = 24
	kibibyte            = 1024
	kilobyte            = 1000
)

var (
	errReportConfigMustBeTable  = errors.New("outputs.report must be a table")
	errPruneConfigMustBeTable   = errors.New("outputs.prune must be a table")
	errSurfaceConfigMustBeTable = errors.New("output surface prune policy must be a table")
	errUnknownOutputConfigPath  = errors.New("unknown output TOML config path")
	errUnknownOutputSurface     = errors.New("unknown output surface")
)

// Settings contains output reporting and pruning configuration.
type Settings struct {
	Prune  PruneSettings  `json:"prune"`
	Report ReportSettings `json:"report"`
}

// ReportSettings controls output report defaults.
type ReportSettings struct {
	DefaultFormat    string `json:"default_format"`
	IncludeTemp      bool   `json:"include_temp"`
	IncludeSensitive bool   `json:"include_sensitive"`
}

// PruneSettings controls command and automatic pruning.
type PruneSettings struct {
	Surfaces             map[string]SurfaceRetentionPolicy `json:"surfaces"`
	Enabled              bool                              `json:"enabled"`
	AutoEnabled          bool                              `json:"auto_enabled"`
	RequireApplyForPrune bool                              `json:"require_apply_for_prune"`
	WritePruneTrace      bool                              `json:"write_prune_trace"`
}

// SurfaceRetentionPolicy controls one output surface lifecycle.
type SurfaceRetentionPolicy struct {
	MaxAgeText             string        `json:"max_age,omitempty"`
	MaxBytesText           string        `json:"max_bytes,omitempty"`
	MaxAge                 time.Duration `json:"-"`
	MaxBytes               int64         `json:"-"`
	KeepLast               int           `json:"keep_last,omitempty"`
	RowRetentionDays       int           `json:"row_retention_days,omitempty"`
	Enabled                bool          `json:"enabled"`
	Auto                   bool          `json:"auto"`
	RequireCodeIntelIngest bool          `json:"require_code_intel_ingest"`
	VacuumAfterPrune       bool          `json:"vacuum_after_prune"`
}

// LoadSettings reads output lifecycle settings from config.toml and
// repo_config.toml under root. Missing files keep compiled defaults.
func LoadSettings(root string) (Settings, error) {
	settings := DefaultSettings()

	cleanRoot, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return Settings{}, fmt.Errorf("resolve output settings root: %w", err)
	}

	for _, name := range []string{"config.toml", "repo_config.toml"} {
		path := filepath.Join(cleanRoot, name)

		config, err := configdata.LoadTOMLMap(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}

			return Settings{}, fmt.Errorf("load output settings %s: %w", path, err)
		}

		err = applyOutputSettingsMap(&settings, config, name)
		if err != nil {
			return Settings{}, err
		}
	}

	return settings, nil
}

// DefaultSettings returns conservative output lifecycle defaults.
func DefaultSettings() Settings {
	settings := Settings{
		Report: ReportSettings{
			DefaultFormat: defaultReportFormat,
		},
		Prune: PruneSettings{
			Enabled:              true,
			AutoEnabled:          true,
			RequireApplyForPrune: true,
			WritePruneTrace:      true,
			Surfaces:             map[string]SurfaceRetentionPolicy{},
		},
	}

	for _, definition := range Definitions() {
		settings.Prune.Surfaces[definition.ID] = SurfaceRetentionPolicy{
			Enabled:                definition.CommandPrune,
			Auto:                   definition.AutomaticPrune,
			MaxAge:                 definition.maxAge,
			MaxAgeText:             durationText(definition.maxAge),
			MaxBytes:               defaultMaxBytes,
			RequireCodeIntelIngest: definition.RequiresIngest,
			VacuumAfterPrune:       definition.DBMaintenance,
		}
	}

	return settings
}

func applyOutputSettingsMap(
	settings *Settings,
	config configdata.Map,
	file string,
) error {
	outputs := configdata.MapValue(config["outputs"])
	if outputs == nil {
		return nil
	}

	for key, value := range outputs {
		switch key {
		case "report":
			report := configdata.MapValue(value)
			if report == nil {
				return fmt.Errorf("%w: %s", errReportConfigMustBeTable, file)
			}

			err := applyReportSettings(&settings.Report, report, file)
			if err != nil {
				return err
			}
		case "prune":
			prune := configdata.MapValue(value)
			if prune == nil {
				return fmt.Errorf("%w: %s", errPruneConfigMustBeTable, file)
			}

			err := applyPruneSettings(&settings.Prune, prune, file)
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: outputs.%s in %s", errUnknownOutputConfigPath, key, file)
		}
	}

	return nil
}

func applyReportSettings(
	settings *ReportSettings,
	values configdata.Map,
	file string,
) error {
	for key, value := range values {
		switch key {
		case "default_format":
			settings.DefaultFormat = strings.TrimSpace(fmt.Sprint(value))
		case "include_temp":
			settings.IncludeTemp = boolValue(value)
		case "include_sensitive":
			settings.IncludeSensitive = boolValue(value)
		default:
			return fmt.Errorf(
				"%w: outputs.report.%s in %s",
				errUnknownOutputConfigPath,
				key,
				file,
			)
		}
	}

	return nil
}

func applyPruneSettings(
	settings *PruneSettings,
	values configdata.Map,
	file string,
) error {
	for key, value := range values {
		switch key {
		case "enabled":
			settings.Enabled = boolValue(value)
		case "auto_enabled":
			settings.AutoEnabled = boolValue(value)
		case "require_apply_for_prune":
			settings.RequireApplyForPrune = boolValue(value)
		case "write_prune_trace":
			settings.WritePruneTrace = boolValue(value)
		case "surfaces":
			surfaces := configdata.MapValue(value)
			if surfaces == nil {
				return fmt.Errorf(
					"%w: outputs.prune.surfaces in %s",
					errPruneConfigMustBeTable,
					file,
				)
			}

			err := applySurfacePolicies(settings, surfaces, file)
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf(
				"%w: outputs.prune.%s in %s",
				errUnknownOutputConfigPath,
				key,
				file,
			)
		}
	}

	return nil
}

func applySurfacePolicies(
	settings *PruneSettings,
	values configdata.Map,
	file string,
) error {
	known := definitionIDs()
	for surfaceID, value := range values {
		if !known[surfaceID] {
			return fmt.Errorf("%w: %q in %s", errUnknownOutputSurface, surfaceID, file)
		}

		policy := settings.Surfaces[surfaceID]

		table := configdata.MapValue(value)
		if table == nil {
			return fmt.Errorf(
				"%w: outputs.prune.surfaces.%s in %s",
				errSurfaceConfigMustBeTable,
				surfaceID,
				file,
			)
		}

		err := applySurfacePolicy(&policy, surfaceID, table, file)
		if err != nil {
			return err
		}

		settings.Surfaces[surfaceID] = policy
	}

	return nil
}

func applySurfacePolicy(
	policy *SurfaceRetentionPolicy,
	surfaceID string,
	values configdata.Map,
	file string,
) error {
	for key, value := range values {
		err := applySurfacePolicyValue(policy, surfaceID, key, value, file)
		if err != nil {
			return err
		}
	}

	return nil
}

func applySurfacePolicyValue(
	policy *SurfaceRetentionPolicy,
	surfaceID string,
	key string,
	value any,
	file string,
) error {
	switch key {
	case "enabled":
		policy.Enabled = boolValue(value)
	case "auto":
		policy.Auto = boolValue(value)
	case "max_age":
		return applySurfaceMaxAge(policy, surfaceID, value, file)
	case "keep_last":
		policy.KeepLast = intValue(value)
	case "max_bytes":
		return applySurfaceMaxBytes(policy, surfaceID, value, file)
	case "require_code_intel_ingest":
		policy.RequireCodeIntelIngest = boolValue(value)
	case "vacuum_after_prune":
		policy.VacuumAfterPrune = boolValue(value)
	case "row_retention_days":
		policy.RowRetentionDays = intValue(value)
	default:
		return fmt.Errorf(
			"%w: outputs.prune.surfaces.%s.%s in %s",
			errUnknownOutputConfigPath,
			surfaceID,
			key,
			file,
		)
	}

	return nil
}

func applySurfaceMaxAge(
	policy *SurfaceRetentionPolicy,
	surfaceID string,
	value any,
	file string,
) error {
	duration, err := parseDurationText(fmt.Sprint(value))
	if err != nil {
		return fmt.Errorf(
			"parse outputs.prune.surfaces.%s.max_age in %s: %w",
			surfaceID,
			file,
			err,
		)
	}

	policy.MaxAge = duration
	policy.MaxAgeText = durationText(duration)

	return nil
}

func applySurfaceMaxBytes(
	policy *SurfaceRetentionPolicy,
	surfaceID string,
	value any,
	file string,
) error {
	bytes, err := parseBytesText(fmt.Sprint(value))
	if err != nil {
		return fmt.Errorf(
			"parse outputs.prune.surfaces.%s.max_bytes in %s: %w",
			surfaceID,
			file,
			err,
		)
	}

	policy.MaxBytes = bytes
	policy.MaxBytesText = bytesText(bytes)

	return nil
}

func definitionIDs() map[string]bool {
	ids := map[string]bool{}
	for _, definition := range Definitions() {
		ids[definition.ID] = true
	}

	return ids
}

func parseDurationText(value string) (time.Duration, error) {
	text := strings.TrimSpace(value)
	if text == "" || text == "0" {
		return 0, nil
	}

	if before, ok := strings.CutSuffix(text, "d"); ok {
		days, err := strconv.Atoi(before)
		if err != nil {
			return 0, fmt.Errorf("parse day count %q: %w", before, err)
		}

		return time.Duration(days) * hoursPerDay * time.Hour, nil
	}

	duration, err := time.ParseDuration(text)
	if err != nil {
		return 0, fmt.Errorf("parse duration %q: %w", text, err)
	}

	return duration, nil
}

// ParseDuration parses output lifecycle duration text such as 24h or 30d.
func ParseDuration(value string) (time.Duration, error) {
	return parseDurationText(value)
}

func parseBytesText(value string) (int64, error) {
	text := strings.TrimSpace(value)
	if text == "" || text == "0" {
		return 0, nil
	}

	multiplier := int64(1)

	for suffix, factor := range map[string]int64{
		"KiB": kibibyte,
		"MiB": kibibyte * kibibyte,
		"GiB": kibibyte * kibibyte * kibibyte,
		"KB":  kilobyte,
		"MB":  kilobyte * kilobyte,
		"GB":  kilobyte * kilobyte * kilobyte,
	} {
		if before, ok := strings.CutSuffix(text, suffix); ok {
			text = strings.TrimSpace(before)
			multiplier = factor

			break
		}
	}

	valueInt, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse byte count %q: %w", text, err)
	}

	return valueInt * multiplier, nil
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

func bytesText(value int64) string {
	if value == 0 {
		return ""
	}

	return strconv.FormatInt(value, 10)
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

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case int32:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return parsed
		}
	}

	return 0
}
