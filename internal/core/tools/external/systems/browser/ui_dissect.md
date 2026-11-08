Dissect a live page into a structural map for UI review and replication.

Actions:
- map: measure the page, segment it into components, review the aesthetics, capture design tokens, and save a JSON + Markdown map under `.mocode/ui/`.
- review: run the same measurement but return only the aesthetic findings.
- replicate: emit a semantic HTML + CSS skeleton for one component (use the `cN` id from a map) and save it under `.mocode/ui/components/`.
- list: show previously saved maps.

Use a URL to open a page first, or omit it to measure the page already open in the browser session. Measurement is read-only; it does not click or navigate beyond the optional URL.
