// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package celexpr

import "blackcat.ca/coding-ethos/go/toolcatalog"

type ToolCapabilityInput struct {
	SeccompProfile    string   `json:"seccomp_profile"`
	SandboxProfile    string   `json:"sandbox_profile"`
	Name              string   `json:"name"`
	Command           []string `json:"command"`
	Tags              []string `json:"tags"`
	ReadPaths         []string `json:"read_paths"`
	WritePaths        []string `json:"write_paths"`
	TimeoutSeconds    int64    `json:"timeout_seconds"`
	CPUQuotaPercent   int64    `json:"cpu_quota_percent"`
	MemoryMB          int64    `json:"memory_mb"`
	RequiresNetwork   bool     `json:"requires_network"`
	RequiresGit       bool     `json:"requires_git"`
	RequiresEnv       bool     `json:"requires_env"`
	RequiresProcesses bool     `json:"requires_processes"`
}

func toolCapabilityInputs() []ToolCapabilityInput {
	views := toolcatalog.ToolCapabilityViews()

	inputs := make([]ToolCapabilityInput, 0, len(views))
	for _, view := range views {
		inputs = append(inputs, ToolCapabilityInput{
			Name:              view.Name,
			Command:           append([]string(nil), view.Command...),
			Tags:              append([]string(nil), view.Tags...),
			ReadPaths:         append([]string(nil), view.ReadPaths...),
			WritePaths:        append([]string(nil), view.WritePaths...),
			SandboxProfile:    view.SandboxProfile,
			TimeoutSeconds:    int64(view.TimeoutSeconds),
			MemoryMB:          int64(view.MemoryMB),
			CPUQuotaPercent:   int64(view.CPUQuotaPercent),
			RequiresNetwork:   view.RequiresNetwork,
			RequiresGit:       view.RequiresGit,
			RequiresEnv:       view.RequiresEnv,
			RequiresProcesses: view.RequiresProcesses,
			SeccompProfile:    view.SeccompProfile,
		})
	}

	return inputs
}
