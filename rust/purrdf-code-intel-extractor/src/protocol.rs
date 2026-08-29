// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

pub const PROTOCOL: &str = "coding-ethos.external-extractor.v1";
pub const PURRDF_REVISION: &str = "3aa4ba514ee9cdbfb6966bd0f1fedb87e0d8a0b6";

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct BatchRequest {
    pub protocol: String,
    pub request_id: String,
    pub files: Vec<FileRequest>,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct FileRequest {
    pub path: String,
    pub content_sha256: String,
    pub content: String,
    #[serde(default)]
    pub base_iri: Option<String>,
}

#[derive(Clone, Debug, Serialize)]
pub struct BatchResponse {
    pub protocol: &'static str,
    pub request_id: String,
    pub extractor: ExtractorIdentity,
    pub results: Vec<FileResult>,
}

#[derive(Clone, Copy, Debug, Serialize)]
pub struct ExtractorIdentity {
    pub name: &'static str,
    pub version: &'static str,
    pub purrdf_revision: &'static str,
}

impl Default for ExtractorIdentity {
    fn default() -> Self {
        Self {
            name: "purrdf",
            version: "1",
            purrdf_revision: PURRDF_REVISION,
        }
    }
}

#[derive(Clone, Debug, Serialize)]
pub struct FileResult {
    pub path: String,
    pub content_sha256: String,
    pub language: String,
    pub status: FileStatus,
    pub document_kind: Option<String>,
    pub facts: Vec<SemanticFact>,
    pub error: Option<String>,
}

impl FileResult {
    pub fn success(
        file: &FileRequest,
        language: &str,
        document_kind: &str,
        mut facts: Vec<SemanticFact>,
    ) -> Self {
        facts.sort_by(|left, right| left.id.cmp(&right.id));
        Self {
            path: file.path.clone(),
            content_sha256: file.content_sha256.clone(),
            language: language.to_owned(),
            status: FileStatus::Ok,
            document_kind: Some(document_kind.to_owned()),
            facts,
            error: None,
        }
    }

    pub fn error(file: &FileRequest, language: &str, message: String) -> Self {
        Self {
            path: file.path.clone(),
            content_sha256: file.content_sha256.clone(),
            language: language.to_owned(),
            status: FileStatus::Error,
            document_kind: None,
            facts: Vec::new(),
            error: Some(message),
        }
    }
}

#[derive(Clone, Copy, Debug, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum FileStatus {
    Ok,
    Error,
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum SemanticTerm {
    Iri {
        value: String,
    },
    Blank {
        value: String,
    },
    Literal {
        value: String,
        datatype: Option<String>,
        language: Option<String>,
        direction: Option<String>,
    },
    QuotedTriple {
        subject: Box<Self>,
        predicate: String,
        object: Box<Self>,
    },
    Variable {
        value: String,
    },
}

#[derive(Clone, Debug, Serialize)]
pub struct SemanticFact {
    pub id: String,
    pub kind: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub subject: Option<SemanticTerm>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub predicate: Option<SemanticTerm>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub object: Option<SemanticTerm>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub graph: Option<SemanticTerm>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub value: Option<String>,
    pub attributes: BTreeMap<String, String>,
    pub provenance: Provenance,
}

#[derive(Serialize)]
struct FactIdentity<'a> {
    kind: &'a str,
    subject: &'a Option<SemanticTerm>,
    predicate: &'a Option<SemanticTerm>,
    object: &'a Option<SemanticTerm>,
    graph: &'a Option<SemanticTerm>,
    value: &'a Option<String>,
    attributes: &'a BTreeMap<String, String>,
    provenance: &'a Provenance,
}

impl SemanticFact {
    #[allow(clippy::too_many_arguments)]
    pub fn new(
        kind: impl Into<String>,
        subject: Option<SemanticTerm>,
        predicate: Option<SemanticTerm>,
        object: Option<SemanticTerm>,
        graph: Option<SemanticTerm>,
        value: Option<String>,
        attributes: BTreeMap<String, String>,
        provenance: Provenance,
    ) -> Result<Self, String> {
        let kind = kind.into();
        let identity = FactIdentity {
            kind: &kind,
            subject: &subject,
            predicate: &predicate,
            object: &object,
            graph: &graph,
            value: &value,
            attributes: &attributes,
            provenance: &provenance,
        };
        let encoded = serde_json::to_vec(&identity)
            .map_err(|error| format!("cannot encode semantic fact identity: {error}"))?;
        let id = format!("sha256:{}", sha256_hex(&encoded));
        Ok(Self {
            id,
            kind,
            subject,
            predicate,
            object,
            graph,
            value,
            attributes,
            provenance,
        })
    }
}

#[derive(Clone, Debug, Serialize)]
pub struct Provenance {
    pub class: &'static str,
    pub parser: &'static str,
    pub parser_revision: &'static str,
    pub span_fidelity: SpanFidelity,
    pub source_path: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub start: Option<SourceStart>,
}

impl Provenance {
    pub fn rdf(source_path: &str, start: Option<SourceStart>) -> Self {
        Self {
            class: "EXTRACTED",
            parser: "purrdf-rdf",
            parser_revision: PURRDF_REVISION,
            span_fidelity: SpanFidelity::SubjectStart,
            source_path: source_path.to_owned(),
            start,
        }
    }

    pub fn sparql(source_path: &str) -> Self {
        Self {
            class: "EXTRACTED",
            parser: "purrdf-sparql-algebra",
            parser_revision: PURRDF_REVISION,
            span_fidelity: SpanFidelity::None,
            source_path: source_path.to_owned(),
            start: None,
        }
    }
}

#[derive(Clone, Copy, Debug, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum SpanFidelity {
    SubjectStart,
    None,
}

#[derive(Clone, Copy, Debug, Serialize)]
pub struct SourceStart {
    pub byte_offset: usize,
    pub line: u32,
    pub column: u32,
}

pub fn sha256_hex(bytes: &[u8]) -> String {
    let digest = Sha256::digest(bytes);
    let mut encoded = String::with_capacity(digest.len() * 2);
    for byte in digest {
        use std::fmt::Write as _;
        let _ = write!(encoded, "{byte:02x}");
    }
    encoded
}

pub fn string_attributes(entries: &[(&str, String)]) -> BTreeMap<String, String> {
    entries
        .iter()
        .map(|(key, value)| ((*key).to_owned(), value.clone()))
        .collect()
}
