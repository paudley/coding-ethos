// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

use std::collections::BTreeMap;

use purrdf_sparql_algebra::{
    BaseDirection, Expression, Function, GraphPattern, GraphTarget, GraphUpdateOperation,
    GroundTerm, NamedNodePattern, OrderExpression, PropertyPathExpression, QuadPattern, Query,
    QueryDataset, SparqlParser, TermPattern, TriplePattern, Update, UsingClause,
};

use crate::protocol::{
    FileRequest, FileResult, Provenance, SemanticFact, SemanticTerm, string_attributes,
};

pub fn extract(file: &FileRequest) -> Result<FileResult, String> {
    let parser = file
        .base_iri
        .as_deref()
        .map_or_else(SparqlParser::new, |base| {
            SparqlParser::new().with_base_iri(base)
        });
    match parser.parse_query(&file.content) {
        Ok(query) => {
            let mut walker = Walker::new(&file.path);
            walker.walk_query(&query)?;
            Ok(FileResult::success(file, "sparql", "query", walker.facts))
        }
        Err(query_error) => match parser.parse_update(&file.content) {
            Ok(update) => {
                let mut walker = Walker::new(&file.path);
                walker.walk_update(&update)?;
                Ok(FileResult::success(file, "sparql", "update", walker.facts))
            }
            Err(update_error) => Err(format!(
                "PurRDF SPARQL parse failed as query ({query_error}) and update ({update_error})"
            )),
        },
    }
}

struct Walker<'a> {
    source_path: &'a str,
    sequence: usize,
    facts: Vec<SemanticFact>,
}

impl<'a> Walker<'a> {
    fn new(source_path: &'a str) -> Self {
        Self {
            source_path,
            sequence: 0,
            facts: Vec::new(),
        }
    }

    #[allow(clippy::too_many_arguments)]
    fn emit(
        &mut self,
        kind: &str,
        subject: Option<SemanticTerm>,
        predicate: Option<SemanticTerm>,
        object: Option<SemanticTerm>,
        graph: Option<SemanticTerm>,
        value: Option<String>,
        mut attributes: BTreeMap<String, String>,
    ) -> Result<(), String> {
        attributes.insert("ordinal".to_owned(), self.sequence.to_string());
        self.sequence += 1;
        self.facts.push(SemanticFact::new(
            kind,
            subject,
            predicate,
            object,
            graph,
            value,
            attributes,
            Provenance::sparql(self.source_path),
        )?);
        Ok(())
    }

    fn emit_simple(&mut self, kind: &str, value: impl Into<String>) -> Result<(), String> {
        self.emit(
            kind,
            None,
            None,
            None,
            None,
            Some(value.into()),
            BTreeMap::new(),
        )
    }

    fn walk_query(&mut self, query: &Query) -> Result<(), String> {
        let (form, pattern, dataset, base_iri, version) = match query {
            Query::Select {
                pattern,
                dataset,
                base_iri,
                version,
            } => ("select", pattern, dataset, base_iri, version),
            Query::Construct {
                template,
                pattern,
                dataset,
                base_iri,
                version,
            } => {
                self.emit_simple("sparql_document", "construct")?;
                for triple in template {
                    self.emit_triple_pattern("sparql_construct_template", triple)?;
                }
                self.walk_dataset(dataset)?;
                self.walk_pattern(pattern)?;
                self.emit_query_metadata(base_iri.as_ref(), version.as_ref())?;
                return Ok(());
            }
            Query::Describe {
                pattern,
                targets,
                dataset,
                base_iri,
                version,
            } => {
                self.emit_simple("sparql_document", "describe")?;
                for target in targets {
                    self.emit(
                        "sparql_describe_target",
                        Some(named_pattern(target)),
                        None,
                        None,
                        None,
                        None,
                        BTreeMap::new(),
                    )?;
                }
                self.walk_dataset(dataset)?;
                self.walk_pattern(pattern)?;
                self.emit_query_metadata(base_iri.as_ref(), version.as_ref())?;
                return Ok(());
            }
            Query::Ask {
                pattern,
                dataset,
                base_iri,
                version,
            } => ("ask", pattern, dataset, base_iri, version),
        };
        self.emit_simple("sparql_document", form)?;
        self.walk_dataset(dataset)?;
        self.walk_pattern(pattern)?;
        self.emit_query_metadata(base_iri.as_ref(), version.as_ref())
    }

