"use client";

import { cn } from "@/lib/utils";
import type {
  SubagentRunStatus,
  SubagentRunSummary,
  SubagentStep,
} from "@/hooks/types";
import type { ComponentProps } from "react";
import { memo, useMemo, useState } from "react";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Shimmer } from "./shimmer";
import {
  AlertCircleIcon,
  BanIcon,
  CheckIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  ClockIcon,
  CoinsIcon,
  Loader2Icon,
  PauseIcon,
  PlayIcon,
  XIcon,
} from "lucide-react";

// ---------------------------------------------------------------------------
// Status visual language
// ---------------------------------------------------------------------------
//
// Each SubAgent run has a single terminal status that drives colour, icon
// and animation. The mapping is intentionally narrow so that the muscle
// memory (blue=in flight, green=done, red=failed, amber=waiting, grey=stopped)
// carries across surfaces.

export type SubagentStatusStyle = {
  dotClass: string;
  textClass: string;
  icon: ComponentProps<"span">["children"];
  label: (agentLabel: string) => string;
  isAnimated: boolean;
};

const STATUS_STYLES: Record<SubagentRunStatus, SubagentStatusStyle> = {
  running: {
    dotClass: "bg-blue-500 animate-pulse",
    textClass: "text-blue-600 dark:text-blue-400",
    icon: <Loader2Icon className="size-3 animate-spin" />,
    label: (a) => `${a} working`,
    isAnimated: true,
  },
  blocked: {
    dotClass: "bg-amber-500",
    textClass: "text-amber-600 dark:text-amber-400",
    icon: <PauseIcon className="size-3" />,
    label: (a) => `${a} blocked`,
    isAnimated: false,
  },
  success: {
    dotClass: "bg-emerald-500",
    textClass: "text-emerald-600 dark:text-emerald-400",
    icon: <CheckIcon className="size-3" />,
    label: (a) => `${a} completed`,
    isAnimated: false,
  },
  error: {
    dotClass: "bg-red-500",
    textClass: "text-red-600 dark:text-red-400",
    icon: <XIcon className="size-3" />,
    label: (a) => `${a} failed`,
    isAnimated: false,
  },
  cancelled: {
    dotClass: "bg-zinc-400",
    textClass: "text-zinc-500 dark:text-zinc-400",
    icon: <BanIcon className="size-3" />,
    label: (a) => `${a} cancelled`,
    isAnimated: false,
  },
};

/** Resolves a status from the live flags we already track. */
function resolveStatus(args: {
  isRunning?: boolean;
  summary?: SubagentRunSummary;
}): SubagentRunStatus {
  if (args.summary) {
    return args.summary.status;
  }
  return args.isRunning ? "running" : "success";
}

// ---------------------------------------------------------------------------
// Progress formatting helpers
// ---------------------------------------------------------------------------

function formatDuration(ms: number): string {
  if (ms <= 0) return "0s";
  const totalSeconds = Math.round(ms / 1000);
  if (totalSeconds < 60) return `${totalSeconds}s`;
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes < 60) {
    return seconds === 0 ? `${minutes}m` : `${minutes}m ${seconds}s`;
  }
  const hours = Math.floor(minutes / 60);
  const remainingMinutes = minutes % 60;
  return remainingMinutes === 0
    ? `${hours}h`
    : `${hours}h ${remainingMinutes}m`;
}

function formatTokenCount(n: number): string {
  if (n < 1000) return String(n);
  if (n < 1_000_000) {
    const k = n / 1000;
    return `${k >= 10 ? k.toFixed(0) : k.toFixed(1)}K`;
  }
  return `${(n / 1_000_000).toFixed(1)}M`;
}

// ---------------------------------------------------------------------------
// SubagentActivity — top-level wrapper rendered inside Tool's ToolContent area
// ---------------------------------------------------------------------------

