Dissect a live page into structure for UI review, regression, replication and taste retrospective.

Actions:
- map: measure the page, segment components, review aesthetics, capture tokens, save JSON + Markdown under `.mocode/ui/`, and refresh the site design system + taste retrospective.
- review: same measurement, only the aesthetic findings.
- replicate: component skeleton in `target` (html / react / vue / svelte) for one `cN` id.
- diff: visual regression (box-level). With `url` compares live page vs newest baseline; without it diffs the two most recent maps.
- system: mine recurring component shapes + tokens across saved maps.
- brand: capture the current page's tokens as the brand baseline.
- tokens: compare current page against the brand baseline (off-brand / unused).
- skill: stage a learn-ready SKILL.md draft for the page or one component.
- dynamic: execute interaction `steps` (click/fill/press/hover/scroll/wait), measuring a state after each step plus the diff between consecutive states. Requires a live browser.
- retro: aesthetic retrospective across all saved maps — score trend, recurring issues, and the rules the project should adopt.
- list: show saved maps.

Notes:
- `url` opens a page first; omit it to use the current browser page.
- Without a usable browser the tool falls back to static HTML (structure only); dynamic capture deliberately refuses without a browser.
- Measurement is read-only except `url` navigation and dynamic steps.
