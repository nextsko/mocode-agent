Dissect a live page into a structural map for UI review, regression and replication.

Actions:
- map: measure the page, segment it into components, review the aesthetics, capture design tokens, and save a JSON + Markdown map under `.mocode/ui/`.
- review: same measurement, but return only the aesthetic findings.
- replicate: emit a component skeleton in `target` (html / react / vue / svelte) for one `cN` component id; saved under `.mocode/ui/components/`.
- diff: compare two maps (visual regression). Pass `url` to compare the live page against the newest baseline, or omit it to diff the two most recent saved maps.
- system: mine the site design system (recurring component shapes + tokens) across all saved maps.
- brand: capture the current page's design tokens as the brand baseline (`.mocode/ui/brand.json`).
- tokens: compare the current page against the brand baseline and report off-brand / unused values.
- skill: stage a `learn`-ready SKILL.md draft from the page (or one component) under `.mocode/ui/skills/`.
- list: show previously saved maps.

Notes:
- Use `url` to open a page first, or omit it to measure the page already open in the browser session.
- Without a usable browser the tool falls back to static HTML: structure and semantics work, but boxes/styles are empty and layout/contrast findings are skipped.
- Measurement is read-only; only `url` navigates.
