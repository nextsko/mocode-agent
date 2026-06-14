import { useEffect, useMemo, useRef, memo } from "react";
import { cn } from "@/lib/utils";
import {
  AlertCircleIcon,
  FileTextIcon,
  FolderIcon,
  PaperclipIcon,
  RefreshCwIcon,
} from "lucide-react";
import type { ReactNode } from "react";
import type { MentionOption, MentionSections } from "./useFileMentions";

// ─── Utilities ───────────────────────────────────────────────────────────────

const formatFileSize = (size?: number): string | null => {
  if (size === null || size === undefined) {
    return null;
  }
  if (size === 0) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = size;
  let idx = 0;
  while (value >= 1024 && idx < units.length - 1) {
    value /= 1024;
    idx += 1;
  }
  const precision = value >= 10 ? 0 : 1;
  return `${value.toFixed(precision)} ${units[idx]}`;
};

const MAX_WORKSPACE_FILES = 500;

const SectionLabel: Record<MentionOption["type"], string> = {
  attachment: "Pending uploads",
  workspace: "Workspace files",
};

const TypeBadge: Record<MentionOption["type"], string> = {
  attachment: "Upload",
  workspace: "Workspace",
};

const TypeIcon = {
  attachment: PaperclipIcon,
  workspace: FileTextIcon,
};

// ─── Match Highlighting ──────────────────────────────────────────────────────

function highlightMatches(text: string, query: string): ReactNode {
  if (!query) return text;

  const lowerText = text.toLowerCase();
  const lowerQuery = query.toLowerCase();
  const parts: ReactNode[] = [];
  let lastIndex = 0;

  let index = lowerText.indexOf(lowerQuery, lastIndex);
  while (index !== -1) {
    if (index > lastIndex) {
      parts.push(text.substring(lastIndex, index));
    }
    parts.push(
      <span
        key={index}
        className="bg-primary/20 text-primary font-semibold rounded-sm px-0.5"
      >
        {text.substring(index, index + query.length)}
      </span>,
    );
    lastIndex = index + query.length;
    index = lowerText.indexOf(lowerQuery, lastIndex);
  }

  if (lastIndex < text.length) {
    parts.push(text.substring(lastIndex));
  }

  return <>{parts}</>;
}

// ─── Path Utilities ──────────────────────────────────────────────────────────

function getParentDirectory(path: string): string {
  const lastSlash = path.lastIndexOf("/");
  if (lastSlash <= 0) return "";
  return path.substring(0, lastSlash + 1);
}

function getFilename(path: string): string {
  const lastSlash = path.lastIndexOf("/");
  if (lastSlash === -1) return path;
  return path.substring(lastSlash + 1);
}

function truncateDirectoryPath(dirPath: string, maxLength: number = 40): string {
  if (dirPath.length <= maxLength) return dirPath;

  const parts = dirPath.split("/").filter(Boolean);
  if (parts.length <= 2) return dirPath;

  const root = parts[0];
  const parent = parts[parts.length - 2];
  const middle = parts.slice(1, -2);

  const truncated = `${root}/.../${parent}/`;
  if (truncated.length > maxLength) {
    return `.../${parent}/`;
  }
  return truncated;
}

// ─── Directory Grouping ──────────────────────────────────────────────────────

interface DirectoryGroup {
  directory: string;
  items: MentionOption[];
}

function groupByDirectory(options: MentionOption[]): DirectoryGroup[] {
  const groups = new Map<string, MentionOption[]>();

  for (const option of options) {
    const path = option.description || option.label;
    const lastSlash = path.lastIndexOf("/");
    const directory = lastSlash > 0 ? path.substring(0, lastSlash + 1) : "(root)";

    if (!groups.has(directory)) {
      groups.set(directory, []);
    }
    groups.get(directory)!.push(option);
  }

  const entries = Array.from(groups.entries());
  entries.sort(([a], [b]) => {
    if (a === "(root)") return -1;
    if (b === "(root)") return 1;
    return a.localeCompare(b);
  });

  return entries.map(([directory, items]) => ({ directory, items }));
}

// ─── Sub-Components ──────────────────────────────────────────────────────────

interface MenuItemProps {
  option: MentionOption;
  isActive: boolean;
  query: string;
  itemRef?: React.Ref<HTMLButtonElement>;
  onHover: () => void;
  onSelect: (option: MentionOption) => void;
}

