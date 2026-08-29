// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//nolint:cyclop,exhaustive,gocognit,gocyclo,lll,mnd,wsl_v5 // Statistical gates stay reviewable.
package tokeneconomy

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"time"
)

const (
	bootstrapSamples            = 10_000
	familyWiseAlpha             = 0.05
	minimumCausalTaskCount      = 10
	precisionHalfWidthPoints    = 10.0
	qualityNoninferiorityPoints = -5.0
)

// ExperimentReport compares completed benchmark arms for one experiment.
func (store *Store) ExperimentReport(
	ctx context.Context,
	experimentID string,
	now time.Time,
) (Report, error) {
	experiment, err := store.Experiment(ctx, experimentID)
	if err != nil {
		return Report{}, err
	}

	runs, err := store.Runs(ctx, experimentID)
	if err != nil {
		return Report{}, err
	}

	coverage := experimentCoverage(experiment, runs)
	checkpointCount := max(len(experiment.AnalysisBlockCheckpoints), 1)
	intervalAlpha := familyWiseAlpha / float64(checkpointCount)
	comparisons := []Comparison{}
	for _, arms := range [][2]Arm{
		{ArmFull, ArmOff},
		{ArmFull, ArmStatic},
		{ArmStatic, ArmOff},
	} {
		comparison, found := compareArms(
			experimentID,
			runs,
			arms[0],
			arms[1],
			intervalAlpha,
		)
		if found {
			comparisons = append(comparisons, comparison)
		}
	}

	causal := experiment.Randomized &&
		experiment.ArmIsolationVerified &&
		coverage.PartialBlockCount == 0 &&
		coverage.MissingLedgerRuns == 0 &&
		coverage.MissingValidationRuns == 0 &&
		coverage.CompleteTaskCount >= minimumCausalTaskCount &&
		slices.Contains(
			experiment.AnalysisBlockCheckpoints,
			coverage.CompleteBlockCount,
		) &&
		allArmsPresent(coverage.Arms)

	conclusion := ConclusionInconclusive
	if primary, found := comparisonFor(comparisons, ArmFull, ArmOff); found && causal {
		conclusion = comparisonConclusion(primary)
	}

	return Report{
		Kind:           ReportKind,
		Cohort:         experimentID,
		GeneratedAtUTC: reportTimestamp(now),
		Conclusion:     conclusion,
		SchemaVersion:  SchemaVersion,
		Causal:         causal,
		Coverage:       coverage,
		Comparisons:    comparisons,
		Provenance: map[string]string{
			"manifest_sha256": experiment.ManifestSHA256,
			"protocol_sha256": experiment.ProtocolSHA256,
			"runtime_version": experiment.RuntimeVersion,
			"provider":        string(experiment.Provider),
			"model":           experiment.Model,
		},
	}, nil
}

func experimentCoverage(experiment Experiment, runs []Run) Coverage {
	coverage := Coverage{
		Providers: []Provider{},
		Arms:      []Arm{},
		Reasons:   []string{},
		RunCount:  len(runs),
	}
	tasks := map[string]struct{}{}
	blocks := map[string]coverageBlockEvidence{}

	for _, run := range runs {
		tasks[run.TaskID] = struct{}{}
		blockID := fmt.Sprintf("%s\x00%d", run.TaskID, run.Replicate)
		block := blocks[blockID]
		if block.arms == nil {
			block = coverageBlockEvidence{arms: map[Arm]int{}, taskID: run.TaskID}
		}
		block.arms[run.Arm]++
		blocks[blockID] = block
		if !slices.Contains(coverage.Providers, run.Provider) {
			coverage.Providers = append(coverage.Providers, run.Provider)
		}
		if !slices.Contains(coverage.Arms, run.Arm) {
			coverage.Arms = append(coverage.Arms, run.Arm)
		}
		if run.Accepted {
			coverage.AcceptedRunCount++
		}
		if strings.TrimSpace(run.LedgerSHA256) == "" {
			coverage.MissingLedgerRuns++
		}
		if strings.TrimSpace(run.ValidationReceiptSHA256) == "" {
			coverage.MissingValidationRuns++
		}
	}

	coverage.TaskCount = len(tasks)
	coverage.CompleteTaskCount,
		coverage.CompleteBlockCount,
		coverage.PartialBlockCount = summarizeCoverageBlocks(blocks)
	slices.Sort(coverage.Providers)
	slices.Sort(coverage.Arms)
	coverage.Reasons = experimentCoverageReasons(experiment, coverage)

	return coverage
}

type coverageBlockEvidence struct {
	arms   map[Arm]int
	taskID string
}

func summarizeCoverageBlocks(blocks map[string]coverageBlockEvidence) (int, int, int) {
	completeTasks := map[string]struct{}{}
	completeBlocks := 0
	partialBlocks := 0
	for _, block := range blocks {
		full := block.arms[ArmFull]
		static := block.arms[ArmStatic]
		off := block.arms[ArmOff]
		if full == 1 && static == 1 && off == 1 && len(block.arms) == 3 {
			completeBlocks++
			completeTasks[block.taskID] = struct{}{}
		} else {
			partialBlocks++
		}
	}

	return len(completeTasks), completeBlocks, partialBlocks
}