export type SubagentActivityProps = ComponentProps<"div"> & {
  steps: SubagentStep[];
  isRunning?: boolean;
  defaultOpen?: boolean;
  /** Built-in subagent type (coder / explore / plan) */
  subagentType?: string;
  /** Terminal run summary, populated from a SubagentCompleted event. */
  subagentRunSummary?: SubagentRunSummary;
  /**
   * Sub-agent instance ID, used to address CancelSubagent. The parent
   * session ID comes from LiveMessage.sessionID upstream; we don't need
   * it here because the cancel handler is wired at the parent tool-call
   * level.
   */
  subagentAgentId?: string;
  /**
   * Optional callback invoked when the user clicks the cancel button. The
   * host component is responsible for issuing the actual HTTP request
   * to the backend and for refreshing the live subagentRunSummary on
   * success. When unset, the cancel button is hidden.
   */
  onCancelSubagent?: (subagentAgentId: string) => void;
  /**
   * Optional callback invoked when the user confirms retrying a failed
   * sub-agent. The host is responsible for dispatching the retry; this
   * component only renders the trigger button. The button is hidden
   * when no terminal error is recorded or when the host does not
   * provide a handler.
   */
  onRetrySubagent?: (subagentAgentId: string) => void;
};

export const SubagentActivity = memo(
  ({
    className,
    steps,
    isRunning = false,
    defaultOpen = false,
    subagentType,
    subagentRunSummary,
    subagentAgentId,
    onCancelSubagent,
    onRetrySubagent,
    ...props
  }: SubagentActivityProps) => {
    const agentLabel = subagentType
      ? `${subagentType.charAt(0).toUpperCase() + subagentType.slice(1)} agent`
      : "Agent";
    const [isOpen, setIsOpen] = useState(defaultOpen);
    const [confirmCancel, setConfirmCancel] = useState(false);
    // detailOpen controls an inline detail panel that surfaces the
    // full token breakdown, raw tool-call input/output and the terminal
    // error message without leaving the conversation context. It is
    // independent of the main Collapsible body which always shows the
    // compressed step list.
    const [detailOpen, setDetailOpen] = useState(false);
    const [confirmRetry, setConfirmRetry] = useState(false);

    const status = resolveStatus({
      isRunning,
      summary: subagentRunSummary,
    });
    const style = STATUS_STYLES[status];

    const toolCallCount = steps.filter((s) => s.kind === "tool-call").length;
    const completedToolCalls = steps.filter(
      (s) => s.kind === "tool-call" && s.status !== "running",
    ).length;
    const hasError =
      subagentRunSummary?.status === "error" ||
      steps.some((s) => s.kind === "tool-call" && s.status === "error");

    // The cancel button is only actionable when the sub-agent is still
    // running AND we have a subagentID AND we have a cancel handler.
    const canCancel =
      status === "running" && !subagentRunSummary && !!subagentAgentId && !!onCancelSubagent;

    const handleCancelClick = (e: React.MouseEvent) => {
      // Stop the click from bubbling up to the Collapsible trigger.
      e.stopPropagation();
      if (!subagentAgentId || !onCancelSubagent) return;
      if (!confirmCancel) {
        setConfirmCancel(true);
        return;
      }
      onCancelSubagent(subagentAgentId);
      setConfirmCancel(false);
    };

    // The retry button is only actionable when the sub-agent has a
    // terminal error AND we have a subagentID AND we have a retry
    // handler. The handler is hidden otherwise to avoid offering
    // unsupported affordances.
    const canRetry =
      status === "error" && !!subagentAgentId && !!onRetrySubagent;

    const handleRetryClick = (e: React.MouseEvent) => {
      e.stopPropagation();
      if (!subagentAgentId || !onRetrySubagent) return;
      if (!confirmRetry) {
        setConfirmRetry(true);
        return;
      }
      onRetrySubagent(subagentAgentId);
      setConfirmRetry(false);
    };

    // Compose the secondary line: "step 3/8 · 2,341 tok · 12s" or
    // "4 tool calls · 12s" when no run summary is present.
    const metaLine = useMemo(() => {
      const parts: string[] = [];
      if (status === "running" && toolCallCount > 0) {
        parts.push(
          `step ${Math.min(completedToolCalls + 1, toolCallCount)}/${toolCallCount}`,
        );
      } else if (toolCallCount > 0) {
        parts.push(
          `${toolCallCount} tool call${toolCallCount !== 1 ? "s" : ""}`,
        );
      }
      if (subagentRunSummary) {
        if (subagentRunSummary.durationMs > 0) {
          parts.push(formatDuration(subagentRunSummary.durationMs));
        }
        const total = subagentRunSummary.usage.total;
        if (total > 0) {
          parts.push(`${formatTokenCount(total)} tok`);
        }
      }
      return parts.join(" · ");
    }, [status, toolCallCount, completedToolCalls, subagentRunSummary]);

    return (
      <Collapsible
        className={cn("mt-2", className)}
        open={isOpen}
        onOpenChange={setIsOpen}
        {...props}
      >
        <CollapsibleTrigger className="flex items-center gap-1.5 text-xs text-muted-foreground group cursor-pointer">
          <span
            aria-hidden
            className={cn(
              "size-1.5 rounded-full shrink-0",
              status === "error" || hasError
                ? "bg-destructive"
                : style.dotClass,
            )}
          />
          <span className="inline-flex items-center gap-1">
            {style.icon}
            <span>
              {status === "running" ? (
                <>
                  {style.label(agentLabel)}
                  <Shimmer
                    as="span"
                    duration={1}
                    className="text-muted-foreground ml-0.5"
                  >
                    ...
                  </Shimmer>
                </>
              ) : (
                style.label(agentLabel)
              )}
            </span>
          </span>
          {metaLine && (
            <span
              className={cn(
                "text-muted-foreground/80 tabular-nums",
                style.textClass,
              )}
            >
              · {metaLine}
            </span>
          )}
          {subagentRunSummary?.error && (
            <span
              className="text-destructive inline-flex items-center gap-0.5"
              title={subagentRunSummary.error}
            >
              <AlertCircleIcon className="size-3" />
            </span>
          )}
          {canCancel && (
            <button
              type="button"
              onClick={handleCancelClick}
              aria-label={confirmCancel ? "Confirm stop sub-agent" : "Stop sub-agent"}
              title={
                confirmCancel
                  ? "Click again to confirm"
                  : "Stop this sub-agent. Main task will continue."
              }
              className={cn(
                "ml-1 inline-flex items-center gap-1 rounded px-1.5 py-0.5",
                "text-muted-foreground hover:text-foreground",
                "opacity-0 group-hover:opacity-100 focus:opacity-100",
                "transition-opacity",
                confirmCancel &&
                  "opacity-100 bg-destructive/10 text-destructive hover:bg-destructive/20",
              )}
            >
              <XIcon className="size-3" />
              <span>{confirmCancel ? "Confirm stop" : "Stop"}</span>
            </button>
          )}
          {confirmCancel && (
            <span className="text-[10px] text-muted-foreground/70 italic">
              click again to confirm
            </span>
          )}
          {canRetry && (
            <button
              type="button"
              onClick={handleRetryClick}
              aria-label={confirmRetry ? "Confirm retry sub-agent" : "Retry sub-agent"}
              title={
                confirmRetry
                  ? "Click again to confirm"
                  : "Re-dispatch this sub-agent with the same prompt."
              }
              className={cn(
                "ml-1 inline-flex items-center gap-1 rounded px-1.5 py-0.5",
                "text-muted-foreground hover:text-foreground",
                "opacity-0 group-hover:opacity-100 focus:opacity-100",
                "transition-opacity",
                confirmRetry &&
                  "opacity-100 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 hover:bg-emerald-500/20",
              )}
            >
              <PlayIcon className="size-3" />
              <span>{confirmRetry ? "Confirm retry" : "Retry"}</span>
            </button>
          )}
          {/* The detail panel button is only useful when there is at
              least one terminal piece of information to show: a summary
              (duration/usage/error) or any tool-call output. While the
              sub-agent is still streaming without any of that, the
              inline Collapsible body is enough. */}
          {(subagentRunSummary ||
            steps.some((s) => s.kind === "tool-call")) && (
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                setDetailOpen((v) => !v);
              }}
              aria-label={detailOpen ? "Hide sub-agent details" : "Show sub-agent details"}
              aria-expanded={detailOpen}
              className={cn(
                "ml-0.5 inline-flex items-center gap-1 rounded px-1.5 py-0.5",
                "text-muted-foreground hover:text-foreground",
                "opacity-0 group-hover:opacity-100 focus:opacity-100",
                "transition-opacity",
                detailOpen && "opacity-100",
              )}
            >
              {detailOpen ? (
                <ChevronDownIcon className="size-3" />
              ) : (
                <ChevronRightIcon className="size-3" />
              )}
              <span>{detailOpen ? "Hide details" : "Details"}</span>
            </button>
          )}
          <ChevronRightIcon
            className={cn(
              "size-3 text-muted-foreground transition-transform duration-200",
              isOpen && "rotate-90",
            )}
          />
        </CollapsibleTrigger>

        <CollapsibleContent
          className={cn(
            "mt-1.5 space-y-0.5 border-l-2 border-border pl-3",
            "data-[state=closed]:fade-out-0 data-[state=open]:slide-in-from-top-1 outline-none data-[state=closed]:animate-out data-[state=open]:animate-in",
          )}
        >
          {subagentRunSummary?.error && (
            <pre className="text-xs text-destructive whitespace-pre-wrap max-h-24 overflow-y-auto">
              {subagentRunSummary.error}
            </pre>
          )}
          {steps.map((step, index) => (
            <SubagentStepItem key={`sa-step-${index}`} step={step} />
          ))}
          {subagentRunSummary && (
            <SubagentRunFooter summary={subagentRunSummary} />
          )}
          {detailOpen && (
            <SubagentDetailPanel
              steps={steps}
              summary={subagentRunSummary}
            />
          )}
        </CollapsibleContent>
      </Collapsible>
    );
  },
);

