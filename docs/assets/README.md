<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca> -->
<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# Docs Assets

This directory contains visual assets used by the README and GitHub Pages docs.

## Demo Recording

Files:

- `coding-ethos-demo.cast`: asciinema source recording.
- `coding-ethos-demo.gif`: rendered GIF used by README and docs.

Regenerate the GIF from the cast:

```bash
agg docs/assets/coding-ethos-demo.cast \
  docs/assets/coding-ethos-demo.gif
```

Regenerate the recording with `asciinema`:

```bash
asciinema record \
  --overwrite \
  --headless \
  --idle-time-limit 1.2 \
  --window-size 100x28 \
  --title "coding-ethos MCP and SARIF demo" \
  --command "bash docs/assets/record-demo.sh" \
  docs/assets/coding-ethos-demo.cast
```

Keep demo scripts deterministic. Do not record secrets, local-only paths,
tokens, private URLs, or interactive prompts.

## Social Preview

Files:

- `../social-preview.svg`: editable source.
- `../social-preview.png`: 1280x640 PNG uploaded to GitHub as the repository
  social preview image.

Regenerate the PNG:

```bash
rsvg-convert -w 1280 -h 640 docs/social-preview.svg \
  -o docs/social-preview.png
```

The social preview is a GitHub repository setting. Updating the checked-in PNG
does not update GitHub metadata until the image is uploaded in repository
settings.
