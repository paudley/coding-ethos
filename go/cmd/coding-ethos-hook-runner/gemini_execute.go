// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"net/http"
	"sync"
	"time"
)

func executeGeminiChecks(
	settings GeminiSettings,
	apiKey string,
	prepared []geminiPreparedCheck,
	changedLinesByFile map[string]map[int]struct{},
) []geminiCheckOutcome {
	client := &http.Client{
		Timeout: time.Duration(settings.TimeoutSeconds) * time.Second,
	}

	return executeGeminiChecksWithClient(
		settings,
		apiKey,
		prepared,
		changedLinesByFile,
		client,
	)
}

func executeGeminiChecksWithClient(
	settings GeminiSettings,
	apiKey string,
	prepared []geminiPreparedCheck,
	changedLinesByFile map[string]map[int]struct{},
	client *http.Client,
) []geminiCheckOutcome {
	patterns := normalizedGeminiModalAllowlistPatterns(settings)
	explicitCacheBindings := buildGeminiExplicitCacheBindings(client, apiKey, prepared)

	outcomes, jobs := initializeGeminiOutcomesAndJobs(prepared)
	if len(jobs) == 0 {
		return outcomes
	}

	semaphore := make(chan struct{}, maxGeminiConcurrency(settings))
	results := make(chan geminiBatchJobResult, len(jobs))

	var waitGroup sync.WaitGroup

	for _, job := range jobs {
		waitGroup.Add(1)

		go func(job geminiBatchJob) {
			defer waitGroup.Done()

			semaphore <- struct{}{}

			defer func() {
				<-semaphore
			}()

			results <- geminiBatchJobResult{
				CheckIndex: job.CheckIndex,
				BatchIndex: job.BatchIndex,
				Outcome: executeGeminiBatchJob(
					client,
					apiKey,
					job,
					explicitCacheBindings,
					patterns,
				),
			}
		}(job)
	}

	go func() {
		waitGroup.Wait()
		close(results)
	}()

	collectGeminiBatchResults(outcomes, results)
	finalizeGeminiOutcomes(outcomes, changedLinesByFile)

	return outcomes
}
