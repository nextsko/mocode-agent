# FileMentionMenu Redesign — Design Document

> **Status:** design  
> **Date:** 2025-01-09  
> **Scope:** Visual redesign of `FileMentionMenu` component (`web/src/features/chat/file-mention-menu.tsx`) with improved grouping, layout, selection states, and UX.

---

## 1. Problem & Vision

The current `FileMentionMenu` is a functional but visually cluttered component. All information (icon, filename, path, size, type badge) is compressed into a single row with weak selection contrast and no directory grouping. In projects with hundreds of files, this creates cognitive overload and selection errors.

**Vision:** A clean, scannable file mention menu that:
- Separates "what" (filename) from "where" (path) via two-tier layout
- Groups workspace files by parent directory for spatial memory
- Provides unambiguous selection state with high contrast
- Highlights query matches to confirm filter intent
- Surfaces keyboard shortcuts for power users

---

## 2. Goals & Non-Goals

### Goals
- Redesign `FileMentionMenu` with two-tier item layout and directory grouping
- Achieve WCAG AA contrast for selection states in both light and dark themes
- Maintain full backward compatibility with `useFileMentions` hook and existing consumers
- Keep performance smooth with 500+ files (no virtual scrolling needed yet)
- Preserve all existing keyboard and mouse interactions

### Non-Goals
- No changes to `useFileMentions.ts` data layer (hook remains untouched)
- No new data fetching or backend changes
- No virtual scrolling (out of scope per requirements)
- No collapsible directory groups (structure supports it, UI deferred)
- No multi-select or drag-and-drop

---

## 3. External Research Summary

| System | Pattern | Adopt | Avoid |
|---|---|---|---|
| **VS Code Quick Open** | Two-line items (filename + path), match highlighting, fuzzy scoring | Two-line layout; match highlighting; strong selection contrast | Fuzzy scoring algorithm (we keep simple `includes` filter) |
| **GitHub Cmd+T** | Directory grouping, path breadcrumbs, file icons | Directory grouping; path truncation strategy | Full file tree expansion |
| **Figma Command Menu** | Clean spacing, keyboard hints footer, subtle section labels | Keyboard hints footer; section spacing | Command palette pattern (not applicable) |
| **Linear Cmd+K** | Minimal two-line items, muted secondary text, strong hover states | Two-line density; muted secondary text | Full command palette scope |
| **Windsurf @ Context** | Flat list, prefix-based typing, action labels | Nothing — serves as anti-pattern reference | Flat ungrouped list; weak selection; no match highlighting |

### Best Practices Distilled
1. **Two-tier layout** separates primary and secondary information into visual hierarchy
2. **Directory grouping** leverages spatial memory; users remember "it's in the components folder"
3. **Selection state** must be unmistakable: background + text color + structural accent (left border)
4. **Match highlighting** confirms to the user that their filter is working
5. **Keyboard hints** teach shortcuts without requiring documentation
6. **Consistent spacing rhythm** creates scannable patterns (8px/12px/16px system)

---

## 4. Architecture

### 4.1 Component Hierarchy

```text
FileMentionMenu (Popover container)
├── MenuHeader (query display — optional)
├── ScrollArea
│   ├── Section (Attachments)
│   │   ├── SectionHeader "PENDING UPLOADS (N)"
│   │   └── MenuItem[] (flat list — uploads are not grouped by dir)
│   └── Section (Workspace)
│       ├── SectionHeader "WORKSPACE FILES (N)"
│       ├── DirectoryGroup "(root)"
│       │   └── MenuItem[]
│       ├── DirectoryGroup "src/components/"
│       │   └── MenuItem[]
│       └── DirectoryGroup "docs/"
│           └── MenuItem[]
└── MenuFooter
    └── KeyboardHints "↑↓ navigate · ↵ select · esc close"
```

### 4.2 Data Flow

```text
useFileMentions hook (unchanged)
    │
    ├── sections.attachments (MentionOption[])
    │       └── Section "Pending Uploads" → flat MenuItem[]
    │
    └── sections.workspace (MentionOption[])
            └── groupByDirectory() → DirectoryGroup[] → MenuItem[]
```

The grouping transformation happens in a `useMemo` inside `FileMentionMenu`, not in the hook.

---

## 5. Component Design

### 5.1 MenuItem — Two-Tier Layout

```
┌─────────────────────────────────────────────────────────────┐
│ [icon]  filename.txt                      [size] [badge]  │  ← Primary Row
│         src/components/                          4.2 KB     │  ← Secondary Row
└─────────────────────────────────────────────────────────────┘
```

