// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package policy

import "testing"

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