    fn emit_query_metadata(
        &mut self,
        base_iri: Option<&purrdf_sparql_algebra::NamedNode>,
        version: Option<&purrdf_sparql_algebra::SparqlVersion>,
    ) -> Result<(), String> {
        if let Some(base_iri) = base_iri {
            self.emit_simple("sparql_base_iri", base_iri.as_str())?;
        }
        if let Some(version) = version {
            self.emit_simple("sparql_version", version.raw())?;
        }
        Ok(())
    }

    fn walk_dataset(&mut self, dataset: &QueryDataset) -> Result<(), String> {
        for graph in &dataset.default {
            self.emit(
                "sparql_dataset_graph",
                Some(SemanticTerm::Iri {
                    value: graph.as_str().to_owned(),
                }),
                None,
                None,
                None,
                None,
                string_attributes(&[("role", "default".to_owned())]),
            )?;
        }
        for graph in &dataset.named {
            self.emit(
                "sparql_dataset_graph",
                Some(SemanticTerm::Iri {
                    value: graph.as_str().to_owned(),
                }),
                None,
                None,
                None,
                None,
                string_attributes(&[("role", "named".to_owned())]),
            )?;
        }
        Ok(())
    }

    fn walk_pattern(&mut self, pattern: &GraphPattern) -> Result<(), String> {
        self.emit_simple("sparql_graph_pattern", graph_pattern_kind(pattern))?;
        match pattern {
            GraphPattern::Bgp { patterns } => {
                for triple in patterns {
                    self.emit_triple_pattern("sparql_triple_pattern", triple)?;
                }
            }
            GraphPattern::Path {
                subject,
                path,
                object,
            } => {
                self.emit(
                    "sparql_path_constraint",
                    Some(term_pattern(subject)),
                    None,
                    Some(term_pattern(object)),
                    None,
                    Some(path.to_string()),
                    BTreeMap::new(),
                )?;
                self.walk_path(path)?;
            }
            GraphPattern::Join { left, right }
            | GraphPattern::Lateral { left, right }
            | GraphPattern::Union { left, right }
            | GraphPattern::Minus { left, right } => {
                self.walk_pattern(left)?;
                self.walk_pattern(right)?;
            }
            GraphPattern::LeftJoin {
                left,
                right,
                expression,
            } => {
                self.walk_pattern(left)?;
                self.walk_pattern(right)?;
                if let Some(expression) = expression {
                    self.walk_expression(expression)?;
                }
            }
            GraphPattern::Filter { expr, inner } => {
                self.walk_expression(expr)?;
                self.walk_pattern(inner)?;
            }
            GraphPattern::Graph { name, inner } => {
                self.emit(
                    "sparql_graph",
                    Some(named_pattern(name)),
                    None,
                    None,
                    None,
                    None,
                    BTreeMap::new(),
                )?;
                self.walk_pattern(inner)?;
            }
            GraphPattern::Extend {
                inner,
                variable,
                expression,
            } => {
                self.emit(
                    "sparql_binding",
                    Some(SemanticTerm::Variable {
                        value: variable.as_str().to_owned(),
                    }),
                    None,
                    None,
                    None,
                    None,
                    BTreeMap::new(),
                )?;
                self.walk_expression(expression)?;
                self.walk_pattern(inner)?;
            }
            GraphPattern::Service {
                name,
                inner,
                silent,
            } => {
                self.emit(
                    "sparql_service",
                    Some(named_pattern(name)),
                    None,
                    None,
                    None,
                    None,
                    string_attributes(&[("silent", silent.to_string())]),
                )?;
                self.walk_pattern(inner)?;
            }
            GraphPattern::Values {
                variables,
                bindings,
            } => {
                self.emit(
                    "sparql_values",
                    None,
                    None,
                    None,
                    None,
                    None,
                    string_attributes(&[
                        ("columns", variables.len().to_string()),
                        ("rows", bindings.len().to_string()),
                    ]),
                )?;
                for (row_index, row) in bindings.iter().enumerate() {
                    for (column_index, cell) in row.iter().enumerate() {
                        if let Some(term) = cell {
                            self.emit(
                                "sparql_values_cell",
                                Some(ground_term(term)),
                                None,
                                None,
                                None,
                                None,
                                string_attributes(&[
                                    ("row", row_index.to_string()),
                                    ("column", column_index.to_string()),
                                ]),
                            )?;
                        }
                    }
                }
            }
            GraphPattern::OrderBy { inner, expression } => {
                for order in expression {
                    match order {
                        OrderExpression::Asc(expression) | OrderExpression::Desc(expression) => {
                            self.walk_expression(expression)?;
                        }
                    }
                }
                self.walk_pattern(inner)?;
            }
            GraphPattern::Project { inner, .. }
            | GraphPattern::Distinct { inner }
            | GraphPattern::Reduced { inner }
            | GraphPattern::Slice { inner, .. } => self.walk_pattern(inner)?,
            GraphPattern::Group {
                inner, aggregates, ..
            } => {
                for (_, aggregate) in aggregates {
                    for expression in aggregate.args() {
                        self.walk_expression(expression)?;
                    }
                }
                self.walk_pattern(inner)?;
            }
            GraphPattern::PropertyFunction(call) => {
                self.emit(
                    "sparql_property_function",
                    None,
                    Some(SemanticTerm::Iri {
                        value: call.iri.clone(),
                    }),
                    None,
                    None,
                    None,
                    string_attributes(&[
                        ("subject_arity", call.subject_args.len().to_string()),
                        ("object_arity", call.object_args.len().to_string()),
                    ]),
                )?;
            }
        }
        Ok(())
    }

