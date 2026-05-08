// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hookrunnercli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func geminiAPIKey() string {
	return strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
}

func normalizeGeminiServiceTier(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" || normalized == "unspecified" {
		return geminiServiceTierNormal, nil
	}

	switch normalized {
	case geminiServiceTierNormal, "flex", "priority":
		return normalized, nil
	default:
		return "", fmt.Errorf("%w: %q", errGeminiServiceTier, value)
	}
}

func resolveGeminiRequestSettings(
	settings GeminiSettings,
	checkName string,
	cacheDir string,
) geminiRequestSettings {
	model := strings.TrimSpace(settings.Model)
	if override := strings.TrimSpace(settings.ModelOverrides[checkName]); override != "" {
		model = override
	}

	if model == "" {
		model = geminiDefaultModel
	}

	serviceTier := settings.ServiceTier
	if override, ok := settings.ServiceTierOverrides[checkName]; ok {
		serviceTier = override
	}

	if serviceTier == "" {
		serviceTier = geminiServiceTierNormal
	}

	var thinkingBudget *int

	if settings.ThinkingBudget != nil {
		value := *settings.ThinkingBudget
		thinkingBudget = &value
	}

	if override, ok := settings.ThinkingBudgetOverrides[checkName]; ok {
		value := override
		thinkingBudget = &value
	}

	return geminiRequestSettings{
		CheckName:             checkName,
		Model:                 model,
		ServiceTier:           serviceTier,
		ThinkingBudget:        thinkingBudget,
		MaxRetries:            settings.MaxRetries,
		InitialBackoffSeconds: settings.InitialBackoffSeconds,
		DisableSafetyFilters:  settings.DisableSafetyFilters,
		Cache: geminiResponseCache{
			Enabled:    settings.Cache.Enabled,
			Dir:        cacheDir,
			TTL:        time.Duration(settings.Cache.TTLSeconds) * time.Second,
			APIEnabled: settings.Cache.APIEnabled,
			APITTL:     time.Duration(settings.Cache.APITTLSeconds) * time.Second,
		},
	}
}

func geminiModelPath(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		model = geminiDefaultModel
	}

	if !strings.HasPrefix(model, "models/") {
		return "models/" + model
	}

	return model
}

func isRetryableGeminiStatus(code int) bool {
	return code == http.StatusTooManyRequests ||
		code == http.StatusRequestTimeout ||
		code == http.StatusBadGateway ||
		code == http.StatusServiceUnavailable ||
		code == http.StatusGatewayTimeout ||
		code >= 500
}

func geminiAPIErrorMessage(body []byte, status string) string {
	var apiError geminiAPIErrorResponse

	err := json.Unmarshal(body, &apiError)
	if err == nil {
		switch {
		case apiError.Error.Message != "" && apiError.Error.Status != "":
			return fmt.Sprintf("%s (%s)", apiError.Error.Message, apiError.Error.Status)
		case apiError.Error.Message != "":
			return apiError.Error.Message
		}
	}

	text := strings.TrimSpace(string(body))
	if text == "" {
		return status
	}

	return text
}

func geminiSafetySettings(disabled bool) []geminiSafetySetting {
	if !disabled {
		return nil
	}

	return []geminiSafetySetting{
		{Category: "HARM_CATEGORY_HARASSMENT", Threshold: "OFF"},
		{Category: "HARM_CATEGORY_HATE_SPEECH", Threshold: "OFF"},
		{Category: "HARM_CATEGORY_SEXUALLY_EXPLICIT", Threshold: "OFF"},
		{Category: "HARM_CATEGORY_DANGEROUS_CONTENT", Threshold: "OFF"},
		{Category: "HARM_CATEGORY_CIVIC_INTEGRITY", Threshold: "OFF"},
	}
}