func experimentCoverageReasons(experiment Experiment, coverage Coverage) []string {
	reasons := []string{}
	if !experiment.Randomized {
		reasons = append(reasons, "experiment is not randomized")
	}
	if !experiment.ArmIsolationVerified {
		reasons = append(reasons, "arm isolation is not verified")
	}
	if coverage.CompleteTaskCount < minimumCausalTaskCount {
		reasons = append(
			reasons,
			fmt.Sprintf(
				"fewer than %d complete three-arm tasks are available",
				minimumCausalTaskCount,
			),
		)
	}
	if !slices.Contains(
		experiment.AnalysisBlockCheckpoints,
		coverage.CompleteBlockCount,
	) {
		reasons = append(
			reasons,
			"complete block count is not a predeclared analysis checkpoint",
		)
	}
	if !allArmsPresent(coverage.Arms) {
		reasons = append(reasons, "one or more benchmark arms are missing")
	}
	if coverage.PartialBlockCount != 0 {
		reasons = append(
			reasons,
			"one or more three-arm blocks are partial",
		)
	}
	if coverage.MissingLedgerRuns != 0 {
		reasons = append(reasons, "provider ledger evidence is incomplete")
	}
	if coverage.MissingValidationRuns != 0 {
		reasons = append(reasons, "validation receipts are incomplete")
	}

	return reasons
}

type taskCluster struct {
	TaskID string
	Runs   []Run
}

func compareArms(
	experimentID string,
	runs []Run,
	treatment Arm,
	control Arm,
	intervalAlpha float64,
) (Comparison, bool) {
	clusters := comparisonClusters(runs, treatment, control)
	if len(clusters) == 0 {
		return Comparison{}, false
	}

	observed := aggregateClusters(clusters, treatment, control)
	if observed.treatmentAccepted == 0 || observed.controlAccepted == 0 {
		comparison := observed.comparison(
			treatment,
			control,
			Interval{},
			Interval{},
			intervalAlpha,
		)
		comparison.PrecisionMet = false
		comparison.QualityNonInferior = false

		return comparison, true
	}

	savings, acceptance := bootstrapIntervals(
		experimentID,
		clusters,
		treatment,
		control,
		intervalAlpha,
	)

	return observed.comparison(
		treatment,
		control,
		savings,
		acceptance,
		intervalAlpha,
	), true
}

func comparisonClusters(runs []Run, treatment, control Arm) []taskCluster {
	byTask := map[string][]Run{}
	for _, run := range runs {
		if run.Arm == treatment || run.Arm == control {
			byTask[run.TaskID] = append(byTask[run.TaskID], run)
		}
	}

	taskIDs := make([]string, 0, len(byTask))
	for taskID, taskRuns := range byTask {
		if armRunCount(taskRuns, treatment) > 0 && armRunCount(taskRuns, control) > 0 {
			taskIDs = append(taskIDs, taskID)
		}
	}
	slices.Sort(taskIDs)

	clusters := make([]taskCluster, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		clusters = append(clusters, taskCluster{TaskID: taskID, Runs: byTask[taskID]})
	}

	return clusters
}

func armRunCount(runs []Run, arm Arm) int {
	count := 0
	for _, run := range runs {
		if run.Arm == arm {
			count++
		}
	}

	return count
}

type aggregate struct {
	taskCount         int
	runCount          int
	treatmentTokens   int64
	controlTokens     int64
	treatmentAccepted int
	controlAccepted   int
	treatmentRuns     int
	controlRuns       int
	treatmentSevere   int
	controlSevere     int
}

func aggregateClusters(clusters []taskCluster, treatment, control Arm) aggregate {
	result := aggregate{taskCount: len(clusters)}
	for _, cluster := range clusters {
		for _, run := range cluster.Runs {
			switch run.Arm {
			case treatment:
				result.treatmentTokens += run.Usage.TotalTokens
				result.treatmentRuns++
				if run.Accepted {
					result.treatmentAccepted++
				}
				if run.SevereGovernanceViolation {
					result.treatmentSevere++
				}
			case control:
				result.controlTokens += run.Usage.TotalTokens
				result.controlRuns++
				if run.Accepted {
					result.controlAccepted++
				}
				if run.SevereGovernanceViolation {
					result.controlSevere++
				}
			default:
				continue
			}
		}
	}
	result.runCount = result.treatmentRuns + result.controlRuns

	return result
}

