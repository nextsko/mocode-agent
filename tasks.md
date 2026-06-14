# FileMentionMenu Redesign — Task List

> **Plan:** file-mention-menu-redesign  
> **Status:** tasks  
> **Date:** 2025-01-09

---

## Implementation Tasks

### Task 1: Refactor MenuItem Component — Two-Tier Layout

- [x] 1.1. Create new `MenuItem` component with two-tier layout
    - *Goal*: Replace single-row layout with primary row (filename + size + badge) and secondary row (parent directory path)
    - *Details*: 
      - Primary row: `flex items-center gap-2` with FileIcon, filename (bold), size (right-aligned), TypeBadge
      - Secondary row: `pl-6` (24px indent matching icon + gap), parent directory path in muted text
      - Use `truncate` on filename and path to prevent overflow
      - Padding: `px-3 py-2` per item
    - *Requirements*: Feature 1 (Two-Tier Layout), Feature 7 (Section Headers with Counts)
    - *File*: `web/src/features/chat/file-mention-menu.tsx`

- [x] 1.2. Implement selection states with high contrast
    - *Goal*: Replace weak `bg-primary/10` with unmistakable active state
    - *Details*:
      - Default: `bg-transparent`
      - Hover: `bg-muted/60`
      - Active: `bg-primary/15 border-l-2 border-primary`
      - Active text: `text-foreground` on primary row, `text-primary/80` on secondary row
      - Add `transition-colors duration-150` for smooth state changes
      - Test contrast in both light and dark themes
    - *Requirements*: Feature 3 (Strong Selection State)
    - *File*: `web/src/features/chat/file-mention-menu.tsx`

- [x] 1.3. Add query match highlighting
    - *Goal*: Highlight matching characters in filename and path when user has typed a query
    - *Details*:
      - Create `highlightMatches(text, query)` utility function
      - Case-insensitive matching, highlight all occurrences
      - Highlight style: `bg-primary/20 text-primary font-semibold rounded-sm px-0.5`
      - Apply to both filename (primary row) and path (secondary row)
      - Memoize per item per query to avoid re-computation
    - *Requirements*: Feature 5 (Query Match Highlighting)
    - *File*: `web/src/features/chat/file-mention-menu.tsx` (or new utility file)

### Task 2: Implement Directory Grouping for Workspace Section

- [x] 2.1. Create `groupByDirectory()` utility function
    - *Goal*: Group workspace files by their parent directory path
    - *Details*:
      - Input: `MentionOption[]` from `sections.workspace`
      - Output: `DirectoryGroup[]` with `{ directory: string, items: MentionOption[] }`
      - Extract parent directory from `option.description` or `option.label`
      - Handle root-level files as `(root)` group
      - Sort groups: `(root)` first, then alphabetical
      - Wrap in `useMemo` for performance
    - *Requirements*: Feature 2 (Directory-Based Grouping)
    - *File*: `web/src/features/chat/file-mention-menu.tsx` (internal utility)

- [x] 2.2. Create `DirectoryGroup` sub-component
    - *Goal*: Render directory header and its file items as a visual cluster
    - *Details*:
      - Header: `flex items-center gap-1.5 px-3 py-1` with FolderIcon, directory path, file count
      - Header style: `bg-muted/30 text-muted-foreground/70 text-[10px]`
      - No hover/selection state on header (not interactive)
      - Items rendered below header with standard `MenuItem` component
      - No separator line between groups (spacing provides separation)
    - *Requirements*: Feature 2 (Directory-Based Grouping)
    - *File*: `web/src/features/chat/file-mention-menu.tsx`

