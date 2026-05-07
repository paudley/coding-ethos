// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

type compiledQuietFilter struct {
	ansi             *regexp.Regexp
	failed           *regexp.Regexp
	metadataPrefixes []string
	passed           *regexp.Regexp
	preexisting      *regexp.Regexp
	separator        *regexp.Regexp
	skipped          *regexp.Regexp
	status           *regexp.Regexp
	suppressExact    map[string]bool
	suppressPrefixes []string
	suppressRegexes  []*regexp.Regexp
	bannerWidth      int
}

func quietFilter(cfg Config, _ []string) int {
	filter, err := compileQuietFilter(cfg.QuietFilter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	return runQuietFilter(filter, os.Stdin, os.Stdout)
}

func compileQuietFilter(cfg QuietFilterConfig) (compiledQuietFilter, error) {
	filter := compiledQuietFilter{
		bannerWidth:      cfg.BannerWidth,
		metadataPrefixes: cfg.MetadataPrefixes,
		suppressExact:    stringSet(cfg.SuppressExact),
		suppressPrefixes: cfg.SuppressPrefixes,
	}
	if filter.bannerWidth == 0 {
		filter.bannerWidth = reportDividerWidth
	}

	err := compileQuietFilterRegexes(cfg, &filter)
	if err != nil {
		return filter, err
	}

	return compileQuietFilterSuppressions(cfg, filter)
}

func compileQuietFilterRegexes(
	cfg QuietFilterConfig,
	filter *compiledQuietFilter,
) error {
	var err error

	filter.ansi, err = compileConfiguredRegex(
		"quiet_filter.ansi_regex",
		cfg.ANSIRegex,
	)
	if err != nil {
		return err
	}

	filter.passed, err = compileConfiguredRegex(
		"quiet_filter.passed_regex",
		cfg.PassedRegex,
	)
	if err != nil {
		return err
	}

	filter.skipped, err = compileConfiguredRegex(
		"quiet_filter.skipped_regex",
		cfg.SkippedRegex,
	)
	if err != nil {
		return err
	}

	filter.failed, err = compileConfiguredRegex(
		"quiet_filter.failed_regex",
		cfg.FailedRegex,
	)
	if err != nil {
		return err
	}

	filter.status, err = compileConfiguredRegex(
		"quiet_filter.status_regex",
		cfg.StatusRegex,
	)
	if err != nil {
		return err
	}

	filter.preexisting, err = compileConfiguredRegex(
		"quiet_filter.preexisting_regex",
		cfg.PreexistingRegex,
	)
	if err != nil {
		return err
	}

	filter.separator, err = compileConfiguredRegex(
		"quiet_filter.separator_regex",
		cfg.SeparatorRegex,
	)

	return err
}

func compileQuietFilterSuppressions(
	cfg QuietFilterConfig,
	filter compiledQuietFilter,
) (compiledQuietFilter, error) {
	for i, pattern := range cfg.SuppressRegexes {
		compiled, err := compileConfiguredRegex(
			fmt.Sprintf("quiet_filter.suppress_regexes[%d]", i),
			pattern,
		)
		if err != nil {
			return filter, err
		}

		filter.suppressRegexes = append(filter.suppressRegexes, compiled)
	}

	return filter, nil
}

func compileConfiguredRegex(name, pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return regexp.MustCompile(`a^`), nil
	}

	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}

	return compiled, nil
}

func runQuietFilter(filter compiledQuietFilter, input io.Reader, output io.Writer) int {
	state := newQuietFilterState(filter, output)
	scanner := bufio.NewScanner(input)
	scanner.Buffer(
		make([]byte, 0, scannerBufferCapacity),
		scannerTokenLimit,
	)

	for scanner.Scan() {
		state.processLine(scanner.Text())
	}

	err := scanner.Err()
	if err != nil {
		fmt.Fprintf(os.Stderr, "quiet-filter: %v\n", err)

		return 1
	}

	state.printSummary()

	return 0
}

type quietFilterState struct {
	output                io.Writer
	seenBanners           map[string]bool
	filter                compiledQuietFilter
	passed                int
	skipped               int
	failed                int
	suppressHowToFix      bool
	suppressBannerContent bool
	lastWasSeparator      bool
	lastWasBlank          bool
	suppressMeta          bool
	suppressPreexisting   bool
}

func newQuietFilterState(
	filter compiledQuietFilter,
	output io.Writer,
) *quietFilterState {
	return &quietFilterState{
		filter:      filter,
		output:      output,
		seenBanners: map[string]bool{},
	}
}