func geminiPromptWithInlineContent(template, content string) string {
	if strings.Contains(template, "{code_content}") {
		return strings.Replace(template, "{code_content}", content, 1)
	}

	if strings.TrimSpace(content) == "" {
		return template
	}

	return strings.TrimSpace(template) + "\n\n" + content
}

func geminiPromptForExplicitCachedContent(template string) string {
	replacement := strings.TrimSpace(
		"The source corpus to review is provided as cached content attached " +
			"to this request. " +
			"Analyze that cached file corpus directly; do not ask for it again.",
	)
	if strings.Contains(template, "{code_content}") {
		return strings.Replace(template, "{code_content}", replacement, 1)
	}

	return strings.TrimSpace(template) + "\n\n" + replacement
}

func geminiExplicitContentKey(model, content string) string {
	sum := sha256.Sum256([]byte(geminiModelPath(model) + "\x00" + content))

	return hex.EncodeToString(sum[:])
}

func geminiCachePath(cache geminiResponseCache, key string) string {
	return filepath.Join(cache.Dir, key+".json")
}

func geminiExplicitCachePath(cache geminiResponseCache, key string) string {
	return filepath.Join(cache.Dir, "explicit-api", key+".json")
}

func readGeminiCache(cache geminiResponseCache, key string) (string, bool, error) {
	if !cache.Enabled {
		return "", false, nil
	}

	path := geminiCachePath(cache, key)

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}

		return "", false, fmt.Errorf("read %s: %w", path, err)
	}

	var entry geminiCacheEntry

	err = json.Unmarshal(data, &entry)
	if err != nil {
		return "", false, fmt.Errorf("parse %s: %w", path, err)
	}

	createdAt, err := time.Parse(time.RFC3339Nano, entry.CreatedAt)
	if err != nil {
		return "", false, fmt.Errorf("parse %s timestamp: %w", path, err)
	}

	if cache.TTL > 0 && time.Since(createdAt) > cache.TTL {
		_ = os.Remove(path)

		return "", false, nil
	}

	return entry.Text, true, nil
}

func writeGeminiCache(cache geminiResponseCache, key, text string) error {
	if !cache.Enabled {
		return nil
	}

	err := os.MkdirAll(cache.Dir, defaultDirPerm)
	if err != nil {
		return fmt.Errorf("create cache dir %s: %w", cache.Dir, err)
	}

	entry := geminiCacheEntry{
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Text:      text,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode cache entry: %w", err)
	}

	path := geminiCachePath(cache, key)
	tempPath := fmt.Sprintf("%s.%d.tmp", path, time.Now().UnixNano())

	err = os.WriteFile(tempPath, data, defaultFilePerm)
	if err != nil {
		return fmt.Errorf("write cache temp file %s: %w", tempPath, err)
	}

	err = os.Rename(tempPath, path)
	if err != nil {
		_ = os.Remove(tempPath)

		return fmt.Errorf("rename cache file %s: %w", path, err)
	}

	return nil
}

func readGeminiExplicitCache(
	cache geminiResponseCache,
	key string,
) (string, bool, error) {
	if !cache.APIEnabled {
		return "", false, nil
	}

	path := geminiExplicitCachePath(cache, key)

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}

		return "", false, fmt.Errorf("read %s: %w", path, err)
	}

	var entry geminiExplicitCacheEntry

	err = json.Unmarshal(data, &entry)
	if err != nil {
		return "", false, fmt.Errorf("parse %s: %w", path, err)
	}

	if strings.TrimSpace(entry.Name) == "" {
		return "", false, nil
	}

	expireTime, err := time.Parse(time.RFC3339Nano, entry.ExpireTime)
	if err != nil {
		return "", false, fmt.Errorf("parse %s timestamp: %w", path, err)
	}

	if time.Now().UTC().After(expireTime) {
		_ = os.Remove(path)

		return "", false, nil
	}

	return entry.Name, true, nil
}

