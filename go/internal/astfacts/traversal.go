// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package astfacts

import tree_sitter "github.com/tree-sitter/go-tree-sitter"

func Walk(root *tree_sitter.Node, visit func(*tree_sitter.Node)) {
	if root == nil {
		return
	}
	visit(root)
	for index := uint(0); index < root.NamedChildCount(); index++ {
		Walk(root.NamedChild(index), visit)
	}
}

func WalkWithDepth(root *tree_sitter.Node, visit func(*tree_sitter.Node, int)) {
	walkWithDepth(root, 0, visit)
}

func walkWithDepth(node *tree_sitter.Node, depth int, visit func(*tree_sitter.Node, int)) {
	if node == nil {
		return
	}
	visit(node, depth)
	for index := uint(0); index < node.NamedChildCount(); index++ {
		walkWithDepth(node.NamedChild(index), depth+1, visit)
	}
}

func NodeRowSpan(node *tree_sitter.Node) (int, int, bool) {
	if node == nil {
		return 0, 0, false
	}
	start := boundedUintToInt(node.StartPosition().Row, int(^uint(0)>>1)) + 1
	end := boundedUintToInt(node.EndPosition().Row, int(^uint(0)>>1)) + 1

	return start, end, true
}

func NodeContainsLine(node *tree_sitter.Node, line int) bool {
	start, end, ok := NodeRowSpan(node)

	return ok && start <= line && line <= end
}
