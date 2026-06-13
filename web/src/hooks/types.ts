import type { ChatStatus, FileUIPart, ToolUIPart } from "ai";
import type { QuestionItem } from "./wireTypes";

export type NoPreviewAttachment = {
  kind: "nopreview";
  filename: string;
};

export type VideoNoPreviewAttachment = {
  kind: "video-nopreview";
  mediaType: string;
  filename: string;
};

export type MessageAttachmentPart = FileUIPart | NoPreviewAttachment | VideoNoPreviewAttachment;

// Re-export API types for convenience
export type { Session } from "../lib/api/models";

/**
 * A single step recorded from a subagent's activity.
 * Accumulated as SubagentEvents arrive and rendered inside the parent Agent tool call.
 */
export type SubagentStep =
  | { kind: "thinking"; text: string }
  | { kind: "text"; text: string }
  | {
      kind: "tool-call";
      toolCallId: string;
      toolName: string;
      /** Raw accumulated arguments string (for streaming ToolCallPart) */
      rawArgs?: string;
      input?: unknown;
      status: "running" | "success" | "error";
      output?: string;
      errorText?: string;
    };

/**
 * Terminal status for a sub-agent run.
 *
 * - "running": the sub-agent is still executing
 * - "success": the sub-agent finished without error
 * - "error": the sub-agent failed
 * - "cancelled": the sub-agent was cancelled by the user
 * - "blocked": the sub-agent was blocked because a DAG dependency failed
 */
export type SubagentRunStatus =
  | "running"
  | "success"
  | "error"
  | "cancelled"
  | "blocked";

/**
 * SubagentTokenUsage is the wire-level token usage reported on each
 * SubagentCompleted event. Mirrors the backend's notify.SubagentTokenUsage.
 *
 * NOTE: the backend emits int64 values. The JSON wire format encodes them
 * as plain numbers, which the JavaScript client receives as IEEE-754
 * doubles. Token counts well under Number.MAX_SAFE_INTEGER (2^53 - 1)
 * round-trip exactly, which holds for every realistic LLM run today.
 */
export type SubagentTokenUsage = {
  input: number;
  output: number;
  cache_read: number;
  cache_creation: number;
  total: number;
};

/**
 * SubagentRunSummary is the persisted summary of a sub-agent run, attached
 * to the parent tool call when the backend publishes a SubagentCompleted
 * event. It is used by the UI to render the post-run header and to keep
 * duration/token counts visible after the live stream ends.
 */
export type SubagentRunSummary = {
  status: SubagentRunStatus;
  durationMs: number;
  usage: SubagentTokenUsage;
  summary?: string;
  error?: string;
};

/**
 * Live message in the chat - this is a UI-specific type
 * that extends beyond what the API provides
 */
export type LiveMessage = {
  /** Unique identifier for this UI message (React key) */
  id: string;
  /** Backend message ID from StatusUpdate event (identifies the turn) */
  messageId?: string;
  /** 0-based turn index, set on user messages at TurnBegin */
  turnIndex?: number;
  role: "user" | "assistant";
  content?: string;
  attachments?: MessageAttachmentPart[];
  isStreaming?: boolean;
  variant?:
    | "text"
    | "chain-of-thought"
    | "tool"
    | "code"
    | "thinking"
    | "message-id"
    | "status";
  /** Thinking/reasoning content from the model */
  thinking?: string;
  /** Duration of thinking in seconds */
  thinkingDuration?: number;
  chainOfThought?: {
    title: string;
    steps: {
      label: string;
      description: string;
    }[];
    revealedSteps: number;
    relatedSources?: string[];
  };
  toolCall?: {
    title: string;
    type: ToolUIPart["type"];
    state:
      | ToolUIPart["state"]
      | "approval-requested"
      | "approval-responded"
      | "question-requested"
      | "question-responded"
      | "output-denied";
    input?: ToolUIPart["input"];
    /** Tool call ID for tracking */
    toolCallId?: string;
    /**
     * Tool result fields (aligned with backend ToolReturnValue)
     * @see kosong.tooling.ToolReturnValue
     */
    /** The output content returned by the tool (for model) */
    output?: string;
    /** An explanatory message to be given to the model */
    message?: string;
    /** Content blocks to be displayed to the user */
    display?: Array<{ type: string; data: unknown }>;
    /** Extra debugging/testing data */
    extras?: Record<string, unknown>;
    /** Whether the tool call resulted in an error */
    isError?: boolean;
    /** Error text for display (derived from message when isError) */
    errorText?: string;
    /** Media parts extracted from tool output (images/videos from ReadMediaFile etc.) */
    mediaParts?: Array<{ type: "image_url" | "video_url"; url: string }>;
    approval?: {
      id: string;
      action: string;
      description: string;
      sender: string;
      toolCallId?: string;
      submitted?: boolean;
      resolved?: boolean;
      approved?: boolean;
      reason?: string;
      response?: unknown;
      feedback?: string;
      sourceKind?: "foreground_turn" | "background_agent" | null;
      sourceDescription?: string | null;
    };
    question?: {
      id: string;
      toolCallId: string;
      questions: QuestionItem[];
      rpcMessageId?: string | number;
      submitted?: boolean;
      resolved?: boolean;
      answers?: Record<string, string>;
    };
    /** Steps from a subagent (Agent tool) — populated by SubagentEvent processing */
    subagentSteps?: SubagentStep[];
    /** Whether the subagent is still actively running */
    subagentRunning?: boolean;
    /** Built-in subagent type (coder / explore / plan) */
    subagentType?: string;
    /** Subagent instance ID */
    subagentAgentId?: string;
    /** True when this tool message was created from a sub-agent's ApprovalRequest
     *  (the tool_call_id belongs to the sub-agent, not the main agent) */
    isSubagentOrigin?: boolean;
    /** Terminal run summary populated from a SubagentCompleted event. */
    subagentRunSummary?: SubagentRunSummary;
  };
  codeSnippet?: {
    title: string;
    code: string;
    language: string;
    description?: string;
  };
};

/**
 * Session operations returned by useSessions
 * Uses API types: Session
 */
export type SessionOperations = {
  sessions: import("../lib/api/models").Session[];
  selectedSessionId: string;
  isLoading: boolean;
  error: string | null;
  refreshSessions: () => Promise<void>;
  loadMoreSessions: () => Promise<void>;
  hasMoreSessions: boolean;
  isLoadingMore: boolean;
  searchQuery: string;
  setSearchQuery: (query: string) => void;
  refreshSession: (
    sessionId: string,
  ) => Promise<import("../lib/api/models").Session | null>;
  createSession: () => Promise<import("../lib/api/models").Session>;
  deleteSession: (sessionId: string) => Promise<boolean>;
  selectSession: (sessionId: string) => void;
  applySessionStatus: (
    status: import("../lib/api/models").SessionStatus,
  ) => void;
  getRelativeTime: (session: import("../lib/api/models").Session) => string;
};

/**
 * Chat operations
 */
export type ChatOperations = {
  messages: LiveMessage[];
  status: ChatStatus;
  sendMessage: (text: string, attachments?: FileUIPart[]) => Promise<void>;
  cancelStream: () => void;
  clearMessages: () => void;
};