**Structure:**
```tsx
<button className="flex flex-col w-full ...">
  {/* Primary Row */}
  <div className="flex items-center gap-2">
    <FileIcon className="size-4 shrink-0" />
    <span className="font-medium truncate">{filename}</span>
    <span className="ml-auto text-[10px] tabular-nums">{size}</span>
    <TypeBadge>{type}</TypeBadge>
  </div>
  {/* Secondary Row */}
  <div className="flex items-center pl-6 text-xs text-muted-foreground">
    <span className="truncate">{parentPath}</span>
  </div>
</button>
```

**Spacing:**
- Item padding: `px-3 py-2` (12px horizontal, 8px vertical)
- Gap between primary and secondary row: `gap-0.5` (2px)
- Icon to text gap: `gap-2` (8px)
- Secondary row indent: `pl-6` (24px = icon width + gap)

**Selection State:**
```
Default:    bg-transparent, text-foreground
Hover:      bg-muted/60
Active:     bg-primary/15, border-l-2 border-primary, 
            text-foreground (primary row), 
            text-primary/80 (secondary row)
```

### 5.2 DirectoryGroup

```
┌─────────────────────────────────────────────────────────────┐
│  📁 src/components/                                         │  ← Directory Header
├─────────────────────────────────────────────────────────────┤
│ [icon]  Button.tsx                                          │
│         src/components/                            2.1 KB   │
├─────────────────────────────────────────────────────────────┤
│ [icon]  Input.tsx                                           │
│         src/components/                            1.8 KB   │
└─────────────────────────────────────────────────────────────┘
```

**Directory Header:**
```tsx
<div className="flex items-center gap-1.5 px-3 py-1 text-[10px] uppercase tracking-wider text-muted-foreground/70">
  <FolderIcon className="size-3" />
  <span>{directoryPath}</span>
  <span className="ml-auto">({fileCount})</span>
</div>
```

**Styling:**
- Background: `bg-muted/30` (very subtle, distinguishes from items)
- Text: `text-muted-foreground/70` (more muted than items)
- No hover/selection state (header is not interactive)

### 5.3 SectionHeader

```
┌─────────────────────────────────────────────────────────────┐
│  PENDING UPLOADS (3)                                        │
├─────────────────────────────────────────────────────────────┤
```

```tsx
<div className="px-3 py-1.5 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
  {label} ({count})
</div>
```

**Styling:**
- Same as current implementation but **with count badge**
- Padding slightly increased: `py-1.5` (6px) for better visual separation

### 5.4 MenuFooter

```
┌─────────────────────────────────────────────────────────────┐
│  ↑↓ navigate · ↵ select · esc close                        │
└─────────────────────────────────────────────────────────────┘
```

```tsx
<div className="flex items-center justify-between px-3 py-2 text-[10px] text-muted-foreground/60 border-t border-border/50">
  <div className="flex items-center gap-1">
    <kbd className="px-1 py-0.5 rounded bg-muted font-mono text-[9px]">↑↓</kbd>
    <span>navigate</span>
  </div>
  <div className="flex items-center gap-3">
    <span className="flex items-center gap-1">
      <kbd className="px-1 py-0.5 rounded bg-muted font-mono text-[9px]">↵</kbd>
      <span>select</span>
    </span>
    <span className="flex items-center gap-1">
      <kbd className="px-1 py-0.5 rounded bg-muted font-mono text-[9px]">esc</kbd>
      <span>close</span>
    </span>
  </div>
</div>
```

**Styling:**
- `kbd` elements use `bg-muted` for subtle keycap appearance
- Text is `text-muted-foreground/60` — visible but not distracting
- Border-top separates footer from scrollable content

---

## 6. Data Models & Transformations

### 6.1 Grouping Algorithm

```typescript
// Input: MentionOption[] from useFileMentions
// Output: DirectoryGroup[]

interface DirectoryGroup {
  directory: string;        // e.g., "src/components/" or "(root)"
  items: MentionOption[];
}

function groupByDirectory(options: MentionOption[]): DirectoryGroup[] {
  const groups = new Map<string, MentionOption[]>();
  
  for (const option of options) {
    const path = option.description || option.label;
    const lastSlash = path.lastIndexOf('/');
    const directory = lastSlash > 0 ? path.substring(0, lastSlash + 1) : '(root)';
    
    if (!groups.has(directory)) {
      groups.set(directory, []);
    }
    groups.get(directory)!.push(option);
  }
  
  // Sort: root first, then alphabetical
  const entries = Array.from(groups.entries());
  entries.sort(([a], [b]) => {
    if (a === '(root)') return -1;
    if (b === '(root)') return 1;
    return a.localeCompare(b);
  });
  
  return entries.map(([directory, items]) => ({ directory, items }));
}
```

