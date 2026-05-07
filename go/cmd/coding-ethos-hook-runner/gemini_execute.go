// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"net/http"
	"sync"
	"time"
)

func executeGeminiChecks(
	ctx context.Context,
	settings GeminiSettings,
	apiKey string,
	prepared []geminiPreparedCheck,
	changedLinesByFile map[string]map[int]struct{},
) []geminiCheckOutcome {
	client := &http.Client{
		Timeout: time.Duration(settings.TimeoutSeconds) * time.Second,
	}

	return executeGeminiChecksWithClient(
		ctx,
		settings,
		apiKey,
		prepared,
		changedLinesByFile,
		client,
	)
}

func executeGeminiChecksWithClient(
	ctx context.Context,
	settings GeminiSettings,
	apiKey string,
	prepared []geminiPreparedCheck,
	changedLinesByFile map[string]map[int]struct{},
	client *http.Client,
) []geminiCheckOutcome {
	patterns := normalizedGeminiModalAllowlistPatterns(settings)
	explicitCacheBindings := buildGeminiExplicitCacheBindings(
		ctx,
		client,
		apiKey,
		prepared,
	)

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
					ctx,
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
	finalizeGeminiOutcomes(ctx, outcomes, changedLinesByFile)

	return outcomes
}
