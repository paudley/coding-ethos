// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy

func CodeDefenseLayers() DefenseLayers {
	return DefenseLayers{
		Persuade:  true,
		Intercept: "advise",
		Detect:    "block",
		Enforce:   "pre_commit",
		Notify:    "on_failure",
		Record:    true,
	}
}

func GitDefenseLayers(
	intercept string,
	mediate string,
	detect string,
	enforce string,
	verify string,
) DefenseLayers {
	return DefenseLayers{
		Persuade:  true,
		Intercept: intercept,
		Mediate:   mediate,
		Detect:    detect,
		Enforce:   enforce,
		Verify:    verify,
		Notify:    "on_block",
		Record:    true,
	}
}

func GeneratedConfigDefenseLayers() DefenseLayers {
	return DefenseLayers{
		Persuade:  true,
		Intercept: "ask",
		Detect:    "block",
		Enforce:   "pre_commit",
		Notify:    "on_failure",
		Record:    true,
	}
}

func PytestDefenseLayers() DefenseLayers {
	return DefenseLayers{
		Persuade:  true,
		Intercept: "prepare",
		Detect:    "block",
		Enforce:   "pre_commit",
		Notify:    "on_failure",
		Record:    true,
	}
}