SubagentActivity.displayName = "SubagentActivity";

// ---------------------------------------------------------------------------
// SubagentRunFooter — terminal-state chips (duration / token / error)
// ---------------------------------------------------------------------------

const SubagentRunFooter = ({ summary }: { summary: SubagentRunSummary }) => {
  const tokens = summary.usage.total;
  const duration = summary.durationMs;
  if (tokens === 0 && duration === 0) {
    return null;
  }
  return (
    <div className="mt-1.5 flex items-center gap-3 text-[10px] text-muted-foreground/80 tabular-nums">
      {duration > 0 && (
        <span className="inline-flex items-center gap-0.5">
          <ClockIcon className="size-2.5" />
          {formatDuration(duration)}
        </span>
      )}
      {tokens > 0 && (
        <span
          className="inline-flex items-center gap-0.5"
          title={`in ${summary.usage.input} · out ${summary.usage.output} · cache read ${summary.usage.cache_read} · cache creation ${summary.usage.cache_creation}`}
        >
          <CoinsIcon className="size-2.5" />
          {formatTokenCount(tokens)} tokens
        </span>
      )}
    </div>
  );
};

// ---------------------------------------------------------------------------
// SubagentDetailPanel — inline detail surface that shows the full token
// breakdown, every tool-call input/output and the terminal error. Lives
// in-place so the user never has to navigate away from the conversation
// context (per design.md §Phase 2 详情抽屉).
// ---------------------------------------------------------------------------

