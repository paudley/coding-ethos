// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

use std::io::Write;
use std::process::{Command, Stdio};

use coding_ethos_purrdf_extractor::{
    BatchRequest, FileRequest, FileStatus, PROTOCOL, PURRDF_REVISION, SemanticTerm, extract_batch,
    sha256_hex,
};

const TURTLE: &str = include_str!("fixtures/rdf/semantic.ttl");
const TRIG: &str = include_str!("fixtures/rdf/semantic.trig");
const NTRIPLES: &str = include_str!("fixtures/rdf/semantic.nt");
const NQUADS: &str = include_str!("fixtures/rdf/semantic.nq");
const QUERY: &str = include_str!("fixtures/sparql/query.rq");
const UPDATE: &str = include_str!("fixtures/sparql/update.sparql");
const MALFORMED: &str = include_str!("fixtures/sparql/malformed.rq");

fn file(path: &str, content: &str) -> FileRequest {
    FileRequest {
        path: path.to_owned(),
        content_sha256: sha256_hex(content.as_bytes()),
        content: content.to_owned(),
        base_iri: None,
    }
}

fn request(files: Vec<FileRequest>) -> BatchRequest {
    BatchRequest {
        protocol: PROTOCOL.to_owned(),
        request_id: "integration-test".to_owned(),
        files,
    }
}

#[test]
fn extracts_all_frozen_extensions_deterministically() {
    let files = vec![
        file("z/query.rq", QUERY),
        file("a/semantic.ttl", TURTLE),
        file("b/semantic.trig", TRIG),
        file("c/semantic.nt", NTRIPLES),
        file("d/semantic.nq", NQUADS),
        file("y/update.sparql", UPDATE),
    ];
    let first = extract_batch(request(files.clone())).expect("batch extracts");
    let second = extract_batch(request(files)).expect("same batch extracts again");
    assert_eq!(
        serde_json::to_vec(&first.response).expect("first response encodes"),
        serde_json::to_vec(&second.response).expect("second response encodes"),
        "same content must produce byte-identical JSON"
    );
    assert_eq!(first.response.extractor.purrdf_revision, PURRDF_REVISION);
    assert!(
        first
            .response
            .results
            .windows(2)
            .all(|pair| pair[0].path < pair[1].path),
        "results are path sorted"
    );
    assert!(
        first
            .response
            .results
            .iter()
            .all(|result| matches!(result.status, FileStatus::Ok))
    );
    for result in &first.response.results {
        assert!(
            result.facts.windows(2).all(|pair| pair[0].id < pair[1].id),
            "{} facts are ID sorted and unique",
            result.path
        );
    }
}

#[test]
fn rdf_facts_preserve_rdf12_and_source_contracts() {
    let outcome = extract_batch(request(vec![
        file("semantic.ttl", TURTLE),
        file("semantic.trig", TRIG),
    ]))
    .expect("RDF batch extracts");
    let facts: Vec<_> = outcome
        .response
        .results
        .iter()
        .flat_map(|result| &result.facts)
        .collect();
    assert!(facts.iter().any(|fact| fact.kind == "rdf_reifier"));
    assert!(facts.iter().any(|fact| fact.kind == "rdf_annotation"));
    assert!(facts.iter().any(|fact| fact.graph.is_some()));
    assert!(facts.iter().any(|fact| {
        matches!(
            fact.object.as_ref(),
            Some(SemanticTerm::QuotedTriple { .. })
        )
    }));
    assert!(facts.iter().any(|fact| {
        matches!(fact.subject.as_ref(), Some(SemanticTerm::Blank { .. }))
            || matches!(fact.object.as_ref(), Some(SemanticTerm::Blank { .. }))
    }));

    let encoded = serde_json::to_value(&outcome.response).expect("response encodes");
    let facts = encoded["results"]
        .as_array()
        .expect("results array")
        .iter()
        .flat_map(|result| result["facts"].as_array().expect("facts array"))
        .collect::<Vec<_>>();
    assert!(
        facts
            .iter()
            .all(|fact| { fact["provenance"]["span_fidelity"] == "subject_start" })
    );
    assert!(
        facts
            .iter()
            .any(|fact| fact["provenance"]["start"].is_object())
    );
}

