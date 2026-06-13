import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import {
  SubagentActivity,
  __test as helpers,
} from "@/components/ai-elements/subagent-steps";
import type { SubagentRunSummary } from "@/hooks/types";

function baseProps(overrides: Partial<Parameters<typeof SubagentActivity>[0]> = {}) {
  return {
    steps: [
      { kind: "thinking", text: "thinking…" },
      {
        kind: "tool-call",
        toolCallId: "t1",
        toolName: "bash",
        status: "running" as const,
      },
    ],
    isRunning: true,
    subagentType: "coder",
    ...overrides,
  };
}

describe("SubagentActivity cancel button", () => {
  it("hides the stop button when no onCancelSubagent is provided", () => {
    render(<SubagentActivity {...baseProps()} />);
    expect(screen.queryByRole("button", { name: /stop sub-agent/i })).toBeNull();
  });

  it("hides the stop button when the sub-agent has a terminal summary", () => {
    const summary: SubagentRunSummary = {
      status: "success",
      durationMs: 1234,
      usage: { input: 1, output: 1, cache_read: 0, cache_creation: 0, total: 2 },
    };
    const onCancel = vi.fn();
    render(
      <SubagentActivity
        {...baseProps({
          isRunning: false,
          subagentRunSummary: summary,
          subagentAgentId: "agent-1",
          onCancelSubagent: onCancel,
        })}
      />,
    );
    expect(screen.queryByRole("button", { name: /stop sub-agent/i })).toBeNull();
  });

  it("requires two clicks (confirm) before invoking the cancel handler", () => {
    const onCancel = vi.fn();
    render(
      <SubagentActivity
        {...baseProps({
          subagentAgentId: "agent-7",
          onCancelSubagent: onCancel,
        })}
      />,
    );

    const stopBtn = screen.getByRole("button", { name: /stop sub-agent/i });
    fireEvent.click(stopBtn);
    expect(onCancel).not.toHaveBeenCalled();

    // Second click flips to "Confirm stop" and dispatches the cancel.
    const confirmBtn = screen.getByRole("button", { name: /confirm stop sub-agent/i });
    fireEvent.click(confirmBtn);
    expect(onCancel).toHaveBeenCalledTimes(1);
    expect(onCancel).toHaveBeenCalledWith("agent-7");
  });
});

describe("subagent status helpers", () => {
  it("formatDuration rounds to nearest second under 60s", () => {
    expect(helpers.formatDuration(0)).toBe("0s");
    expect(helpers.formatDuration(500)).toBe("1s");
    expect(helpers.formatDuration(59_400)).toBe("59s");
  });

  it("formatDuration breaks into minutes and hours beyond 60s", () => {
    expect(helpers.formatDuration(60_000)).toBe("1m");
    expect(helpers.formatDuration(125_000)).toBe("2m 5s");
    expect(helpers.formatDuration(3_600_000)).toBe("1h");
    expect(helpers.formatDuration(3_900_000)).toBe("1h 5m");
  });

  it("formatTokenCount picks the right magnitude", () => {
    expect(helpers.formatTokenCount(0)).toBe("0");
    expect(helpers.formatTokenCount(999)).toBe("999");
    expect(helpers.formatTokenCount(1500)).toBe("1.5K");
    expect(helpers.formatTokenCount(12_345)).toBe("12K");
    expect(helpers.formatTokenCount(1_500_000)).toBe("1.5M");
  });

  it("resolveStatus prefers the summary when present", () => {
    const summary: SubagentRunSummary = {
      status: "error",
      durationMs: 1,
      usage: { input: 0, output: 0, cache_read: 0, cache_creation: 0, total: 0 },
    };
    expect(helpers.resolveStatus({ isRunning: true, summary })).toBe("error");
    expect(helpers.resolveStatus({ isRunning: false, summary })).toBe("error");
  });

  it("resolveStatus falls back to isRunning when no summary is set", () => {
    expect(helpers.resolveStatus({ isRunning: true })).toBe("running");
    expect(helpers.resolveStatus({ isRunning: false })).toBe("success");
    expect(helpers.resolveStatus({})).toBe("success");
  });

  it("STATUS_STYLES covers every SubagentRunStatus", () => {
    const expected = ["running", "blocked", "success", "error", "cancelled"];
    for (const s of expected) {
      expect(helpers.STATUS_STYLES[s as keyof typeof helpers.STATUS_STYLES]).toBeDefined();
    }
  });
});
