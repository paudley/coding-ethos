// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package minhash_test

import (
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/minhash"
)

func TestNormalizeTokensGoFunction(t *testing.T) {
	t.Parallel()

	code := `func processOrder(order *Order, total float64) error {
	if total <= 0 {
		return fmt.Errorf("invalid total: %f", total)
	}
	return nil
}`
	tokens := minhash.NormalizeTokens(code, "go")

	joined := strings.Join(tokens, " ")
	if strings.Contains(joined, "processOrder") {
		t.Fatal("identifier 'processOrder' was not normalized")
	}

	if strings.Contains(joined, "Order") {
		t.Fatal("type name 'Order' was not normalized")
	}

	if !strings.Contains(joined, "func") {
		t.Fatal("keyword 'func' was stripped")
	}

	if !strings.Contains(joined, "return") {
		t.Fatal("keyword 'return' was stripped")
	}

	if !strings.Contains(joined, minhash.PlaceholderID) {
		t.Fatal("no identifier placeholders produced")
	}

	if !strings.Contains(joined, minhash.PlaceholderNumber) {
		t.Fatal("no number placeholders produced")
	}
}

func TestNormalizeTokensPythonFunction(t *testing.T) {
	t.Parallel()

	code := `def calculate_total(items, discount=0.1):
    # Apply discount to all items
    total = sum(item.price for item in items)
    return total * (1 - discount)`

	tokens := minhash.NormalizeTokens(code, "python")
	joined := strings.Join(tokens, " ")

	if strings.Contains(joined, "calculate_total") {
		t.Fatal("identifier not normalized")
	}

	if strings.Contains(joined, "# Apply") {
		t.Fatal("comment not stripped")
	}

	if !strings.Contains(joined, "def") {
		t.Fatal("keyword 'def' was stripped")
	}

	if !strings.Contains(joined, "return") {
		t.Fatal("keyword 'return' was stripped")
	}
}

func TestNormalizeTokensStripsComments(t *testing.T) {
	t.Parallel()

	goCode := `func main() {
	// This is a comment
	x := 42 /* inline block */
}`
	tokens := minhash.NormalizeTokens(goCode, "go")
	joined := strings.Join(tokens, " ")

	if strings.Contains(joined, "comment") {
		t.Fatal("line comment not stripped")
	}

	if strings.Contains(joined, "inline") {
		t.Fatal("block comment not stripped")
	}
}

func TestNormalizeTokensReplacesStringLiterals(t *testing.T) {
	t.Parallel()

	code := `x := "hello world"
y := 'c'`
	tokens := minhash.NormalizeTokens(code, "go")

	strCount := 0

	for _, tok := range tokens {
		if tok == minhash.PlaceholderString {
			strCount++
		}
	}

	if strCount < 2 {
		t.Fatalf("expected at least 2 string placeholders, got %d", strCount)
	}
}

func TestNormalizeTokensReplacesNumbers(t *testing.T) {
	t.Parallel()

	code := `a := 42
b := 3.14
c := 0xFF
d := 0b1010`
	tokens := minhash.NormalizeTokens(code, "go")

	numCount := 0

	for _, tok := range tokens {
		if tok == minhash.PlaceholderNumber {
			numCount++
		}
	}

	if numCount < 4 {
		t.Fatalf("expected at least 4 number placeholders, got %d", numCount)
	}
}

func TestNormalizeTokensPreservesOperators(t *testing.T) {
	t.Parallel()

	code := `a := b + c * d`
	tokens := minhash.NormalizeTokens(code, "go")
	joined := strings.Join(tokens, " ")

	for _, op := range []string{"+", "*"} {
		if !strings.Contains(joined, op) {
			t.Fatalf("operator %q not preserved", op)
		}
	}
}

func TestNormalizeTokensJavaScript(t *testing.T) {
	t.Parallel()

	code := `function greet(name) {
	const msg = "Hello, " + name;
	console.log(msg);
}`
	tokens := minhash.NormalizeTokens(code, "javascript")
	joined := strings.Join(tokens, " ")

	if strings.Contains(joined, "greet") {
		t.Fatal("identifier not normalized")
	}

	if !strings.Contains(joined, "function") {
		t.Fatal("keyword 'function' was stripped")
	}

	if !strings.Contains(joined, "const") {
		t.Fatal("keyword 'const' was stripped")
	}
}