const MenuItem = memo(function MenuItem({
  option,
  isActive,
  query,
  itemRef,
  onHover,
  onSelect,
}: MenuItemProps) {
  const Icon = TypeIcon[option.type];
  const sizeLabel = formatFileSize(option.meta?.size);
  const path = option.description || option.label;
  const filename = getFilename(path);
  const parentDir = getParentDirectory(path);

  const highlightedFilename = useMemo(
    () => highlightMatches(filename, query),
    [filename, query],
  );

  const highlightedPath = useMemo(
    () => (parentDir ? highlightMatches(truncateDirectoryPath(parentDir), query) : null),
    [parentDir, query],
  );

  return (
    <button
      ref={itemRef}
      type="button"
      role="option"
      aria-selected={isActive}
      tabIndex={-1}
      className={cn(
        "flex flex-col w-full text-left transition-colors duration-150",
        "px-3 py-2 gap-0.5",
        isActive
          ? "bg-primary/15 text-foreground border-l-2 border-primary"
          : "hover:bg-muted/60 border-l-2 border-transparent",
      )}
      onMouseDown={(event) => {
        event.preventDefault();
        onSelect(option);
      }}
      onMouseEnter={onHover}
    >
      {/* Primary Row: Icon + Filename + Size + Badge */}
      <div className="flex items-center gap-2 min-w-0">
        <Icon
          className={cn(
            "size-4 shrink-0",
            isActive ? "text-primary" : "text-muted-foreground",
          )}
        />
        <span className="font-medium text-sm truncate min-w-0 flex-1">
          {highlightedFilename}
        </span>
        <div className="flex shrink-0 items-center gap-1.5 text-[10px]">
          {sizeLabel ? (
            <span className={cn(
              "tabular-nums",
              isActive ? "text-primary/80" : "text-muted-foreground",
            )}>
              {sizeLabel}
            </span>
          ) : null}
          <span className={cn(
            "rounded border px-1 py-px font-medium uppercase",
            isActive
              ? "border-primary/40 text-primary/80"
              : "border-border/60 text-muted-foreground",
          )}>
            {TypeBadge[option.type]}
          </span>
        </div>
      </div>

      {/* Secondary Row: Parent Directory Path */}
      {parentDir && (
        <div className="flex items-center pl-6 text-xs min-w-0">
          <span className={cn(
            "truncate",
            isActive ? "text-primary/80" : "text-muted-foreground",
          )}>
            {highlightedPath}
          </span>
        </div>
      )}
    </button>
  );
});

interface SectionHeaderProps {
  label: string;
  count: number;
}

function SectionHeader({ label, count }: SectionHeaderProps) {
  return (
    <div className="px-3 py-1.5 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
      {label} ({count})
    </div>
  );
}

interface DirectoryGroupHeaderProps {
  directory: string;
  count: number;
}

function DirectoryGroupHeader({ directory, count }: DirectoryGroupHeaderProps) {
  return (
    <div
      role="group"
      aria-label={`${directory} directory, ${count} files`}
      className="flex items-center gap-1.5 px-3 py-1 text-[10px] text-muted-foreground/70 bg-muted/30"
    >
      <FolderIcon className="size-3 shrink-0" />
      <span className="truncate">{directory}</span>
      <span className="ml-auto tabular-nums">({count})</span>
    </div>
  );
}

interface MenuFooterProps {
  totalItems: number;
}

function MenuFooter({ totalItems }: MenuFooterProps) {
  return (
    <div className="flex items-center justify-between px-3 py-2 text-[10px] text-muted-foreground/60 border-t border-border/50">
      <span className="tabular-nums">{totalItems} item{totalItems === 1 ? "" : "s"}</span>
      <div className="flex items-center gap-3">
        <span className="flex items-center gap-1">
          <kbd className="px-1 py-0.5 rounded bg-muted font-mono text-[9px]">↑↓</kbd>
          <span>navigate</span>
        </span>
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
  );
}

// ─── Main Component ──────────────────────────────────────────────────────────

export interface FileMentionMenuProps {
  open: boolean;
  query: string;
  sections: MentionSections;
  flatOptions: MentionOption[];
  activeIndex: number;
  onSelect: (option: MentionOption) => void;
  onHover: (index: number) => void;
  workspaceStatus: "idle" | "loading" | "ready" | "error";
  workspaceError: string | null;
  onRetryWorkspace: () => void;
  isWorkspaceAvailable: boolean;
  workspaceFileCount?: number;
}