    fn walk_path(&mut self, path: &PropertyPathExpression) -> Result<(), String> {
        let attributes = match path {
            PropertyPathExpression::Range { min, max, .. } => string_attributes(&[
                ("operator", "range".to_owned()),
                ("min", min.to_string()),
                (
                    "max",
                    max.map_or_else(|| "unbounded".to_owned(), |value| value.to_string()),
                ),
            ]),
            PropertyPathExpression::Wildcard { namespace } => string_attributes(&[
                ("operator", "wildcard".to_owned()),
                (
                    "namespace",
                    namespace
                        .as_ref()
                        .map_or_else(String::new, |node| node.as_str().to_owned()),
                ),
            ]),
            other => string_attributes(&[("operator", property_path_kind(other).to_owned())]),
        };
        let predicate = match path {
            PropertyPathExpression::NamedNode(node) => Some(SemanticTerm::Iri {
                value: node.as_str().to_owned(),
            }),
            _ => None,
        };
        self.emit(
            "sparql_property_path",
            None,
            predicate,
            None,
            None,
            Some(path.to_string()),
            attributes,
        )?;
        match path {
            PropertyPathExpression::Reverse(inner)
            | PropertyPathExpression::ZeroOrMore(inner)
            | PropertyPathExpression::OneOrMore(inner)
            | PropertyPathExpression::ZeroOrOne(inner)
            | PropertyPathExpression::Range { inner, .. } => self.walk_path(inner)?,
            PropertyPathExpression::Sequence(left, right)
            | PropertyPathExpression::Alternative(left, right) => {
                self.walk_path(left)?;
                self.walk_path(right)?;
            }
            PropertyPathExpression::NegatedPropertySet(elements) => {
                for element in elements {
                    self.emit(
                        "sparql_negated_path_predicate",
                        None,
                        Some(SemanticTerm::Iri {
                            value: element.predicate.as_str().to_owned(),
                        }),
                        None,
                        None,
                        None,
                        string_attributes(&[("inverse", element.inverse.to_string())]),
                    )?;
                }
            }
            PropertyPathExpression::NamedNode(_) | PropertyPathExpression::Wildcard { .. } => {}
        }
        Ok(())
    }

