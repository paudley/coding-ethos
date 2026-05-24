// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package celexpr

type PythonASTFactInput struct {
	File                    string `json:"file"`
	Language                string `json:"language"`
	NodeKind                string `json:"node_kind"`
	SymbolKind              string `json:"symbol_kind"`
	SymbolName              string `json:"symbol_name"`
	SymbolPath              string `json:"symbol_path"`
	ParentSymbolPath        string `json:"parent_symbol_path"`
	EnclosingFunction       string `json:"enclosing_function"`
	EnclosingSymbol         string `json:"enclosing_symbol"`
	Text                    string `json:"text"`
	ReturnAnnotation        string `json:"return_annotation"`
	ExceptionType           string `json:"exception_type"`
	ExceptionAction         string `json:"exception_action"`
	ImportModule            string `json:"import_module"`
	CallName                string `json:"call_name"`
	AnnotationRole          string `json:"annotation_role"`
	SuppressionLabel        string `json:"suppression_label"`
	LoggerName              string `json:"logger_name"`
	LoggerMethod            string `json:"logger_method"`
	Line                    int64  `json:"line"`
	Column                  int64  `json:"column"`
	EndLine                 int64  `json:"end_line"`
	ParameterCount          int64  `json:"parameter_count"`
	HasVarargs              bool   `json:"has_varargs"`
	HasKwargs               bool   `json:"has_kwargs"`
	ModuleLevel             bool   `json:"module_level"`
	UnderClass              bool   `json:"under_class"`
	UnderConditional        bool   `json:"under_conditional"`
	UnderFunction           bool   `json:"under_function"`
	UnderTry                bool   `json:"under_try"`
	UnderTypeChecking       bool   `json:"under_type_checking"`
	IsImport                bool   `json:"is_import"`
	IsImportFallback        bool   `json:"is_import_fallback"`
	IsDynamicImport         bool   `json:"is_dynamic_import"`
	IsAssignedLambda        bool   `json:"is_assigned_lambda"`
	IsClosureFactory        bool   `json:"is_closure_factory"`
	IsSuppression           bool   `json:"is_suppression"`
	IsOptionalReturn        bool   `json:"is_optional_return"`
	IsBareExcept            bool   `json:"is_bare_except"`
	IsSilentExcept          bool   `json:"is_silent_except"`
	IsStructuredLog         bool   `json:"is_structured_log"`
	IsDirectImport          bool   `json:"is_direct_import"`
	IsUnexplainedTypeIgnore bool   `json:"is_unexplained_type_ignore"`
}