func writeGeminiExplicitCache(
	cache geminiResponseCache,
	key string,
	name string,
	expireTime time.Time,
) error {
	if !cache.APIEnabled {
		return nil
	}

	path := geminiExplicitCachePath(cache, key)

	err := os.MkdirAll(filepath.Dir(path), defaultDirPerm)
	if err != nil {
		return fmt.Errorf("create cache dir %s: %w", filepath.Dir(path), err)
	}

	entry := geminiExplicitCacheEntry{
		Name:       name,
		ExpireTime: expireTime.UTC().Format(time.RFC3339Nano),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode explicit cache entry: %w", err)
	}

	tempPath := fmt.Sprintf("%s.%d.tmp", path, time.Now().UnixNano())

	err = os.WriteFile(tempPath, data, defaultFilePerm)
	if err != nil {
		return fmt.Errorf("write explicit cache temp file %s: %w", tempPath, err)
	}

	err = os.Rename(tempPath, path)
	if err != nil {
		_ = os.Remove(tempPath)

		return fmt.Errorf("rename explicit cache file %s: %w", path, err)
	}

	return nil
}

func geminiDurationLiteral(duration time.Duration) string {
	if duration <= 0 {
		duration = time.Hour
	}

	return fmt.Sprintf("%.0fs", duration.Seconds())
}

func createGeminiExplicitCache(
	ctx context.Context,
	client *http.Client,
	apiKey string,
	model string,
	content string,
	ttl time.Duration,
	displayName string,
) (geminiCachedContentResponse, error) {
	var created geminiCachedContentResponse

	payload, err := geminiCachedContentPayload(model, content, ttl, displayName)
	if err != nil {
		return created, fmt.Errorf(
			"encode Gemini cachedContents.create request: %w",
			err,
		)
	}

	request, err := newGeminiCachedContentRequest(ctx, apiKey, payload)
	if err != nil {
		return created, fmt.Errorf(
			"build Gemini cachedContents.create request: %w",
			err,
		)
	}

	response, err := client.Do(request)
	if err != nil {
		return created, fmt.Errorf("gemini cachedContents.create failed: %w", err)
	}

	body, err := readGeminiCachedContentResponse(response)
	if err != nil {
		return created, err
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return created, fmt.Errorf(
			"%w: %s: %s",
			errGeminiAPIResponse,
			response.Status,
			geminiAPIErrorMessage(body, response.Status),
		)
	}

	err = json.Unmarshal(body, &created)
	if err != nil {
		return created, fmt.Errorf(
			"parse Gemini cachedContents.create response: %w",
			err,
		)
	}

	if strings.TrimSpace(created.Name) == "" {
		return created, errGeminiCreateNoName
	}

	return created, nil
}