func (state *quietFilterState) processLine(line string) {
	clean := state.filter.ansi.ReplaceAllString(line, "")
	if state.consumeStatus(clean) ||
		state.consumeMetadata(clean) ||
		shouldSuppressQuietLine(state.filter, clean) ||
		state.consumePreexisting(clean) ||
		state.consumeSeparator(line, clean) ||
		state.consumeBannerContent(clean) ||
		state.consumeHowToFix(line, clean) ||
		state.consumeBlank(clean) {
		return
	}

	_, _ = fmt.Fprintln(state.output, line)
}

func (state *quietFilterState) consumeStatus(clean string) bool {
	if state.filter.passed.MatchString(clean) {
		state.passed++
		state.suppressMeta = true

		return true
	}

	if state.filter.skipped.MatchString(clean) {
		state.skipped++
		state.suppressMeta = true

		return true
	}

	if state.filter.failed.MatchString(clean) {
		state.failed++
	}

	return false
}

func (state *quietFilterState) consumeMetadata(clean string) bool {
	if !state.suppressMeta {
		return false
	}

	if clean == "" || hasPrefix(clean, state.filter.metadataPrefixes) {
		return true
	}

	state.suppressMeta = false

	return false
}

func (state *quietFilterState) consumePreexisting(clean string) bool {
	if state.filter.preexisting.MatchString(clean) {
		state.suppressPreexisting = true

		return true
	}

	if !state.suppressPreexisting {
		return false
	}

	if strings.HasPrefix(clean, " ") || clean == "" {
		return true
	}

	state.suppressPreexisting = false

	return false
}

func (state *quietFilterState) consumeSeparator(line, clean string) bool {
	if state.filter.separator.MatchString(clean) {
		state.lastWasSeparator = true

		return true
	}

	if !state.lastWasSeparator || clean == "" {
		state.lastWasSeparator = false

		return false
	}

	state.lastWasSeparator = false
	if state.isBannerHeading(clean) {
		return state.handleBannerHeading(line, clean)
	}

	_, _ = fmt.Fprintln(state.output, strings.Repeat("=", state.filter.bannerWidth))
	_, _ = fmt.Fprintln(state.output, line)

	return true
}

func (state *quietFilterState) isBannerHeading(clean string) bool {
	return !strings.HasPrefix(clean, "-") && !startsWithDigit(clean)
}

func (state *quietFilterState) handleBannerHeading(line, clean string) bool {
	if state.seenBanners[clean] {
		state.suppressBannerContent = true

		return true
	}

	state.seenBanners[clean] = true
	if clean == "How to fix:" {
		state.seenBanners["howtofix"] = true
	}

	_, _ = fmt.Fprintln(state.output, strings.Repeat("=", state.filter.bannerWidth))
	_, _ = fmt.Fprintln(state.output, line)
	_, _ = fmt.Fprintln(state.output, strings.Repeat("=", state.filter.bannerWidth))

	return true
}

func (state *quietFilterState) consumeBannerContent(clean string) bool {
	if !state.suppressBannerContent {
		return false
	}

	if state.filter.status.MatchString(clean) {
		state.suppressBannerContent = false

		return false
	}

	return true
}

func (state *quietFilterState) consumeHowToFix(line, clean string) bool {
	if clean == "How to fix:" {
		if !state.seenBanners["howtofix"] {
			state.seenBanners["howtofix"] = true
			_, _ = fmt.Fprintln(state.output, line)
			state.suppressHowToFix = false
		} else {
			state.suppressHowToFix = true
		}

		return true
	}

	if !state.suppressHowToFix {
		return false
	}

	if strings.HasPrefix(clean, " ") || clean == "" {
		return true
	}

	state.suppressHowToFix = false

	return false
}

func (state *quietFilterState) consumeBlank(clean string) bool {
	if clean == "" {
		if state.lastWasBlank {
			return true
		}

		state.lastWasBlank = true
	} else {
		state.lastWasBlank = false
	}

	return false
}

func (state *quietFilterState) printSummary() {
	if state.failed == 0 {
		return
	}

	parts := make([]string, 0, quietSummaryParts)
	if state.passed > 0 {
		parts = append(parts, fmt.Sprintf("\033[32m%d passed\033[0m", state.passed))
	}

	parts = append(parts, fmt.Sprintf("\033[31m%d failed\033[0m", state.failed))
	if state.skipped > 0 {
		parts = append(parts, fmt.Sprintf("\033[33m%d skipped\033[0m", state.skipped))
	}

	_, _ = fmt.Fprintf(state.output, "  (%s)\n", strings.Join(parts, ", "))
}

func shouldSuppressQuietLine(filter compiledQuietFilter, clean string) bool {
	if filter.suppressExact[clean] {
		return true
	}

	if hasPrefix(clean, filter.suppressPrefixes) {
		return true
	}

	for _, pattern := range filter.suppressRegexes {
		if pattern.MatchString(clean) {
			return true
		}
	}

	return false
}

func startsWithDigit(value string) bool {
	return value != "" && value[0] >= '0' && value[0] <= '9'
}