    fn walk_expression(&mut self, expression: &Expression) -> Result<(), String> {
        self.emit_simple("sparql_expression", expression_kind(expression))?;
        match expression {
            Expression::NamedNode(node) => self.emit(
                "sparql_iri_reference",
                Some(SemanticTerm::Iri {
                    value: node.as_str().to_owned(),
                }),
                None,
                None,
                None,
                None,
                BTreeMap::new(),
            )?,
            Expression::Literal(_) | Expression::Variable(_) | Expression::Bound(_) => {}
            Expression::Or(left, right)
            | Expression::And(left, right)
            | Expression::Equal(left, right)
            | Expression::SameTerm(left, right)
            | Expression::Greater(left, right)
            | Expression::GreaterOrEqual(left, right)
            | Expression::Less(left, right)
            | Expression::LessOrEqual(left, right)
            | Expression::Add(left, right)
            | Expression::Subtract(left, right)
            | Expression::Multiply(left, right)
            | Expression::Divide(left, right) => {
                self.walk_expression(left)?;
                self.walk_expression(right)?;
            }
            Expression::UnaryPlus(inner)
            | Expression::UnaryMinus(inner)
            | Expression::Not(inner) => self.walk_expression(inner)?,
            Expression::In(inner, entries) => {
                self.walk_expression(inner)?;
                for entry in entries {
                    self.walk_expression(entry)?;
                }
            }
            Expression::If(condition, when_true, when_false) => {
                self.walk_expression(condition)?;
                self.walk_expression(when_true)?;
                self.walk_expression(when_false)?;
            }
            Expression::Coalesce(entries) => {
                for entry in entries {
                    self.walk_expression(entry)?;
                }
            }
            Expression::FunctionCall(function, arguments) => {
                let iri = match function {
                    Function::Custom(node) => Some(node.as_str()),
                    Function::Purrdf(call) => Some(call.iri.as_str()),
                    _ => None,
                };
                if let Some(iri) = iri {
                    self.emit(
                        "sparql_function",
                        None,
                        Some(SemanticTerm::Iri {
                            value: iri.to_owned(),
                        }),
                        None,
                        None,
                        None,
                        BTreeMap::new(),
                    )?;
                }
                for argument in arguments {
                    self.walk_expression(argument)?;
                }
            }
            Expression::Exists(pattern) => self.walk_pattern(pattern)?,
        }
        Ok(())
    }

    fn emit_triple_pattern(&mut self, kind: &str, triple: &TriplePattern) -> Result<(), String> {
        self.emit(
            kind,
            Some(term_pattern(&triple.subject)),
            Some(named_pattern(&triple.predicate)),
            Some(term_pattern(&triple.object)),
            None,
            None,
            BTreeMap::new(),
        )
    }

    fn emit_quad_pattern(&mut self, role: &str, quad: &QuadPattern) -> Result<(), String> {
        self.emit(
            "sparql_quad_pattern",
            Some(term_pattern(&quad.triple.subject)),
            Some(named_pattern(&quad.triple.predicate)),
            Some(term_pattern(&quad.triple.object)),
            quad.graph.as_ref().map(named_pattern),
            None,
            string_attributes(&[("role", role.to_owned())]),
        )
    }

    fn walk_update(&mut self, update: &Update) -> Result<(), String> {
        self.emit(
            "sparql_document",
            None,
            None,
            None,
            None,
            Some("update".to_owned()),
            string_attributes(&[("operation_count", update.operations.len().to_string())]),
        )?;
        self.emit_query_metadata(update.base_iri.as_ref(), update.version.as_ref())?;
        for operation in &update.operations {
            self.walk_update_operation(operation)?;
        }
        Ok(())
    }

