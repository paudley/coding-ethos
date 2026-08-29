// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

use std::io::{self, Read, Write};
use std::process::ExitCode;

use coding_ethos_purrdf_extractor::{BatchRequest, extract_batch};

fn main() -> ExitCode {
    match run() {
        Ok(()) => ExitCode::SUCCESS,
        Err(Failure::Protocol(message)) => {
            eprintln!("coding-ethos-purrdf-extractor: protocol error: {message}");
            ExitCode::from(2)
        }
        Err(Failure::Internal(message)) => {
            eprintln!("coding-ethos-purrdf-extractor: internal error: {message}");
            ExitCode::FAILURE
        }
    }
}

fn run() -> Result<(), Failure> {
    let mut input = Vec::new();
    io::stdin()
        .read_to_end(&mut input)
        .map_err(|error| Failure::Internal(format!("cannot read stdin: {error}")))?;
    let request: BatchRequest = serde_json::from_slice(&input)
        .map_err(|error| Failure::Protocol(format!("invalid request JSON: {error}")))?;
    let outcome = extract_batch(request).map_err(Failure::Protocol)?;
    for diagnostic in outcome.diagnostics {
        eprintln!("coding-ethos-purrdf-extractor: {diagnostic}");
    }

    let mut output = serde_json::to_vec(&outcome.response)
        .map_err(|error| Failure::Internal(format!("cannot encode response JSON: {error}")))?;
    output.push(b'\n');
    io::stdout()
        .write_all(&output)
        .map_err(|error| Failure::Internal(format!("cannot write stdout: {error}")))?;
    Ok(())
}

enum Failure {
    Protocol(String),
    Internal(String),
}