#[test]
fn sparql_facts_cover_query_update_graph_service_and_paths() {
    let outcome = extract_batch(request(vec![
        file("query.rq", QUERY),
        file("update.sparql", UPDATE),
    ]))
    .expect("SPARQL batch extracts");
    let facts: Vec<_> = outcome
        .response
        .results
        .iter()
        .flat_map(|result| &result.facts)
        .collect();
    for kind in [
        "sparql_graph",
        "sparql_service",
        "sparql_property_path",
        "sparql_update_operation",
        "sparql_quad_pattern",
    ] {
        assert!(facts.iter().any(|fact| fact.kind == kind), "missing {kind}");
    }
    let encoded = serde_json::to_value(&outcome.response).expect("response encodes");
    assert!(
        encoded["results"]
            .as_array()
            .expect("results array")
            .iter()
            .flat_map(|result| result["facts"].as_array().expect("facts array"))
            .all(|fact| {
                fact["provenance"]["span_fidelity"] == "none"
                    && fact["provenance"].get("start").is_none()
            })
    );
}

#[test]
fn malformed_file_is_structured_and_does_not_fail_batch() {
    let outcome = extract_batch(request(vec![
        file("good.nt", NTRIPLES),
        file("malformed.rq", MALFORMED),
    ]))
    .expect("a valid envelope returns every file result");
    assert_eq!(outcome.response.results.len(), 2);
    let malformed = outcome
        .response
        .results
        .iter()
        .find(|result| result.path == "malformed.rq")
        .expect("malformed result exists");
    assert!(matches!(malformed.status, FileStatus::Error));
    assert!(malformed.facts.is_empty());
    assert!(
        malformed
            .error
            .as_deref()
            .is_some_and(|message| message.contains("failed as query"))
    );
    assert_eq!(outcome.diagnostics.len(), 1);
}

#[test]
fn binary_keeps_stdout_machine_only_and_stderr_diagnostic_only() {
    let request = request(vec![file("malformed.rq", MALFORMED)]);
    let encoded = serde_json::to_vec(&request).expect("request encodes");
    let mut child = Command::new(env!("CARGO_BIN_EXE_coding-ethos-purrdf-extractor"))
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .expect("extractor starts");
    child
        .stdin
        .as_mut()
        .expect("stdin pipe")
        .write_all(&encoded)
        .expect("request writes");
    let output = child.wait_with_output().expect("extractor completes");
    assert!(
        output.status.success(),
        "file errors do not fail a valid batch"
    );
    let response: serde_json::Value =
        serde_json::from_slice(&output.stdout).expect("stdout is exactly JSON");
    assert_eq!(response["protocol"], PROTOCOL);
    let stderr = String::from_utf8(output.stderr).expect("stderr is UTF-8");
    assert!(stderr.contains("malformed.rq"));
    assert!(!stderr.contains(PROTOCOL));
}

#[test]
fn binary_rejects_an_unknown_protocol_with_exit_two() {
    let mut request = request(Vec::new());
    request.protocol = "coding-ethos.external-extractor.v0".to_owned();
    let encoded = serde_json::to_vec(&request).expect("request encodes");
    let mut child = Command::new(env!("CARGO_BIN_EXE_coding-ethos-purrdf-extractor"))
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .expect("extractor starts");
    child
        .stdin
        .as_mut()
        .expect("stdin pipe")
        .write_all(&encoded)
        .expect("request writes");
    let output = child.wait_with_output().expect("extractor completes");
    assert_eq!(output.status.code(), Some(2));
    assert!(output.stdout.is_empty());
    assert!(
        String::from_utf8(output.stderr)
            .expect("stderr is UTF-8")
            .contains("protocol error")
    );
}
