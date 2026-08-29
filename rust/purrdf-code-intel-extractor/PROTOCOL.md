<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# PurRDF external extractor protocol

`coding-ethos-purrdf-extractor` is a one-shot content transformer. It reads one
UTF-8 JSON request from standard input, writes one compact JSON response plus a
newline to standard output, writes human diagnostics to standard error, and
exits. It does not read source files, open sockets, or provide a daemon mode.

The protocol discriminator is exactly:

```text
coding-ethos.external-extractor.v1
```

## Request

```json
{
  "protocol": "coding-ethos.external-extractor.v1",
  "request_id": "opaque-caller-id",
  "files": [
    {
      "path": "ontology/example.ttl",
      "content_sha256": "351c7056c2164c6789cd6cf6d91e4194719945e1c5e8f19cc9bc1778ece0d0b4",
      "content": "@prefix ex: <https://example.org/> .\nex:s ex:p ex:o .\n",
      "base_iri": "https://example.org/source/"
    }
  ]
}
```

`base_iri` is optional. Unknown fields, an empty request ID or path, duplicate
paths, malformed hashes, and an unsupported protocol are envelope errors.
`content_sha256` is SHA-256 over the exact UTF-8 bytes in `content`. The path is
source identity only; the extractor never opens it. Its case-insensitive suffix
selects the parser:

| Suffix | Language | PurRDF media type |
| --- | --- | --- |
| `.ttl` | `turtle` | `text/turtle` |
| `.trig` | `trig` | `application/trig` |
| `.nt` | `ntriples` | `application/n-triples` |
| `.nq` | `nquads` | `application/n-quads` |
| `.rq`, `.sparql` | `sparql` | query parse, then update parse |

## Response

```json
{
  "protocol": "coding-ethos.external-extractor.v1",
  "request_id": "opaque-caller-id",
  "extractor": {
    "name": "purrdf",
    "version": "1",
    "purrdf_revision": "3aa4ba514ee9cdbfb6966bd0f1fedb87e0d8a0b6"
  },
  "results": [
    {
      "path": "ontology/example.ttl",
      "content_sha256": "351c7056c2164c6789cd6cf6d91e4194719945e1c5e8f19cc9bc1778ece0d0b4",
      "language": "turtle",
      "status": "ok",
      "document_kind": "dataset",
      "facts": [],
      "error": null
    }
  ]
}
```

Results are sorted by path. Valid envelopes exit zero even when an individual
file has `status: "error"`; that result has a null `document_kind`, no facts,
and a structured error string. The same file error is mirrored to standard
error for operators. Envelope errors exit 2 without a response. Internal I/O or
JSON serialization errors exit 1.

## Facts and terms

Every fact has this shape; optional semantic positions are absent rather than
null:

```json
{
  "id": "sha256:...",
  "kind": "rdf_quad",
  "subject": { "kind": "iri", "value": "https://example.org/s" },
  "predicate": { "kind": "iri", "value": "https://example.org/p" },
  "object": { "kind": "iri", "value": "https://example.org/o" },
  "attributes": {
    "language": "turtle",
    "media_type": "text/turtle"
  },
  "provenance": {
    "class": "EXTRACTED",
    "parser": "purrdf-rdf",
    "parser_revision": "3aa4ba514ee9cdbfb6966bd0f1fedb87e0d8a0b6",
    "span_fidelity": "subject_start",
    "source_path": "ontology/example.ttl",
    "start": { "byte_offset": 38, "line": 2, "column": 1 }
  }
}
```

Term tags are `iri`, `blank`, `literal`, `quoted_triple`, and `variable`.
Literal terms carry `value`, nullable `datatype`, nullable `language`, and
nullable `direction`. A quoted-triple term recursively carries `subject`, an IRI
string `predicate`, and `object`. Blank values include the `_:` prefix; variable
values omit the `?` or `$` sigil.

RDF fact kinds are `rdf_quad`, `rdf_reifier`, and `rdf_annotation`. SPARQL facts
describe the parsed public algebra, including document/query form, triple and
quad patterns, dataset/graph/service scopes, expressions, property paths,
VALUES cells, functions, bindings, and update operations.

Fact IDs are SHA-256 over the canonical semantic payload excluding the ID
itself. Attributes use sorted keys, SPARQL traversal carries a deterministic
ordinal, facts are sorted by ID, and repeated extraction of the same request is
byte-identical.

## Location fidelity

RDF parsing always calls PurRDF `parse_dataset_with` with
`track_source_spans: true`. PurRDF exposes the first source position for a
lexical named-node or blank-node subject, not a full statement range. Therefore
RDF provenance says `subject_start`, and `start` may be absent for quoted-triple
subjects. Lines and columns are one-based; byte offsets are zero-based.

The public SPARQL algebra has no physical source spans. Every SPARQL fact says
`span_fidelity: "none"` and omits `start`.