func TestNormalizedHashDeterministic(t *testing.T) {
	t.Parallel()

	tokens := []string{
		"func",
		minhash.PlaceholderID,
		"(",
		")",
		"{",
		"return",
		minhash.PlaceholderID,
		"}",
	}
	hashA := minhash.NormalizedHash(tokens)
	hashB := minhash.NormalizedHash(tokens)

	if hashA != hashB {
		t.Fatal("NormalizedHash not deterministic")
	}

	if len(hashA) != 64 {
		t.Fatalf("NormalizedHash length = %d, want 64 hex chars", len(hashA))
	}
}

func TestNormalizedHashDiffersForDifferentTokens(t *testing.T) {
	t.Parallel()

	hashA := minhash.NormalizedHash([]string{"func", minhash.PlaceholderID, "(", ")"})
	hashB := minhash.NormalizedHash([]string{"def", minhash.PlaceholderID, "(", ")"})

	if hashA == hashB {
		t.Fatal("different tokens produced same NormalizedHash")
	}
}

func TestRenamedFunctionsProduceSameNormalizedTokens(t *testing.T) {
	t.Parallel()

	codeA := `func processOrder(order *Order) error {
	if order.Total <= 0 {
		return fmt.Errorf("bad total")
	}
	return nil
}`
	codeB := `func handlePurchase(purchase *Purchase) error {
	if purchase.Amount <= 0 {
		return fmt.Errorf("invalid amount")
	}
	return nil
}`
	tokensA := minhash.NormalizeTokens(codeA, "go")
	tokensB := minhash.NormalizeTokens(codeB, "go")
	hashA := minhash.NormalizedHash(tokensA)
	hashB := minhash.NormalizedHash(tokensB)

	if hashA != hashB {
		t.Fatalf(
			"renamed functions produced different hashes:\n  A tokens: %v\n  B tokens: %v",
			tokensA,
			tokensB,
		)
	}
}

func TestStructurallyDifferentFunctionsProduceDifferentHashes(t *testing.T) {
	t.Parallel()

	codeA := `func add(a, b int) int {
	return a + b
}`
	codeB := `func multiply(a, b int) int {
	result := a * b
	if result < 0 {
		return -result
	}
	return result
}`
	tokensA := minhash.NormalizeTokens(codeA, "go")
	tokensB := minhash.NormalizeTokens(codeB, "go")
	hashA := minhash.NormalizedHash(tokensA)
	hashB := minhash.NormalizedHash(tokensB)

	if hashA == hashB {
		t.Fatal("structurally different functions produced same hash")
	}
}

func TestEndToEndSimilarityPipeline(t *testing.T) {
	t.Parallel()

	config := minhash.DefaultConfig()

	codeA := `func processOrder(order *Order) error {
	if order.Total <= 0 {
		return fmt.Errorf("bad total: %f", order.Total)
	}
	order.Status = "processed"
	return nil
}`
	codeB := `func handlePurchase(purchase *Purchase) error {
	if purchase.Amount <= 0 {
		return fmt.Errorf("invalid amount: %f", purchase.Amount)
	}
	purchase.State = "completed"
	return nil
}`
	codeC := `func sendEmail(to string, body string) error {
	conn, err := smtp.Dial(smtpServer)
	if err != nil {
		return err
	}
	defer conn.Close()
	return conn.Send(to, body)
}`

	tokensA := minhash.NormalizeTokens(codeA, "go")
	tokensB := minhash.NormalizeTokens(codeB, "go")
	tokensC := minhash.NormalizeTokens(codeC, "go")

	sigA := minhash.ComputeSignature(tokensA, config)
	sigB := minhash.ComputeSignature(tokensB, config)
	sigC := minhash.ComputeSignature(tokensC, config)

	jAB := minhash.EstimateJaccard(sigA, sigB)
	jAC := minhash.EstimateJaccard(sigA, sigC)

	if jAB <= jAC {
		t.Fatalf(
			"similar functions (%.3f) should score higher than unrelated (%.3f)",
			jAB,
			jAC,
		)
	}

	if jAB < 0.5 {
		t.Fatalf("similar functions Jaccard = %.3f, expected >= 0.5", jAB)
	}
}

func TestShellCommentStripping(t *testing.T) {
	t.Parallel()

	code := `#!/bin/bash
# This is a comment
echo "hello"
x=42`
	tokens := minhash.NormalizeTokens(code, "shell")
	joined := strings.Join(tokens, " ")

	if strings.Contains(joined, "comment") {
		t.Fatal("shell comment not stripped")
	}

	if !strings.Contains(joined, "echo") {
		t.Fatal("keyword 'echo' was stripped")
	}
}