func geminiCachedContentPayload(
	model string,
	content string,
	ttl time.Duration,
	displayName string,
) ([]byte, error) {
	payload, err := json.Marshal(geminiCachedContentCreateRequest{
		Model:       geminiModelPath(model),
		DisplayName: displayName,
		Contents: []geminiContent{{
			Parts: []geminiPart{{Text: content}},
		}},
		TTL: geminiDurationLiteral(ttl),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal Gemini cached content payload: %w", err)
	}

	return payload, nil
}

func newGeminiCachedContentRequest(
	ctx context.Context,
	apiKey string,
	payload []byte,
) (*http.Request, error) {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://generativelanguage.googleapis.com/v1beta/cachedContents",
		bytes.NewReader(payload),
	)
	if err != nil {
		return nil, fmt.Errorf("create Gemini cached content request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Goog-Api-Key", apiKey)

	return request, nil
}

func readGeminiCachedContentResponse(response *http.Response) ([]byte, error) {
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()

	if readErr != nil {
		return nil, fmt.Errorf(
			"read Gemini cachedContents.create response: %w",
			readErr,
		)
	}

	if closeErr != nil {
		return nil, fmt.Errorf(
			"close Gemini cachedContents.create response: %w",
			closeErr,
		)
	}

	return body, nil
}

func ensureGeminiExplicitCache(
	ctx context.Context,
	client *http.Client,
	apiKey string,
	seed geminiExplicitCacheSeed,
	key string,
) (string, bool) {
	if !seed.Cache.APIEnabled || strings.TrimSpace(seed.Content) == "" {
		return "", false
	}

	cachedName, ok, err := readGeminiExplicitCache(seed.Cache, key)
	if err == nil && ok {
		return cachedName, true
	}

	created, err := createGeminiExplicitCache(
		ctx,
		client,
		apiKey,
		seed.Model,
		seed.Content,
		seed.Cache.APITTL,
		"coding-ethos-"+key[:12],
	)
	if err != nil {
		return "", false
	}

	expireTime := time.Now().UTC().Add(seed.Cache.APITTL)

	parsedExpireTime, err := time.Parse(time.RFC3339Nano, created.ExpireTime)
	if err == nil {
		expireTime = parsedExpireTime
	}

	err = writeGeminiExplicitCache(seed.Cache, key, created.Name, expireTime)
	if err != nil {
		writef(
			os.Stderr,
			"WARN: failed to persist Gemini explicit cache entry: %v\n",
			err,
		)
	}

	return created.Name, true
}

func generateGeminiText(
	ctx context.Context,
	client *http.Client,
	settings geminiRequestSettings,
	apiKey string,
	prompt string,
	responseDependency string,
	cachedContent string,
) (string, error) {
	cacheKey := geminiCacheKey(settings, prompt, responseDependency)

	cachedText, ok, err := readGeminiCache(settings.Cache, cacheKey)
	if err == nil && ok {
		return cachedText, nil
	}

	payload, err := json.Marshal(geminiTextRequest(settings, prompt, cachedContent))
	if err != nil {
		return "", fmt.Errorf("encode Gemini request: %w", err)
	}

	endpoint := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/%s:generateContent",
		geminiModelPath(settings.Model),
	)

	backoff := time.Duration(settings.InitialBackoffSeconds * float64(time.Second))
	if backoff <= 0 {
		backoff = time.Second
	}

	var lastErr error

	for attempt := 0; attempt <= settings.MaxRetries; attempt++ {
		text, retryable, requestErr := generateGeminiTextAttempt(
			ctx,
			client,
			apiKey,
			endpoint,
			payload,
			settings,
			cacheKey,
		)
		if requestErr == nil {
			return text, nil
		}

		lastErr = requestErr
		if !retryable {
			return "", lastErr
		}

		if attempt >= settings.MaxRetries {
			break
		}

		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()

			return "", fmt.Errorf("gemini request canceled: %w", ctx.Err())
		case <-timer.C:
		}

		backoff *= 2
	}

	return "", fmt.Errorf(
		"gemini request failed after %d attempts: %w",
		settings.MaxRetries+1,
		lastErr,
	)
}

func geminiTextRequest(
	settings geminiRequestSettings,
	prompt string,
	cachedContent string,
) geminiRequest {
	request := geminiRequest{
		Contents: []geminiContent{{
			Parts: []geminiPart{{Text: prompt}},
		}},
		GenerationConfig: geminiGenerationConfig{
			ResponseMIMEType: "application/json",
		},
		CachedContent:  cachedContent,
		ServiceTier:    settings.ServiceTier,
		SafetySettings: geminiSafetySettings(settings.DisableSafetyFilters),
	}
	if settings.ThinkingBudget != nil {
		request.GenerationConfig.ThinkingConfig = &geminiThinkingConfig{
			ThinkingBudget: *settings.ThinkingBudget,
		}
	}

	return request
}

