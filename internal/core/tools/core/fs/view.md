Read a file by path with line numbers; supports offset and line limit (default 2000, max 200KB); renders images; also resolves internal `mocode://` resources (skills, docs, experience notes, GitHub files, the live browser session).

<usage>
- Provide file path to read
- Optional offset: start reading from specific line (0-based)
- Optional limit: control lines read (default 2000)
- Don't use for directories (use LS tool instead)
- Supports image files (PNG, JPEG, GIF, BMP, SVG, WebP)
- Accepts `mocode://` references as file_path (see below)
</usage>

<internal_resources>
Beyond files on disk, `file_path` may be an internal resource:

- `mocode://skills/<name>/SKILL.md` — an installed skill (builtin or user).
- `mocode://skills/<name>/references/<file>.md` — skills ship reference material alongside SKILL.md;
  read those the same way.
- `mocode://docs/<path>` — the workspace's documentation roots (design notes, architecture, guides).
- `mocode://exp/<NNN>-<title>.md` — the experience-notes library: durable write-ups of problems solved.
- `mocode://github/<owner>/<repo>/<path>` — a file in a public GitHub repository. Add `?ref=<branch|tag|sha>`
  to pin a revision (default `HEAD`). Address a directory to list it.
- `mocode://browser/<resource>` — the live browser session: `page`, `snapshot`, `text`, `console`, `screenshot`.
- `mocode://memory/` — the memory index (every entry, plus where each session's memory lives).
  Individual entries: `mocode://memory/global/<file>.md`, `mocode://memory/sessions/<session>/<file>.md`.

Address a namespace with **no path** to list it: `mocode://skills`, `mocode://docs`, `mocode://github/owner/repo`.
A mistyped reference reports the available namespaces rather than failing silently.
</internal_resources>

<features>
- Displays contents with line numbers
- Can read from any file position using offset
- Handles large files by limiting lines read
- Auto-truncates very long lines for display
- Suggests similar filenames when file not found
- Renders image files directly in terminal
- Resolves internal `mocode://` resources through one registry
</features>

<limitations>
- Max file size: 200KB (skills are exempt and read whole)
- Default limit: 2000 lines
- Lines >2000 chars truncated
- Binary files (except images) cannot be displayed
- GitHub reads are capped at 1 MiB per file
- GitHub directory listings use the unauthenticated API (60 requests/hour); set GITHUB_TOKEN to raise it
- `mocode://browser/*` needs a running session (`browser` tool, `action=launch`)
</limitations>

<cross_platform>
- Handles Windows (CRLF) and Unix (LF) line endings
- Works with forward slashes (/) and backslashes (\)
- Auto-detects text encoding for common formats
- Internal resource paths always use forward slashes
</cross_platform>

<tips>
- Use with Glob to find files first
- For code exploration: Grep to find relevant files, then View to examine
- For large files: use offset parameter for specific sections
- Prefer `mocode://github/...` over fetching a raw URL: it handles refs, proxies and images for you
- Read `mocode://skills` once to see what is installed rather than guessing skill names
</tips>
