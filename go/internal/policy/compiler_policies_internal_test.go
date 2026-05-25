// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package policy

import "testing"

func TestSPDXLicenseTextURLUsesLicenseListDataTextSource(t *testing.T) {
	t.Parallel()

	got := spdxLicenseTextURL("AGPL-3.0-only")
	want := "https://raw.githubusercontent.com/" +
		"spdx/license-list-data/main/text/AGPL-3.0-only.txt"

	if got != want {
		t.Fatalf("spdxLicenseTextURL = %q, want %q", got, want)
	}
}
