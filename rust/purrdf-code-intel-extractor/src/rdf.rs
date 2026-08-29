// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

use std::collections::BTreeMap;

use purrdf_rdf::{ParseOptions, RdfTerm, SpanTable, parse_dataset_with};

use crate::protocol::{
    FileRequest, FileResult, Provenance, SemanticFact, SemanticTerm, SourceStart,
};

const RDF_REIFIES: &str = "http://www.w3.org/1999/02/22-rdf-syntax-ns#reifies";

pub fn extract(file: &FileRequest, language: &str) -> Result<FileResult, String> {
    let media_type = match language {
        "turtle" => "text/turtle",
        "trig" => "application/trig",
        "ntriples" => "application/n-triples",
        "nquads" => "application/n-quads",
        other => return Err(format!("internal unsupported RDF language {other:?}")),
    };
    let options = ParseOptions {
        track_source_spans: true,
    };
    let (dataset, spans) = parse_dataset_with(
        file.content.as_bytes(),
        media_type,
        file.base_iri.as_deref(),
        &options,
    )
    .map_err(|error| format!("PurRDF {language} parse failed: {error}"))?;

    let attributes = || {
        BTreeMap::from([
            ("language".to_owned(), language.to_owned()),
            ("media_type".to_owned(), media_type.to_owned()),
        ])
    };
    let mut facts = Vec::new();
    for quad in dataset.owned_quads() {
        let start = subject_start(&quad.subject, spans.as_ref());
        facts.push(SemanticFact::new(
            "rdf_quad",
            Some(rdf_term(&quad.subject)),
            Some(SemanticTerm::Iri {
                value: quad.predicate,
            }),
            Some(rdf_term(&quad.object)),
            quad.graph_name.as_ref().map(rdf_term),
            None,
            attributes(),
            Provenance::rdf(&file.path, start),
        )?);
    }
    for reifier in dataset.owned_reifiers() {
        let start = subject_start(&reifier.reifier, spans.as_ref());
        facts.push(SemanticFact::new(
            "rdf_reifier",
            Some(rdf_term(&reifier.reifier)),
            Some(SemanticTerm::Iri {
                value: RDF_REIFIES.to_owned(),
            }),
            Some(SemanticTerm::QuotedTriple {
                subject: Box::new(rdf_term(&reifier.statement.subject)),
                predicate: reifier.statement.predicate,
                object: Box::new(rdf_term(&reifier.statement.object)),
            }),
            reifier.graph.as_ref().map(rdf_term),
            None,
            attributes(),
            Provenance::rdf(&file.path, start),
        )?);
    }
    for annotation in dataset.owned_annotations() {
        let start = subject_start(&annotation.reifier, spans.as_ref());
        facts.push(SemanticFact::new(
            "rdf_annotation",
            Some(rdf_term(&annotation.reifier)),
            Some(SemanticTerm::Iri {
                value: annotation.predicate,
            }),
            Some(rdf_term(&annotation.object)),
            annotation.graph.as_ref().map(rdf_term),
            None,
            attributes(),
            Provenance::rdf(&file.path, start),
        )?);
    }

    Ok(FileResult::success(file, language, "dataset", facts))
}

fn rdf_term(term: &RdfTerm) -> SemanticTerm {
    match term {
        RdfTerm::Iri(value) => SemanticTerm::Iri {
            value: value.clone(),
        },
        RdfTerm::BlankNode(label) => SemanticTerm::Blank {
            value: format!("_:{label}"),
        },
        RdfTerm::Literal(literal) => SemanticTerm::Literal {
            value: literal.lexical_form.clone(),
            datatype: literal.datatype.clone(),
            language: literal.language.clone(),
            direction: literal
                .direction
                .map(|direction| direction.as_str().to_owned()),
        },
        RdfTerm::Triple(triple) => SemanticTerm::QuotedTriple {
            subject: Box::new(rdf_term(&triple.subject)),
            predicate: triple.predicate.clone(),
            object: Box::new(rdf_term(&triple.object)),
        },
    }
}

fn subject_start(term: &RdfTerm, spans: Option<&SpanTable>) -> Option<SourceStart> {
    let key = match term {
        RdfTerm::Iri(value) => value.clone(),
        RdfTerm::BlankNode(label) => format!("_:{label}"),
        RdfTerm::Literal(_) | RdfTerm::Triple(_) => return None,
    };
    let position = spans?.position_for_subject(&key)?;
    Some(SourceStart {
        byte_offset: position.byte_offset,
        line: position.line,
        column: position.column,
    })
}

#[cfg(test)]
mod tests {
    use crate::protocol::sha256_hex;

    use super::*;

    #[test]
    fn turtle_uses_first_subject_start_only() {
        let content = concat!(
            "@prefix ex: <https://example.org/> .\n",
            "ex:s ex:p ex:o .\n",
            "ex:s ex:q ex:r .\n",
        );
        let file = FileRequest {
            path: "source.ttl".to_owned(),
            content_sha256: sha256_hex(content.as_bytes()),
            content: content.to_owned(),
            base_iri: None,
        };
        let result = extract(&file, "turtle").expect("valid Turtle extracts");
        assert_eq!(result.facts.len(), 2);
        assert!(result.facts.iter().all(|fact| {
            fact.provenance
                .start
                .is_some_and(|start| start.line == 2 && start.column == 1)
        }));
    }
}