### 6.2 Path Truncation (Smart)

```typescript
function truncatePath(path: string, maxLength: number = 40): string {
  if (path.length <= maxLength) return path;
  
  const parts = path.split('/');
  const filename = parts.pop() || '';
  const parentDir = parts.pop() || '';
  
  // Strategy: show root + ... + parent + filename
  // e.g., "src/.../components/Button.tsx"
  if (parts.length > 0) {
    const root = parts[0];
    return `${root}/.../${parentDir}/${filename}`;
  }
  
  return `.../${parentDir}/${filename}`;
}
```

**Visual display in secondary row:**
- Always show the **immediate parent directory** in full
- If the full path is longer than ~40 chars, use smart truncation
- The secondary row shows the **parent directory path** (not the full path)

### 6.3 Match Highlighting

```typescript
function highlightMatches(text: string, query: string): ReactNode {
  if (!query) return text;
  
  const lowerText = text.toLowerCase();
  const lowerQuery = query.toLowerCase();
  const parts: ReactNode[] = [];
  let lastIndex = 0;
  
  // Find all occurrences
  let index = lowerText.indexOf(lowerQuery, lastIndex);
  while (index !== -1) {
    // Add non-matching prefix
    if (index > lastIndex) {
      parts.push(text.substring(lastIndex, index));
    }
    // Add highlighted match
    parts.push(
      <span key={index} className="bg-primary/20 text-primary font-semibold rounded-sm px-0.5">
        {text.substring(index, index + query.length)}
      </span>
    );
    lastIndex = index + query.length;
    index = lowerText.indexOf(lowerQuery, lastIndex);
  }
  
  // Add remaining text
  if (lastIndex < text.length) {
    parts.push(text.substring(lastIndex));
  }
  
  return <>{parts}</>;
}
```

---

## 7. Interaction Design

### 7.1 Keyboard Navigation

| Key | Action |
|-----|--------|
| `↑` / `↓` | Move active index up/down across all items (crosses sections and directory groups) |
| `↵` / `Tab` | Select active item, insert mention, close menu |
| `Esc` | Close menu without inserting |
| `PageUp` / `PageDown` | Jump 5 items at a time |
| `Home` / `End` | Jump to first/last item |

**Scroll behavior:**
- When active index changes, `scrollIntoView({ block: 'nearest', behavior: 'smooth' })`
- Smooth scroll prevents jarring jumps

### 7.2 Mouse Interaction

| Action | Behavior |
|--------|----------|
| Hover | Update `activeIndex` to hovered item (immediate feedback) |
| Click | Select item, insert mention, close menu |
| Click outside | Close menu (handled by Popover) |

### 7.3 Active Index Management

The `activeIndex` is a **global flat index** across all visible items:

```
Attachments Section:
  [0] file1.txt
  [1] image.png
Workspace Section:
  Directory "(root)":
    [2] README.md
  Directory "src/components/":
    [3] Button.tsx
    [4] Input.tsx
  Directory "docs/":
    [5] API.md
```

This preserves the existing `useFileMentions` hook contract (which provides `flatOptions` with `order` field).

---

## 8. Styling System

### 8.1 Spacing Scale

| Token | Value | Usage |
|-------|-------|-------|
| `space-0.5` | 2px | Gap between primary/secondary row |
| `space-1` | 4px | Small internal gaps |
| `space-1.5` | 6px | Section header padding |
| `space-2` | 8px | Icon-to-text gap, directory header padding |
| `space-2.5` | 10px | — |
| `space-3` | 12px | Item horizontal padding, section horizontal padding |
| `space-4` | 16px | Popover padding |

### 8.2 Typography Scale

| Element | Size | Weight | Color |
|---------|------|--------|-------|
| Filename (primary) | `text-sm` (14px) | `font-medium` (500) | `text-foreground` |
| Path (secondary) | `text-xs` (12px) | `font-normal` (400) | `text-muted-foreground` |
| Section header | `text-[10px]` | `font-semibold` (600) | `text-muted-foreground` |
| Directory header | `text-[10px]` | `font-normal` (400) | `text-muted-foreground/70` |
| File size | `text-[10px]` | `font-normal` | `text-muted-foreground` |
| Type badge | `text-[10px]` | `font-medium` (500) | `text-muted-foreground` |
| Keyboard hints | `text-[10px]` | `font-normal` | `text-muted-foreground/60` |
| Kbd keys | `text-[9px]` | `font-mono` | `text-muted-foreground` |

### 8.3 Color Tokens (Theme-Aware)

