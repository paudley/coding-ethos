// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package policy

import (
	"reflect"
	"testing"
)

func TestSPDXLicenseTextURLUsesLicenseListDataTextSource(t *testing.T) {
	t.Parallel()

	got := spdxLicenseTextURL("AGPL-3.0-only")
	want := "https://raw.githubusercontent.com/" +
		"spdx/license-list-data/v3.28.0/text/AGPL-3.0-only.txt"

	if got != want {
		t.Fatalf("spdxLicenseTextURL = %q, want %q", got, want)
	}
}

func TestValidateSPDXLicenseIDRejectsPathTraversal(t *testing.T) {
	t.Parallel()

	for _, spdxID := range []string{
		"../AGPL-3.0-only",
		"..",
		"AGPL/3.0-only",
		`AGPL\3.0-only`,
		"AGPL-3.0-only..MIT",
	} {
		if err := validateSPDXLicenseID(spdxID); err == nil {
			t.Fatalf("validateSPDXLicenseID(%q) succeeded, want error", spdxID)
		}
	}
}

func TestValidateSPDXLicenseIDAllowsCommonIdentifierCharacters(t *testing.T) {
	t.Parallel()

	for _, spdxID := range []string{
		"AGPL-3.0-only",
		"GPL-2.0+",
		"GPL-2.0-or-later",
		"BSD-2-Clause",
	} {
		if err := validateSPDXLicenseID(spdxID); err != nil {
			t.Fatalf("validateSPDXLicenseID(%q): %v", spdxID, err)
		}
	}
}

func TestExampleBundleUsesOneCanonicalRequiredGateExitStatusPolicy(t *testing.T) {
	t.Parallel()

	bundle := ExampleBundle()
	want := requiredGateExitStatusRoutePolicy(bundle.Principles)
	got, ok := bundle.Policies[want.ID]
	if !ok {
		t.Fatalf("example bundle missing %q", want.ID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("example policy = %#v, want canonical policy %#v", got, want)
	}

	count := 0
	for key, candidate := range bundle.Policies {
		if candidate.ID != want.ID {
			continue
		}

		count++
		if key != want.ID {
			t.Errorf("canonical policy stored under key %q, want %q", key, want.ID)
		}
	}
	if count != 1 {
		t.Fatalf("example bundle contains %d policies with ID %q, want 1", count, want.ID)
	}
}
