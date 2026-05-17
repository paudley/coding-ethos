// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

const geminiCacheKeyDomain = "coding-ethos-gemini-cache-key-v1"

func geminiCacheKey(
	settings geminiRequestSettings,
	prompt string,
	dependency string,
) string {
	thinkingBudget := "unset"
	if settings.ThinkingBudget != nil {
		thinkingBudget = strconv.Itoa(*settings.ThinkingBudget)
	}

	payload := strings.Join(
		[]string{
			"v1",
			settings.CheckName,
			settings.Model,
			settings.ServiceTier,
			thinkingBudget,
			strconv.FormatBool(settings.DisableSafetyFilters),
			dependency,
			prompt,
		},
		"\x00",
	)
	mac := hmac.New(sha256.New, []byte(geminiCacheKeyDomain))
	_, _ = mac.Write([]byte(payload))

	return hex.EncodeToString(mac.Sum(nil))
}
