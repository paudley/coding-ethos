// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package ca

import (
	"time"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
)

// GateInput carries the configuration and environment signals the opt-in
// HTTPS-interception gate evaluates before provisioning or trusting a CA.
type GateInput struct {
	Now        time.Time
	Mode       string
	CAApproval string
	RepoRoot   string
	EnvOptIn   bool
}

// Evaluate applies the fail-closed opt-in gate and returns interception
// evidence. The error return signals genuine IO faults during provisioning;
// a disabled or denied decision is reported through the evidence value.
func Evaluate(input GateInput) (agentproxy.InterceptionEvidence, error) {
	if input.Mode != agentproxy.InterceptionModeRequired {
		return disabledEvidence(input.Mode), nil
	}

	if !input.EnvOptIn {
		return staleConfigEvidence(), nil
	}

	priorFingerprint := priorCAFingerprint(input.RepoRoot)

	authority, err := EnsureCA(input.RepoRoot, input.Now)
	if err != nil {
		return agentproxy.InterceptionEvidence{}, err
	}

	if approvalMismatch(priorFingerprint, input.CAApproval, authority.Fingerprint()) {
		return approvalMismatchEvidence(), nil
	}

	return agentproxy.InterceptionEvidence{
		Mode:          agentproxy.InterceptionModeRequired,
		Enabled:       true,
		CAFingerprint: authority.Fingerprint(),
		CACertPath:    authority.CertPath(),
		Reason:        "interception enabled",
	}, nil
}

func disabledEvidence(mode string) agentproxy.InterceptionEvidence {
	resolvedMode := mode
	if resolvedMode == "" {
		resolvedMode = agentproxy.InterceptionModeOff
	}

	return agentproxy.InterceptionEvidence{
		Mode:    resolvedMode,
		Enabled: false,
		Denied:  false,
		Reason:  "interception disabled (mode not required)",
	}
}

func staleConfigEvidence() agentproxy.InterceptionEvidence {
	return agentproxy.InterceptionEvidence{
		Mode:    agentproxy.InterceptionModeRequired,
		Enabled: false,
		Denied:  true,
		Reason: "mode=required but CODE_ETHOS_AGENT_PROXY_INTERCEPT not set " +
			"(stale-config guard)",
	}
}

func approvalMismatchEvidence() agentproxy.InterceptionEvidence {
	return agentproxy.InterceptionEvidence{
		Mode:    agentproxy.InterceptionModeRequired,
		Enabled: false,
		Denied:  true,
		Reason:  "ca_approval does not match provisioned CA fingerprint",
	}
}

func priorCAFingerprint(repoRoot string) string {
	authority, err := Load(repoRoot)
	if err != nil {
		return ""
	}

	return authority.Fingerprint()
}

func approvalMismatch(priorFingerprint, approval, fingerprint string) bool {
	return priorFingerprint != "" && approval != "" && approval != fingerprint
}
