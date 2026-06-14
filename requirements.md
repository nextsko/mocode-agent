# FileMentionMenu Redesign — Requirements Document

> **Plan:** file-mention-menu-redesign  
> **Status:** requirements  
> **Date:** 2025-01-09

---

## 1. Problem Statement

The current `FileMentionMenu` component (`web/src/features/chat/file-mention-menu.tsx`) has significant UX and visual design problems that degrade the user experience when mentioning files in chat:

1. **Cluttered single-line layout**: All information (icon, filename, full path, file size, type badge) is crammed into a single row, creating visual competition and poor scannability.
2. **Weak selection state**: The active item uses `bg-primary/10` (10% opacity) with a 1px ring — barely visible against the popover background, especially in bright environments.
3. **No directory grouping**: Workspace files are displayed as a flat list, making it hard to understand project structure or locate files in deep directories.
4. **Poor truncation strategy**: Long paths are simply truncated with CSS `truncate`, losing critical differentiation information when multiple files share the same basename.
5. **Inconsistent information density**: Some items show file size, others don't; the type badge competes with the filename for attention.
6. **No keyboard navigation hints**: Users must discover arrow keys, Enter, and Escape through trial and error.
7. **Missing match highlighting**: When the user types a query, matching characters are not visually emphasized in the results.

---

## 2. Core Features

### Feature 1: Two-Tier Layout (Primary + Secondary)
Each menu item should display information across two visual tiers:
- **Primary row**: Icon + filename (bold, prominent)
- **Secondary row**: Path breadcrumb + file size + action hint (muted, smaller)

This separates "what" (filename) from "where/how" (path, size, action), reducing cognitive load.

### Feature 2: Directory-Based Grouping
Workspace files should be grouped by their parent directory:
- Files in the same directory are visually clustered under a directory header
- Directory headers show the relative path (e.g., `src/components/`, `docs/`)
- Root-level files appear under a "(root)" group
- Groups are collapsible (optional for v1, but structure should support it)

### Feature 3: Strong Selection State
The active item must have unambiguous visual prominence:
- Background: solid `bg-primary` or high-opacity tint (`bg-primary/20` minimum)
- Text: `text-primary-foreground` for maximum contrast
- Left border accent: `border-l-2 border-primary` on the active item
- Smooth transition: `transition-colors duration-150`

### Feature 4: Smart Path Truncation
When paths are long, preserve the most discriminative segments:
- Always show the **filename** in full (primary row)
- In the secondary row, show the **parent directory path**
- If the parent path is still too long, truncate from the **middle** (keep root and immediate parent), not the end
- Use a subtle `…` indicator for truncated segments

### Feature 5: Query Match Highlighting
When the user has typed a query after `@`:
- Matching characters in filenames and paths should be highlighted
- Highlight style: `text-primary font-semibold` or `bg-primary/20 rounded-sm`
- Case-insensitive matching

### Feature 6: Keyboard Navigation Hints
The footer should display contextual keyboard shortcuts:
- `↑↓` navigate
- `↵` select / `␛` close
- Optionally: `⌘+click` to preview (if supported)

### Feature 7: Section Headers with Counts
The existing two sections (Attachments / Workspace) should be enhanced:
- Section label: uppercase, small, muted
- **Item count badge**: show `(N)` next to the section label
- Visual separator between sections (subtle border or spacing)

---

## 3. User Stories

- **As a** developer working in a large codebase, **I want** files grouped by directory in the mention menu, **so that** I can quickly locate files in the module I'm working on without scanning a flat list of 500+ files.

- **As a** user typing `@config` to find configuration files, **I want** matching characters highlighted in the results, **so that** I can confirm the filter is working and identify the correct file at a glance.

- **As a** user navigating the mention menu with arrow keys, **I want** the selected item to have strong visual contrast, **so that** I never lose track of which item is active, even in bright lighting or with peripheral vision.

- **As a** user mentioning a file deep in the project structure, **I want** to see the full parent path (or a smart truncation), **so that** I can distinguish between `src/utils.ts` and `test/utils.ts`.

- **As a** first-time user, **I want** to see keyboard shortcut hints at the bottom of the menu, **so that** I can learn to navigate efficiently without reading documentation.

- **As a** user with pending file uploads and workspace files, **I want** clear separation between "files I'm about to upload" and "files already in the project", **so that** I don't accidentally reference the wrong source.

---

## 4. Acceptance Criteria

### Visual Design
- [ ] Each menu item uses a two-tier layout (primary + secondary rows)
- [ ] Active item has `bg-primary/20` or higher opacity, plus left border accent
- [ ] Section headers show item count in parentheses: "WORKSPACE FILES (12)"
- [ ] Directory grouping headers are visible when workspace has files in multiple directories
- [ ] Keyboard shortcut hints are visible in the footer when menu is open

### Interaction
- [ ] Arrow Up/Down cycles through all items across sections and groups
- [ ] Enter selects the active item and inserts the mention
- [ ] Escape closes the menu without inserting
- [ ] Mouse hover updates the active index
- [ ] Mouse click selects the hovered item

### Data Display
- [ ] File size is displayed in human-readable format (KB, MB) when available
- [ ] Path display uses smart truncation (preserves filename and immediate parent)
- [ ] Query matches are visually highlighted in both filename and path
- [ ] Duplicate filenames from different directories are visually distinguishable

### Performance
- [ ] Menu renders without noticeable lag with up to 500 workspace files
- [ ] Keyboard navigation is smooth (no jank when cycling through items)
- [ ] ScrollIntoView behavior is smooth and doesn't cause layout shifts

### Accessibility
- [ ] All interactive elements are keyboard-navigable
- [ ] Active item has sufficient color contrast (WCAG AA minimum)
- [ ] Screen reader announces the selected item and its context

---

## 5. Non-functional Requirements

### Performance
- Grouping and sorting of workspace files should happen in a `useMemo` to avoid re-computation on every render
- Match highlighting should not require additional re-renders beyond the existing filter logic
- Virtual scrolling is not required for v1 (500 items is manageable with native scroll), but the component structure should not prevent adding it later

### Compatibility
- The redesign must maintain the existing `FileMentionMenuProps` interface (or extend it minimally)
- The `useFileMentions` hook should require no changes, or only additive changes
- All existing consumers of `FileMentionMenu` must continue to work without modification

### Theme Support
- All colors must use CSS variables / Tailwind theme tokens (`bg-primary`, `text-muted-foreground`, etc.)
- The component must work in both light and dark modes
- Selection state must be visible in both modes

---

## 6. Out of Scope (for this iteration)

- Virtual scrolling for >500 items
- Collapsible/expandable directory groups
- Multi-select mentions
- File preview on hover
- Recent/frequently-mentioned files section
- Drag-and-drop file attachment integration
- Custom file type icons beyond the current two (attachment / workspace)

---

## 7. Related Files

| File | Responsibility |
|---|---|
| `web/src/features/chat/file-mention-menu.tsx` | **Main component to redesign** |
| `web/src/features/chat/useFileMentions.ts` | Hook providing data (should need minimal changes) |
| `web/src/features/chat/components/chat-prompt-composer.tsx` | Consumer of FileMentionMenu |
| `web/src/components/ui/` | shadcn/ui base components (Button, Tooltip, etc.) |

---

## 8. Design References

- **VS Code Quick Open**: fuzzy matching, match highlighting, two-line items
- **GitHub file finder**: directory grouping, path truncation
- **Figma command palette**: strong selection state, keyboard hints
- **Linear command menu**: clean two-tier layout, subtle grouping