function formatJson(value: unknown): string {
  if (value === undefined) return "(no input)";
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

const SubagentDetailPanel = ({
  steps,
  summary,
}: {
  steps: SubagentStep[];
  summary?: SubagentRunSummary;
}) => {
  const toolCalls = steps.filter(
    (s): s is Extract<SubagentStep, { kind: "tool-call" }> =>
      s.kind === "tool-call",
  );
  const hasToolCalls = toolCalls.length > 0;
  const hasSummary = summary !== undefined;

  if (!hasToolCalls && !hasSummary) {
    return null;
  }

  return (
    <div className="mt-1.5 rounded border border-border/60 bg-muted/20 p-2 text-xs space-y-2">
      {hasSummary && (
        <div className="space-y-1">
          <div className="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
            Run summary
          </div>
          <dl className="grid grid-cols-[max-content_1fr] gap-x-3 gap-y-0.5 tabular-nums">
            <dt className="text-muted-foreground">status</dt>
            <dd className="font-medium">{summary!.status}</dd>
            <dt className="text-muted-foreground">duration</dt>
            <dd>{formatDuration(summary!.durationMs)}</dd>
            <dt className="text-muted-foreground">total tokens</dt>
            <dd>{formatTokenCount(summary!.usage.total)}</dd>
            <dt className="text-muted-foreground">in / out</dt>
            <dd>
              {formatTokenCount(summary!.usage.input)} in ·{" "}
              {formatTokenCount(summary!.usage.output)} out
            </dd>
            <dt className="text-muted-foreground">cache read</dt>
            <dd>{formatTokenCount(summary!.usage.cache_read)}</dd>
            <dt className="text-muted-foreground">cache creation</dt>
            <dd>{formatTokenCount(summary!.usage.cache_creation)}</dd>
            {summary!.summary && (
              <>
                <dt className="text-muted-foreground">summary</dt>
                <dd className="whitespace-pre-wrap break-words">
                  {summary!.summary}
                </dd>
              </>
            )}
            {summary!.error && (
              <>
                <dt className="text-muted-foreground text-destructive">error</dt>
                <dd className="whitespace-pre-wrap break-words text-destructive">
                  {summary!.error}
                </dd>
              </>
            )}
          </dl>
        </div>
      )}
      {hasToolCalls && (
        <div className="space-y-1">
          <div className="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
            Tool calls ({toolCalls.length})
          </div>
          <ol className="space-y-1.5 pl-0 list-none">
            {toolCalls.map((tc, idx) => (
              <li
                key={tc.toolCallId || `tc-${idx}`}
                className="rounded border border-border/40 bg-background/40 p-1.5"
              >
                <div className="flex items-center gap-1 text-foreground/80">
                  <span className="text-muted-foreground/60 tabular-nums">
                    {idx + 1}.
                  </span>
                  <span className="font-medium">{tc.toolName}</span>
                  <span className="text-muted-foreground text-[10px] ml-1">
                    · {tc.status}
                  </span>
                </div>
                {tc.input !== undefined && (
                  <pre className="mt-1 max-h-32 overflow-y-auto rounded bg-muted/40 p-1 text-[10px] leading-relaxed text-foreground/70 whitespace-pre-wrap break-words">
                    {formatJson(tc.input)}
                  </pre>
                )}
                {tc.output && (
                  <div className="mt-1">
                    <div className="text-[10px] text-muted-foreground">
                      output
                    </div>
                    <pre className="max-h-40 overflow-y-auto rounded bg-muted/40 p-1 text-[10px] leading-relaxed text-foreground/70 whitespace-pre-wrap break-words">
                      {tc.output}
                    </pre>
                  </div>
                )}
                {tc.errorText && (
                  <div className="mt-1">
                    <div className="text-[10px] text-destructive">error</div>
                    <pre className="max-h-40 overflow-y-auto rounded bg-destructive/10 p-1 text-[10px] leading-relaxed text-destructive whitespace-pre-wrap break-words">
                      {tc.errorText}
                    </pre>
                  </div>
                )}
              </li>
            ))}
          </ol>
        </div>
      )}
    </div>
  );
};

// ---------------------------------------------------------------------------
// SubagentStepItem — renders a single step based on kind
// ---------------------------------------------------------------------------

const SubagentStepItem = ({ step }: { step: SubagentStep }) => {
  switch (step.kind) {
    case "thinking":
      return (
        <div className="text-muted-foreground/60 italic text-xs line-clamp-2">
          {step.text}
        </div>
      );

    case "text":
      return (
        <div className="text-foreground/70 text-xs line-clamp-2">
          {step.text}
        </div>
      );

    case "tool-call":
      return <SubToolCallItem step={step} />;

    default:
      return null;
  }
};

// ---------------------------------------------------------------------------
// SubToolCallItem — expandable sub-tool-call with status + output
// ---------------------------------------------------------------------------

/** Extract a primary parameter value for inline display */
const getPrimaryParam = (input: unknown): string | null => {
  if (!input || typeof input !== "object") return null;
  const keys = ["path", "command", "pattern", "url", "query", "file_path"];
  for (const key of keys) {
    const val = (input as Record<string, unknown>)[key];
    if (typeof val === "string" && val.length > 0) {
      return val.length > 50 ? `${val.slice(0, 50)}…` : val;
    }
  }
  return null;
};

const getSubToolStatusIcon = (status: string) => {
  switch (status) {
    case "success":
      return <CheckIcon className="size-2.5 text-success shrink-0" />;
    case "error":
      return <XIcon className="size-2.5 text-destructive shrink-0" />;
    default:
      return <Loader2Icon className="size-2.5 text-muted-foreground animate-spin shrink-0" />;
  }
};

const SubToolCallItem = ({
  step,
}: {
  step: Extract<SubagentStep, { kind: "tool-call" }>;
}) => {
  const [expanded, setExpanded] = useState(false);
  const primaryParam = getPrimaryParam(step.input);
  const hasExpandableContent = Boolean(step.output || step.errorText);

  return (
    <div>
      <div
        className={cn(
          "flex items-center gap-1 text-xs",
          hasExpandableContent && "cursor-pointer",
        )}
        onClick={() => hasExpandableContent && setExpanded(!expanded)}
        onKeyDown={(e) => {
          if (hasExpandableContent && (e.key === "Enter" || e.key === " ")) {
            e.preventDefault();
            setExpanded(!expanded);
          }
        }}
        role={hasExpandableContent ? "button" : undefined}
        tabIndex={hasExpandableContent ? 0 : undefined}
      >
        {getSubToolStatusIcon(step.status)}
        <span className="text-primary/80 font-medium">{step.toolName}</span>
        {primaryParam && !expanded && (
          <span className="text-muted-foreground truncate">
            ({primaryParam})
          </span>
        )}
        {hasExpandableContent && (
          <ChevronRightIcon
            className={cn(
              "size-2.5 text-muted-foreground/50 transition-transform duration-200",
              expanded && "rotate-90",
            )}
          />
        )}
      </div>
      {expanded && (
        <div className="ml-4 mt-0.5">
          {step.errorText && (
            <pre className="text-xs text-destructive whitespace-pre-wrap max-h-24 overflow-y-auto">
              {step.errorText}
            </pre>
          )}
          {step.output && !step.errorText && (
            <pre className="text-xs text-foreground/60 whitespace-pre-wrap max-h-24 overflow-y-auto">
              {step.output.length > 500
                ? `${step.output.slice(0, 500)}…`
                : step.output}
            </pre>
          )}
        </div>
      )}
    </div>
  );
};

// Re-exported helpers for tests.
export const __test = {
  formatDuration,
  formatTokenCount,
  resolveStatus,
  STATUS_STYLES,
};