    fn walk_update_operation(&mut self, operation: &GraphUpdateOperation) -> Result<(), String> {
        self.emit_simple("sparql_update_operation", update_operation_kind(operation))?;
        match operation {
            GraphUpdateOperation::InsertData { data } => {
                for quad in data {
                    self.emit_quad_pattern("insert_data", quad)?;
                }
            }
            GraphUpdateOperation::DeleteData { data } => {
                for quad in data {
                    self.emit_quad_pattern("delete_data", quad)?;
                }
            }
            GraphUpdateOperation::DeleteInsert {
                delete,
                insert,
                with,
                using,
                pattern,
            } => {
                if let Some(with) = with {
                    self.emit_simple("sparql_with_graph", with.as_str())?;
                }
                for clause in using {
                    let (role, node) = match clause {
                        UsingClause::Default(node) => ("default", node),
                        UsingClause::Named(node) => ("named", node),
                    };
                    self.emit(
                        "sparql_using_graph",
                        Some(SemanticTerm::Iri {
                            value: node.as_str().to_owned(),
                        }),
                        None,
                        None,
                        None,
                        None,
                        string_attributes(&[("role", role.to_owned())]),
                    )?;
                }
                for quad in delete {
                    self.emit_quad_pattern("delete_template", quad)?;
                }
                for quad in insert {
                    self.emit_quad_pattern("insert_template", quad)?;
                }
                self.walk_pattern(pattern)?;
            }
            GraphUpdateOperation::Load {
                silent,
                source,
                destination,
            } => self.emit_graph_operation(
                "load",
                *silent,
                Some(SemanticTerm::Iri {
                    value: source.as_str().to_owned(),
                }),
                Some(destination),
            )?,
            GraphUpdateOperation::Clear { silent, target } => {
                self.emit_graph_operation("clear", *silent, None, Some(target))?;
            }
            GraphUpdateOperation::Drop { silent, target } => {
                self.emit_graph_operation("drop", *silent, None, Some(target))?;
            }
            GraphUpdateOperation::Create { silent, graph } => self.emit(
                "sparql_graph_operation",
                Some(SemanticTerm::Iri {
                    value: graph.as_str().to_owned(),
                }),
                None,
                None,
                None,
                Some("create".to_owned()),
                string_attributes(&[("silent", silent.to_string())]),
            )?,
            GraphUpdateOperation::Add {
                silent,
                source,
                destination,
            }
            | GraphUpdateOperation::Move {
                silent,
                source,
                destination,
            }
            | GraphUpdateOperation::Copy {
                silent,
                source,
                destination,
            } => self.emit(
                "sparql_graph_operation",
                graph_target_term(source),
                None,
                graph_target_term(destination),
                None,
                Some(update_operation_kind(operation).to_owned()),
                string_attributes(&[
                    ("silent", silent.to_string()),
                    ("source", source.to_string()),
                    ("destination", destination.to_string()),
                ]),
            )?,
        }
        Ok(())
    }

    fn emit_graph_operation(
        &mut self,
        operation: &str,
        silent: bool,
        subject: Option<SemanticTerm>,
        target: Option<&GraphTarget>,
    ) -> Result<(), String> {
        let target_text = target.map_or_else(String::new, ToString::to_string);
        self.emit(
            "sparql_graph_operation",
            subject,
            None,
            target.and_then(graph_target_term),
            None,
            Some(operation.to_owned()),
            string_attributes(&[("silent", silent.to_string()), ("target", target_text)]),
        )
    }
}

fn term_pattern(term: &TermPattern) -> SemanticTerm {
    match term {
        TermPattern::NamedNode(node) => SemanticTerm::Iri {
            value: node.as_str().to_owned(),
        },
        TermPattern::BlankNode(node) => SemanticTerm::Blank {
            value: format!("_:{}", node.as_str()),
        },
        TermPattern::Literal(literal) => sparql_literal(literal),
        TermPattern::Variable(variable) => SemanticTerm::Variable {
            value: variable.as_str().to_owned(),
        },
        TermPattern::Triple(triple) => SemanticTerm::QuotedTriple {
            subject: Box::new(term_pattern(&triple.subject)),
            predicate: named_pattern_value(&triple.predicate),
            object: Box::new(term_pattern(&triple.object)),
        },
    }
}

fn ground_term(term: &GroundTerm) -> SemanticTerm {
    match term {
        GroundTerm::NamedNode(node) => SemanticTerm::Iri {
            value: node.as_str().to_owned(),
        },
        GroundTerm::Literal(literal) => sparql_literal(literal),
        GroundTerm::Triple(triple) => SemanticTerm::QuotedTriple {
            subject: Box::new(ground_term(&triple.subject)),
            predicate: triple.predicate.as_str().to_owned(),
            object: Box::new(ground_term(&triple.object)),
        },
        GroundTerm::BlankNode(node) => SemanticTerm::Blank {
            value: format!("_:{}", node.as_str()),
        },
    }
}