- [x] 2.3. Implement smart path truncation for display
    - *Goal*: Show discriminative path information even when paths are long
    - *Details*:
      - Secondary row shows parent directory path (not full path)
      - If parent path > 40 chars, use smart truncation: `root/.../parent/`
      - Always preserve immediate parent directory name in full
      - Never truncate the filename (it's in the primary row)
    - *Requirements*: Feature 4 (Smart Path Truncation)
    - *File*: `web/src/features/chat/file-mention-menu.tsx` (internal utility)

### Task 3: Enhance Section Headers and Menu Footer

- [x] 3.1. Update `SectionHeader` with item count badge
    - *Goal*: Show `(N)` count next to section label
    - *Details*:
      - Format: `"PENDING UPLOADS (3)"` / `"WORKSPACE FILES (12)"`
      - Style: `text-[10px] font-semibold uppercase tracking-wide text-muted-foreground`
      - Padding: `px-3 py-1.5` (slightly increased from current `py-1`)
      - Count is dynamic based on filtered items in section
    - *Requirements*: Feature 7 (Section Headers with Counts)
    - *File*: `web/src/features/chat/file-mention-menu.tsx`

- [x] 3.2. Create `MenuFooter` with keyboard navigation hints
    - *Goal*: Display keyboard shortcuts at bottom of menu for discoverability
    - *Details*:
      - Content: `↑↓ navigate · ↵ select · esc close`
      - Use `kbd` elements for keys: `px-1 py-0.5 rounded bg-muted font-mono text-[9px]`
      - Footer style: `text-[10px] text-muted-foreground/60 border-t border-border/50`
      - Padding: `px-3 py-2`
      - Position: sticky at bottom of scrollable area (always visible)
    - *Requirements*: Feature 6 (Keyboard Navigation Hints)
    - *File*: `web/src/features/chat/file-mention-menu.tsx`

### Task 4: Integration and Polish

- [x] 4.1. Assemble complete `FileMentionMenu` component
    - *Goal*: Compose all sub-components into final menu structure
    - *Details*:
      - Popover container (unchanged from current)
      - ScrollArea for scrollable content
      - Section: Attachments (flat list of MenuItems)
      - Section: Workspace (DirectoryGroups containing MenuItems)
      - MenuFooter at bottom
      - Ensure `activeIndex` correctly maps across all items (flat index across sections and groups)
      - Maintain existing keyboard event handling (arrow keys, enter, escape)
    - *Requirements*: All features
    - *File*: `web/src/features/chat/file-mention-menu.tsx`

- [x] 4.2. Add accessibility attributes
    - *Goal*: Ensure screen reader and keyboard accessibility
    - *Details*:
      - Menu container: `role="listbox"`, `aria-activedescendant={activeId}`
      - Each item: `role="option"`, `aria-selected={isActive}`
      - Section headers: `role="group"`, `aria-label`
      - Directory headers: `role="group"`, `aria-label`
      - Ensure focus management works with `tabIndex={-1}` on items
    - *Requirements*: Accessibility (Non-functional)
    - *File*: `web/src/features/chat/file-mention-menu.tsx`

- [x] 4.3. Performance optimization and memoization
    - *Goal*: Ensure smooth rendering with 500+ files
    - *Details*:
      - Wrap `MenuItem` in `React.memo`
      - Memoize `groupByDirectory` result with `useMemo`
      - Memoize `highlightMatches` results
      - Memoize flat item list for `activeIndex` lookup
      - Throttle `scrollIntoView` calls (only on `activeIndex` change)
      - Verify no unnecessary re-renders with React DevTools Profiler
    - *Requirements*: Performance (Non-functional)
    - *File*: `web/src/features/chat/file-mention-menu.tsx`

### Task 5: Testing and Validation

- [x] 5.1. Visual testing in both themes
    - *Goal*: Verify design looks correct in light and dark modes
    - *Details*:
      - Test selection state contrast in both themes
      - Test hover states
      - Test with long filenames and deep paths
      - Test with empty sections (no uploads, no workspace files)
      - Test with 500+ files (performance check)
    - *Requirements*: Visual Design acceptance criteria
    - *Method*: Manual browser testing

- [x] 5.2. Interaction testing
    - *Goal*: Verify all keyboard and mouse interactions work correctly
    - *Details*:
      - Arrow Up/Down cycles through all items across sections and directory groups
      - Enter selects active item and inserts mention
      - Escape closes menu without inserting
      - Mouse hover updates active index
      - Mouse click selects item
      - ScrollIntoView keeps active item visible when navigating
    - *Requirements*: Interaction acceptance criteria
    - *Method*: Manual browser testing

- [x] 5.3. Edge case testing
    - *Goal*: Handle unusual scenarios gracefully
    - *Details*:
      - Files with same name in different directories (distinguishable via path)
      - Very long filenames (>50 chars)
      - Very deep paths (>100 chars)
      - Query with no matches (show "No results" state)
      - Empty workspace (show appropriate message)
      - Empty attachments section (hide or show "No pending uploads")
    - *Requirements*: Edge case handling
    - *Method*: Manual browser testing with mock data

---

## Task Dependencies

```
Task 1.1 (MenuItem layout) ──┐
Task 1.2 (Selection states) ──┼──┐
Task 1.3 (Match highlighting)─┘  │
                                  ├──→ Task 4.1 (Assemble component)
Task 2.1 (groupByDirectory) ─────┐  │
Task 2.2 (DirectoryGroup) ────────┼──┘
Task 2.3 (Path truncation) ─────┘

Task 3.1 (SectionHeader) ────────┐
Task 3.2 (MenuFooter) ───────────┼──→ Task 4.1 (Assemble component)
                                 │
Task 4.2 (Accessibility) ────────┼──→ Task 4.1 (Assemble component)
Task 4.3 (Performance) ──────────┘

Task 4.1 ──→ Task 5.1 (Visual testing)
Task 4.1 ──→ Task 5.2 (Interaction testing)
Task 4.1 ──→ Task 5.3 (Edge case testing)
```

**Parallel execution groups:**
- **Group A**: Tasks 1.1, 1.2, 1.3 (MenuItem development) — can be done sequentially by one agent
- **Group B**: Tasks 2.1, 2.2, 2.3 (Directory grouping) — can be done sequentially by one agent
- **Group C**: Tasks 3.1, 3.2 (Headers and footer) — can be done in parallel with A and B
- **Group D**: Tasks 4.1, 4.2, 4.3 (Integration) — depends on A, B, C
- **Group E**: Tasks 5.1, 5.2, 5.3 (Testing) — depends on D

---

## Estimated Timeline

| Task | Estimated Time | Notes |
|------|-------------|-------|
| Task 1: MenuItem Component | 3 hours | Layout + selection + highlighting |
| Task 2: Directory Grouping | 2 hours | Algorithm + sub-component + truncation |
| Task 3: Headers and Footer | 1.5 hours | SectionHeader + MenuFooter |
| Task 4: Integration | 2 hours | Assembly + accessibility + performance |
| Task 5: Testing | 2 hours | Visual + interaction + edge cases |
| **Total** | **10.5 hours** | Can be compressed to 6-8 hours with parallel work |

---

## Risk Assessment

| Risk | Probability | Impact | Mitigation |
|------|------------|--------|-----------|
| `useFileMentions` hook interface mismatch | Low | High | Verify hook output shape before starting; no changes to hook |
| Theme contrast issues (selection state) | Medium | Medium | Test in both light/dark early; adjust opacity if needed |
| Performance with 500+ files | Low | Medium | Memoization strategy; test early with large mock dataset |
| Keyboard navigation regression | Medium | High | Maintain existing event handlers; test all key combinations |
| Scroll behavior issues | Low | Low | Use `scrollIntoView({ block: 'nearest' })`; test with tall menus |

---

## Files to Modify

| File | Change Type | Description |
|------|------------|-------------|
| `web/src/features/chat/file-mention-menu.tsx` | Major rewrite | Complete component redesign |
| `web/src/features/chat/useFileMentions.ts` | No changes | Hook contract preserved |
| `web/src/features/chat/components/chat-prompt-composer.tsx` | No changes | Consumer unchanged |