func (value aggregate) comparison(
	treatment Arm,
	control Arm,
	savings Interval,
	acceptance Interval,
	intervalAlpha float64,
) Comparison {
	treatmentRate := tokensPerAccepted(value.treatmentTokens, value.treatmentAccepted)
	controlRate := tokensPerAccepted(value.controlTokens, value.controlAccepted)
	savingsPoint := savingsPercent(treatmentRate, controlRate)
	acceptancePoint := acceptanceDifference(value)
	additionalSevere := max(value.treatmentSevere-value.controlSevere, 0)

	return Comparison{
		TreatmentArm:                 treatment,
		ControlArm:                   control,
		TaskCount:                    value.taskCount,
		AssignedRuns:                 value.runCount,
		TreatmentAccepted:            value.treatmentAccepted,
		ControlAccepted:              value.controlAccepted,
		TreatmentTokensPerAccepted:   treatmentRate,
		ControlTokensPerAccepted:     controlRate,
		SavingsPercent:               savingsPoint,
		SavingsPercentInterval:       savings,
		AcceptanceDifferencePoints:   acceptancePoint,
		AcceptanceDifferenceInterval: acceptance,
		ConfidenceLevelPercent:       100 * (1 - intervalAlpha),
		PrecisionMet:                 intervalHalfWidth(savings) <= precisionHalfWidthPoints,
		QualityNonInferior: acceptance.Lower >= qualityNoninferiorityPoints &&
			additionalSevere == 0,
		AdditionalSevereViolations: additionalSevere,
	}
}

func bootstrapIntervals(
	experimentID string,
	clusters []taskCluster,
	treatment Arm,
	control Arm,
	intervalAlpha float64,
) (Interval, Interval) {
	seedHash := sha256Bytes([]byte(
		experimentID + "\x00" + string(treatment) + "\x00" + string(control),
	))
	seedOne := binary.LittleEndian.Uint64(seedHash[:8])
	seedTwo := binary.LittleEndian.Uint64(seedHash[8:16])
	//nolint:gosec // Bootstrap reproducibility requires deterministic, non-secret sampling.
	random := rand.New(rand.NewPCG(seedOne, seedTwo))
	savings := make([]float64, 0, bootstrapSamples)
	acceptance := make([]float64, 0, bootstrapSamples)

	for range bootstrapSamples {
		sample := make([]taskCluster, 0, len(clusters))
		for range clusters {
			sample = append(sample, clusters[random.IntN(len(clusters))])
		}

		value := aggregateClusters(sample, treatment, control)
		if value.treatmentAccepted == 0 || value.controlAccepted == 0 {
			continue
		}

		treatmentRate := tokensPerAccepted(value.treatmentTokens, value.treatmentAccepted)
		controlRate := tokensPerAccepted(value.controlTokens, value.controlAccepted)
		savings = append(savings, savingsPercent(treatmentRate, controlRate))
		acceptance = append(acceptance, acceptanceDifference(value))
	}

	return percentileInterval(savings, intervalAlpha), percentileInterval(
		acceptance,
		intervalAlpha,
	)
}

func percentileInterval(values []float64, alpha float64) Interval {
	if len(values) == 0 {
		return Interval{}
	}

	slices.Sort(values)
	lower := int((alpha / 2) * float64(len(values)-1))
	upper := int((1 - alpha/2) * float64(len(values)-1))

	return Interval{Lower: values[lower], Upper: values[upper]}
}

func tokensPerAccepted(tokens int64, accepted int) float64 {
	if accepted == 0 {
		return 0
	}

	return float64(tokens) / float64(accepted)
}

func savingsPercent(treatment, control float64) float64 {
	if control == 0 {
		return 0
	}

	return 100 * (control - treatment) / control
}

func acceptanceDifference(value aggregate) float64 {
	if value.treatmentRuns == 0 || value.controlRuns == 0 {
		return 0
	}

	return 100 * (float64(value.treatmentAccepted)/float64(value.treatmentRuns) -
		float64(value.controlAccepted)/float64(value.controlRuns))
}

func intervalHalfWidth(interval Interval) float64 {
	return (interval.Upper - interval.Lower) / 2
}

func allArmsPresent(arms []Arm) bool {
	return slices.Contains(arms, ArmFull) &&
		slices.Contains(arms, ArmStatic) &&
		slices.Contains(arms, ArmOff)
}

func comparisonFor(
	comparisons []Comparison,
	treatment Arm,
	control Arm,
) (Comparison, bool) {
	for _, comparison := range comparisons {
		if comparison.TreatmentArm == treatment && comparison.ControlArm == control {
			return comparison, true
		}
	}

	return Comparison{}, false
}

func comparisonConclusion(comparison Comparison) Conclusion {
	if !comparison.PrecisionMet {
		return ConclusionInconclusive
	}
	if comparison.SavingsPercentInterval.Lower > 0 {
		if !comparison.QualityNonInferior {
			return ConclusionQualityTradeoff
		}

		return ConclusionCausalSavings
	}
	if comparison.SavingsPercentInterval.Upper < 0 {
		return ConclusionTokenRegression
	}

	return ConclusionNoDifference
}

func sha256Bytes(payload []byte) [32]byte {
	return sha256.Sum256(payload)
}