export const FileMentionMenu = ({
  open,
  query,
  sections,
  flatOptions,
  activeIndex,
  onSelect,
  onHover,
  workspaceStatus,
  workspaceError,
  onRetryWorkspace,
  isWorkspaceAvailable,
  workspaceFileCount = 0,
}: FileMentionMenuProps) => {
  const activeItemRef = useRef<HTMLButtonElement>(null);

  // Scroll active item into view when activeIndex changes.
  useEffect(() => {
    if (open && activeItemRef.current) {
      activeItemRef.current.scrollIntoView({
        block: "nearest",
        behavior: "smooth",
      });
    }
  }, [open, activeIndex]);

  // Group workspace files by directory
  const workspaceGroups = useMemo(
    () => groupByDirectory(sections.workspace),
    [sections.workspace],
  );

  // Build a flat list of all visible items for activeIndex mapping
  const allVisibleItems = useMemo(() => {
    const items: MentionOption[] = [];
    items.push(...sections.attachments);
    for (const group of workspaceGroups) {
      items.push(...group.items);
    }
    return items;
  }, [sections.attachments, workspaceGroups]);

  // Find the active item for ref assignment
  const activeItem = useMemo(
    () => allVisibleItems.find((item) => item.order === activeIndex),
    [allVisibleItems, activeIndex],
  );

  if (!open) {
    return null;
  }

  const hasSections =
    sections.attachments.length > 0 || sections.workspace.length > 0;
  const showStatus = workspaceStatus !== "idle" || !isWorkspaceAvailable;
  const totalItems = sections.attachments.length + sections.workspace.length;

  return (
    <div className="absolute left-0 right-0 bottom-[calc(100%+0.75rem)] z-30">
      <div className="rounded-xl border border-border/80 bg-popover/95 shadow-xl backdrop-blur supports-backdrop-filter:bg-popover/80">
        <div
          role="listbox"
          aria-activedescendant={activeItem ? `mention-item-${activeItem.id}` : undefined}
          className="max-h-96 overflow-y-auto [-webkit-overflow-scrolling:touch]"
        >
          {hasSections ? (
            <>
              {/* Attachments Section */}
              {sections.attachments.length > 0 && (
                <div role="group" aria-label="Pending uploads">
                  <SectionHeader
                    label={SectionLabel.attachment}
                    count={sections.attachments.length}
                  />
                  {sections.attachments.map((option) => (
                    <MenuItem
                      key={option.id}
                      option={option}
                      isActive={option.order === activeIndex}
                      query={query}
                      itemRef={option.order === activeIndex ? activeItemRef : undefined}
                      onHover={() => onHover(option.order)}
                      onSelect={onSelect}
                    />
                  ))}
                </div>
              )}

              {/* Workspace Section with Directory Grouping */}
              {workspaceGroups.length > 0 && (
                <div role="group" aria-label="Workspace files">
                  <SectionHeader
                    label={SectionLabel.workspace}
                    count={sections.workspace.length}
                  />
                  {workspaceGroups.map((group) => (
                    <div key={group.directory}>
                      <DirectoryGroupHeader
                        directory={group.directory}
                        count={group.items.length}
                      />
                      {group.items.map((option) => (
                        <MenuItem
                          key={option.id}
                          option={option}
                          isActive={option.order === activeIndex}
                          query={query}
                          itemRef={option.order === activeIndex ? activeItemRef : undefined}
                          onHover={() => onHover(option.order)}
                          onSelect={onSelect}
                        />
                      ))}
                    </div>
                  ))}
                </div>
              )}
            </>
          ) : (
            <div className="px-3 py-2 text-sm text-muted-foreground">
              {query
                ? `No files match "@${query}".`
                : "No files available to mention yet."}
            </div>
          )}
        </div>

        {/* Keyboard Hints Footer */}
        {hasSections && <MenuFooter totalItems={totalItems} />}

        {/* Status Bar */}
        {showStatus ? (
          <div className="rounded-md border border-border/80 bg-muted/40 px-3 py-2 text-xs text-muted-foreground">
            {workspaceStatus === "loading" ? (
              <div className="flex items-center gap-2">
                <RefreshCwIcon className="size-3.5 animate-spin text-primary" />
                Indexing workspace files…
              </div>
            ) : workspaceStatus === "error" ? (
              <div className="flex flex-wrap items-center gap-2">
                <span className="inline-flex items-center gap-1 text-destructive">
                  <AlertCircleIcon className="size-3.5" />
                  {workspaceError ?? "Workspace files unavailable."}
                </span>
                <button
                  type="button"
                  className="text-xs font-semibold text-primary underline underline-offset-2"
                  onClick={onRetryWorkspace}
                >
                  Retry
                </button>
              </div>
            ) : isWorkspaceAvailable ? (
              <div className="flex items-center justify-between text-xs">
                <span>
                  {flatOptions.length
                    ? `${flatOptions.length} file${
                        flatOptions.length === 1 ? "" : "s"
                      } ready to mention.`
                    : "Workspace files indexed."}
                  {workspaceFileCount >= MAX_WORKSPACE_FILES
                    ? " Type a path to search deeper."
                    : ""}
                </span>
              </div>
            ) : (
              <span>
                Select an active session to enable workspace file mentions.
              </span>
            )}
          </div>
        ) : null}
      </div>
    </div>
  );
};