fn sparql_literal(literal: &purrdf_sparql_algebra::Literal) -> SemanticTerm {
    let direction = literal.direction().map(|direction| match direction {
        BaseDirection::Ltr => "ltr".to_owned(),
        BaseDirection::Rtl => "rtl".to_owned(),
    });
    SemanticTerm::Literal {
        value: literal.value().to_owned(),
        datatype: Some(literal.datatype().as_str().to_owned()),
        language: literal.language().map(ToOwned::to_owned),
        direction,
    }
}

fn named_pattern(pattern: &NamedNodePattern) -> SemanticTerm {
    match pattern {
        NamedNodePattern::NamedNode(node) => SemanticTerm::Iri {
            value: node.as_str().to_owned(),
        },
        NamedNodePattern::Variable(variable) => SemanticTerm::Variable {
            value: variable.as_str().to_owned(),
        },
    }
}

fn named_pattern_value(pattern: &NamedNodePattern) -> String {
    match pattern {
        NamedNodePattern::NamedNode(node) => node.as_str().to_owned(),
        NamedNodePattern::Variable(variable) => format!("?{}", variable.as_str()),
    }
}

fn graph_target_term(target: &GraphTarget) -> Option<SemanticTerm> {
    match target {
        GraphTarget::Named(node) => Some(SemanticTerm::Iri {
            value: node.as_str().to_owned(),
        }),
        GraphTarget::Default | GraphTarget::NamedGraphs | GraphTarget::All => None,
    }
}

fn graph_pattern_kind(pattern: &GraphPattern) -> &'static str {
    match pattern {
        GraphPattern::Bgp { .. } => "bgp",
        GraphPattern::Path { .. } => "path",
        GraphPattern::Join { .. } => "join",
        GraphPattern::LeftJoin { .. } => "left_join",
        GraphPattern::Lateral { .. } => "lateral",
        GraphPattern::Filter { .. } => "filter",
        GraphPattern::Union { .. } => "union",
        GraphPattern::Graph { .. } => "graph",
        GraphPattern::Extend { .. } => "extend",
        GraphPattern::Minus { .. } => "minus",
        GraphPattern::Service { .. } => "service",
        GraphPattern::Values { .. } => "values",
        GraphPattern::OrderBy { .. } => "order_by",
        GraphPattern::Project { .. } => "project",
        GraphPattern::Distinct { .. } => "distinct",
        GraphPattern::Reduced { .. } => "reduced",
        GraphPattern::Slice { .. } => "slice",
        GraphPattern::Group { .. } => "group",
        GraphPattern::PropertyFunction(_) => "property_function",
    }
}

fn property_path_kind(path: &PropertyPathExpression) -> &'static str {
    match path {
        PropertyPathExpression::NamedNode(_) => "named_node",
        PropertyPathExpression::Reverse(_) => "reverse",
        PropertyPathExpression::Sequence(_, _) => "sequence",
        PropertyPathExpression::Alternative(_, _) => "alternative",
        PropertyPathExpression::ZeroOrMore(_) => "zero_or_more",
        PropertyPathExpression::OneOrMore(_) => "one_or_more",
        PropertyPathExpression::ZeroOrOne(_) => "zero_or_one",
        PropertyPathExpression::NegatedPropertySet(_) => "negated_property_set",
        PropertyPathExpression::Range { .. } => "range",
        PropertyPathExpression::Wildcard { .. } => "wildcard",
    }
}

fn expression_kind(expression: &Expression) -> &'static str {
    match expression {
        Expression::NamedNode(_) => "named_node",
        Expression::Literal(_) => "literal",
        Expression::Variable(_) => "variable",
        Expression::Bound(_) => "bound",
        Expression::Or(_, _) => "or",
        Expression::And(_, _) => "and",
        Expression::Equal(_, _) => "equal",
        Expression::SameTerm(_, _) => "same_term",
        Expression::Greater(_, _) => "greater",
        Expression::GreaterOrEqual(_, _) => "greater_or_equal",
        Expression::Less(_, _) => "less",
        Expression::LessOrEqual(_, _) => "less_or_equal",
        Expression::Add(_, _) => "add",
        Expression::Subtract(_, _) => "subtract",
        Expression::Multiply(_, _) => "multiply",
        Expression::Divide(_, _) => "divide",
        Expression::UnaryPlus(_) => "unary_plus",
        Expression::UnaryMinus(_) => "unary_minus",
        Expression::Not(_) => "not",
        Expression::In(_, _) => "in",
        Expression::If(_, _, _) => "if",
        Expression::Coalesce(_) => "coalesce",
        Expression::FunctionCall(_, _) => "function_call",
        Expression::Exists(_) => "exists",
    }
}

