// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import "testing"

func TestCommandHasDynamicExecutableIgnoresDataCommandSubstitution(t *testing.T) {
	t.Parallel()

	command := `for id in $(comm -12 <(comm -23 /tmp/registry_ids.txt /tmp/phase1_ids.txt) /tmp/nquads_ids.txt); do f="/opt/foundation/ontologies/nquads/${id}.nq"; if [ -f "$f" ]; then sz=$(du -h "$f" | cut -f1); lines=$(wc -l < "$f"); type=$(grep -A2 "id: $id" /opt/foundation/ontologies/SOURCE_REGISTRY.yaml | grep 'type:' | awk '{print $2}'); printf "%-45s %8s %10s lines [%s]\n" "$id" "$sz" "$lines" "$type"; fi; done | sort -k4`

	if commandHasDynamicExecutable(command) {
		t.Fatal("data command substitution was classified as a dynamic executable")
	}
}

func TestCommandHasDynamicExecutableBlocksParameterExecutable(t *testing.T) {
	t.Parallel()

	for _, command := range []string{
		"$GIT status",
		"${A}${B}${C} status",
	} {
		if !commandHasDynamicExecutable(command) {
			t.Fatalf("dynamic executable %q was not detected", command)
		}
	}
}