| State | Background | Text | Border |
|-------|-----------|------|--------|
| Default | `transparent` | `text-foreground` | none |
| Hover | `bg-muted/60` | `text-foreground` | none |
| Active | `bg-primary/15` | `text-foreground` | `border-l-2 border-primary` |
| Active (secondary) | — | `text-primary/80` | — |
| Match highlight | `bg-primary/20` | `text-primary` | `rounded-sm` |
| Directory header | `bg-muted/30` | `text-muted-foreground/70` | none |

---

## 9. Accessibility

### 9.1 Keyboard
- All items are `<button>` elements (focusable by default)
- `tabIndex={-1}` on items (arrow keys only, no Tab cycling)
- `aria-activedescendant` on the menu container pointing to active item

### 9.2 Screen Reader
- `role="listbox"` on the menu container
- `role="option"` on each item
- `aria-selected` on the active item
- Section headers: `role="group"` with `aria-label`

### 9.3 Contrast
- Selection background `bg-primary/15` + text `text-foreground` → ~4.5:1 minimum
- In dark mode: primary color is typically bright (e.g., `#5eead4`), 15% opacity on dark background provides sufficient contrast
- If contrast is insufficient, fallback to `bg-primary/20` or solid `bg-primary` with `text-primary-foreground`

---

## 10. Performance

### 10.1 Memoization Strategy

```typescript
// In FileMentionMenu component

const groupedWorkspace = useMemo(
  () => groupByDirectory(sections.workspace),
  [sections.workspace]
);

const allItems = useMemo(
  () => [
    ...sections.attachments,
    ...groupedWorkspace.flatMap(g => g.items)
  ],
  [sections.attachments, groupedWorkspace]
);

const activeItem = useMemo(
  () => allItems.find(item => item.order === activeIndex),
  [allItems, activeIndex]
);
```

### 10.2 Render Optimization
- Each `MenuItem` is a memoized component (`React.memo`)
- `highlightMatches` is memoized per item per query
- Directory groups are stable references (no re-allocation on re-render)

### 10.3 Scroll Optimization
- `scrollIntoView` with `behavior: 'smooth'` is throttled (only on activeIndex change)
- No virtual scrolling needed for 500 items (native browser scroll is sufficient)

---

## 11. File Changes

| File | Change | Description |
|------|--------|-------------|
| `web/src/features/chat/file-mention-menu.tsx` | **Major rewrite** | New component structure: MenuItem, DirectoryGroup, SectionHeader, MenuFooter |
| `web/src/features/chat/useFileMentions.ts` | **No changes** | Hook contract preserved |
| `web/src/features/chat/components/chat-prompt-composer.tsx` | **No changes** | Consumer unchanged |

---

## 12. Testing Strategy

### 12.1 Visual Testing
- [ ] Selection state visible in both light and dark themes
- [ ] Two-tier layout renders correctly with long filenames and paths
- [ ] Directory grouping shows correct headers with file counts
- [ ] Match highlighting renders correctly for overlapping matches
- [ ] Footer keyboard hints are visible and styled correctly

### 12.2 Interaction Testing
- [ ] Arrow Up/Down cycles through all items across sections and groups
- [ ] Enter selects the active item
- [ ] Escape closes the menu
- [ ] Mouse hover updates active index
- [ ] Mouse click selects the hovered item
- [ ] ScrollIntoView keeps active item visible

### 12.3 Edge Cases
- [ ] Empty workspace (no files) shows appropriate empty state
- [ ] Empty attachments section is hidden or shows "No pending uploads"
- [ ] Very long filenames (>50 chars) are truncated gracefully
- [ ] Very deep paths (>100 chars) use smart truncation
- [ ] Files with same name in different directories are distinguishable
- [ ] 500+ items render without noticeable lag
- [ ] Query with no matches shows "No results" state

---

## 13. Migration Plan

1. **Backup** current `file-mention-menu.tsx` (git commit)
2. **Rewrite** component with new structure
3. **Test** in chat-prompt-composer context
4. **Verify** all keyboard interactions work
5. **Commit** with message: `feat(chat): redesign FileMentionMenu with directory grouping and two-tier layout`

---

## 14. Open Questions

1. Should directory groups be sorted alphabetically or by file count (most files first)?
2. Should the `(root)` group be labeled differently, e.g., "Project root"?
3. Should we show file extension icons instead of generic file icon?
4. Should the footer be sticky (always visible) or scroll with content?
5. Is `bg-primary/15` sufficient contrast, or should we use `bg-primary/20`?

---

## 15. Next Step

After design approval, proceed to task writing and implementation.