fn update_operation_kind(operation: &GraphUpdateOperation) -> &'static str {
    match operation {
        GraphUpdateOperation::InsertData { .. } => "insert_data",
        GraphUpdateOperation::DeleteData { .. } => "delete_data",
        GraphUpdateOperation::DeleteInsert { .. } => "delete_insert",
        GraphUpdateOperation::Load { .. } => "load",
        GraphUpdateOperation::Clear { .. } => "clear",
        GraphUpdateOperation::Drop { .. } => "drop",
        GraphUpdateOperation::Create { .. } => "create",
        GraphUpdateOperation::Add { .. } => "add",
        GraphUpdateOperation::Move { .. } => "move",
        GraphUpdateOperation::Copy { .. } => "copy",
    }
}

#[cfg(test)]
mod tests {
    use crate::protocol::sha256_hex;

    use super::*;

    #[test]
    fn query_walks_graph_service_and_paths() {
        let content = concat!(
            "SELECT ?o WHERE { ",
            "GRAPH <https://example.org/g> { ?s <https://example.org/p>/<https://example.org/q>+ ?o } ",
            "SERVICE SILENT <https://example.org/sparql> { ?s <https://example.org/r> ?o } ",
            "}",
        );
        let file = FileRequest {
            path: "query.rq".to_owned(),
            content_sha256: sha256_hex(content.as_bytes()),
            content: content.to_owned(),
            base_iri: None,
        };
        let result = extract(&file).expect("valid query extracts");
        for kind in ["sparql_graph", "sparql_service", "sparql_property_path"] {
            assert!(
                result.facts.iter().any(|fact| fact.kind == kind),
                "missing {kind} fact"
            );
        }
        assert!(
            result
                .facts
                .iter()
                .all(|fact| fact.provenance.start.is_none())
        );
    }

    #[test]
    fn update_walks_operations() {
        let content = concat!(
            "INSERT DATA { GRAPH <https://example.org/g> { ",
            "<https://example.org/s> <https://example.org/p> <https://example.org/o> } }; ",
            "MOVE SILENT GRAPH <https://example.org/g> TO GRAPH <https://example.org/h>",
        );
        let file = FileRequest {
            path: "update.sparql".to_owned(),
            content_sha256: sha256_hex(content.as_bytes()),
            content: content.to_owned(),
            base_iri: None,
        };
        let result = extract(&file).expect("valid update extracts");
        assert_eq!(result.document_kind.as_deref(), Some("update"));
        assert_eq!(
            result
                .facts
                .iter()
                .filter(|fact| fact.kind == "sparql_update_operation")
                .count(),
            2
        );
    }

    #[test]
    fn ground_term_conversion_preserves_quoted_triples() {
        let parser = SparqlParser::new();
        let query = parser
            .parse_query(
                "SELECT ?x WHERE { VALUES ?x { << <https://e/s> <https://e/p> <https://e/o> >> } }",
            )
            .expect("RDF 1.2 quoted triple in VALUES parses");
        let Query::Select { pattern, .. } = query else {
            panic!("expected SELECT");
        };
        let mut stack = vec![&pattern];
        let mut found = false;
        while let Some(pattern) = stack.pop() {
            match pattern {
                GraphPattern::Values { bindings, .. } => {
                    found =
                        bindings.iter().flatten().flatten().any(|term| {
                            matches!(ground_term(term), SemanticTerm::QuotedTriple { .. })
                        });
                }
                GraphPattern::Project { inner, .. }
                | GraphPattern::Distinct { inner }
                | GraphPattern::Reduced { inner }
                | GraphPattern::Slice { inner, .. } => stack.push(inner),
                GraphPattern::Join { left, right } => {
                    stack.push(left);
                    stack.push(right);
                }
                _ => {}
            }
        }
        assert!(found);
    }
}