func generateGeminiTextAttempt(
	ctx context.Context,
	client *http.Client,
	apiKey string,
	endpoint string,
	payload []byte,
	settings geminiRequestSettings,
	cacheKey string,
) (string, bool, error) {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint,
		bytes.NewReader(payload),
	)
	if err != nil {
		return "", false, fmt.Errorf("build Gemini request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Goog-Api-Key", apiKey)

	response, err := client.Do(request)
	if err != nil {
		return "", true, fmt.Errorf("gemini request failed: %w", err)
	}

	defer func() {
		_ = response.Body.Close()
	}()

	return parseGeminiTextResponse(response, settings, cacheKey)
}

func parseGeminiTextResponse(
	response *http.Response,
	settings geminiRequestSettings,
	cacheKey string,
) (string, bool, error) {
	body, err := readGeminiResponseBody(response)
	if err != nil {
		return "", true, err
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		requestErr := fmt.Errorf(
			"%w: %s: %s",
			errGeminiAPIResponse,
			response.Status,
			geminiAPIErrorMessage(body, response.Status),
		)

		return "", isRetryableGeminiStatus(response.StatusCode), requestErr
	}

	text, err := decodeGeminiResponseText(body)
	if err != nil {
		return "", false, err
	}

	cacheErr := writeGeminiCache(settings.Cache, cacheKey, text)
	if cacheErr != nil {
		writef(
			os.Stderr,
			"WARN: failed to persist Gemini response cache: %v\n",
			cacheErr,
		)
	}

	return text, false, nil
}

func readGeminiResponseBody(response *http.Response) ([]byte, error) {
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read Gemini response: %w", err)
	}

	return body, nil
}

func decodeGeminiResponseText(body []byte) (string, error) {
	var parsed geminiGenerateResponse

	err := json.Unmarshal(body, &parsed)
	if err != nil {
		return "", fmt.Errorf("parse Gemini API response: %w", err)
	}

	text := extractGeminiText(parsed)
	if text == "" {
		return "", errGeminiAPINoText
	}

	return text, nil
}

func extractGeminiText(response geminiGenerateResponse) string {
	for _, candidate := range response.Candidates {
		parts := make([]string, 0, len(candidate.Content.Parts))
		for _, part := range candidate.Content.Parts {
			if strings.TrimSpace(part.Text) != "" {
				parts = append(parts, part.Text)
			}
		}

		if len(parts) > 0 {
			return strings.Join(parts, "")
		}
	}

	return ""
}

func stripGeminiCodeFence(text string) string {
	cleaned := strings.TrimSpace(text)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)
	cleaned = strings.TrimSuffix(cleaned, "```")

	return strings.TrimSpace(cleaned)
}

func parseGeminiResult(responseText string) (geminiResult, error) {
	var result geminiResult

	cleaned := stripGeminiCodeFence(responseText)

	if strings.HasPrefix(cleaned, "[") {
		var violations []geminiViolation

		err := json.Unmarshal([]byte(cleaned), &violations)
		if err != nil {
			return result, fmt.Errorf("parse Gemini JSON response: %w", err)
		}

		result.Violations = violations
	} else {
		err := json.Unmarshal([]byte(cleaned), &result)
		if err != nil {
			return result, fmt.Errorf("parse Gemini JSON response: %w", err)
		}
	}

	if result.Verdict == "" {
		result.Verdict = passVerdict
	}

	for index := range result.Violations {
		result.Violations[index].Severity = strings.ToUpper(
			strings.TrimSpace(result.Violations[index].Severity),
		)
		if result.Violations[index].Severity == "" {
			result.Violations[index].Severity = severityInfo
		}

		result.Violations[index].File = normalizeGeminiPath(
			result.Violations[index].File,
		)
		result.Violations[index].Message = strings.TrimSpace(
			result.Violations[index].Message,
		)

		result.Violations[index].EthosSection = strings.TrimSpace(
			result.Violations[index].EthosSection,
		)
		if result.Violations[index].Line < 0 {
			result.Violations[index].Line = 0
		}
	}

	return result, nil
}
