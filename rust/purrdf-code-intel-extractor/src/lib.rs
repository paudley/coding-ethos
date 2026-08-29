// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

mod protocol;
mod rdf;
mod sparql;

use std::collections::BTreeSet;
use std::path::Path;

pub use protocol::{
    BatchRequest, BatchResponse, ExtractorIdentity, FileRequest, FileResult, FileStatus, PROTOCOL,
    PURRDF_REVISION, Provenance, SemanticFact, SemanticTerm, SourceStart, SpanFidelity, sha256_hex,
};

#[derive(Debug)]
pub struct ExtractionOutcome {
    pub response: BatchResponse,
    pub diagnostics: Vec<String>,
}

pub fn extract_batch(request: BatchRequest) -> Result<ExtractionOutcome, String> {
    validate_request(&request)?;

    let mut files: Vec<&FileRequest> = request.files.iter().collect();
    files.sort_by(|left, right| left.path.cmp(&right.path));

    let mut results = Vec::with_capacity(files.len());
    let mut diagnostics = Vec::new();
    for file in files {
        let language = language_for_path(&file.path).unwrap_or("unsupported");
        if sha256_hex(file.content.as_bytes()) != file.content_sha256 {
            let message = "content_sha256 does not match UTF-8 content bytes".to_owned();
            diagnostics.push(format!("{}: {message}", file.path));
            results.push(FileResult::error(file, language, message));
            continue;
        }

        let extracted = match language {
            "turtle" | "trig" | "ntriples" | "nquads" => rdf::extract(file, language),
            "sparql" => sparql::extract(file),
            _ => Err(
                "unsupported extension; expected .ttl, .trig, .nt, .nq, .rq, or .sparql".to_owned(),
            ),
        };
        match extracted {
            Ok(result) => results.push(result),
            Err(message) => {
                diagnostics.push(format!("{}: {message}", file.path));
                results.push(FileResult::error(file, language, message));
            }
        }
    }

    Ok(ExtractionOutcome {
        response: BatchResponse {
            protocol: PROTOCOL,
            request_id: request.request_id,
            extractor: ExtractorIdentity::default(),
            results,
        },
        diagnostics,
    })
}

fn validate_request(request: &BatchRequest) -> Result<(), String> {
    if request.protocol != PROTOCOL {
        return Err(format!(
            "unsupported protocol {:?}; expected {PROTOCOL:?}",
            request.protocol
        ));
    }
    if request.request_id.is_empty() {
        return Err("request_id must not be empty".to_owned());
    }

    let mut paths = BTreeSet::new();
    for file in &request.files {
        if file.path.is_empty() {
            return Err("file path must not be empty".to_owned());
        }
        if file.path.contains('\0') {
            return Err(format!("file path {:?} contains NUL", file.path));
        }
        if !paths.insert(file.path.as_str()) {
            return Err(format!("duplicate file path {:?}", file.path));
        }
        if file.content_sha256.len() != 64
            || !file
                .content_sha256
                .bytes()
                .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))
        {
            return Err(format!(
                "content_sha256 for {:?} must be 64 lowercase hexadecimal characters",
                file.path
            ));
        }
    }
    Ok(())
}

fn language_for_path(path: &str) -> Option<&'static str> {
    let extension = Path::new(path).extension()?.to_str()?.to_ascii_lowercase();
    match extension.as_str() {
        "ttl" => Some("turtle"),
        "trig" => Some("trig"),
        "nt" => Some("ntriples"),
        "nq" => Some("nquads"),
        "rq" | "sparql" => Some("sparql"),
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn rejects_duplicate_paths() {
        let content = "<https://e/s> <https://e/p> <https://e/o> .\n";
        let file = FileRequest {
            path: "same.nt".to_owned(),
            content_sha256: sha256_hex(content.as_bytes()),
            content: content.to_owned(),
            base_iri: None,
        };
        let request = BatchRequest {
            protocol: PROTOCOL.to_owned(),
            request_id: "duplicates".to_owned(),
            files: vec![file.clone(), file],
        };
        let error = extract_batch(request).expect_err("duplicate paths must fail the envelope");
        assert!(error.contains("duplicate file path"));
    }
}
