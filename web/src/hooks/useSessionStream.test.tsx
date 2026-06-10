import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useSessionStream } from "./useSessionStream";

type WireEnvelope = Record<string, unknown>;

class MockWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;
  static instances: MockWebSocket[] = [];

  readonly url: string;
  readyState = MockWebSocket.CONNECTING;
  sent: WireEnvelope[] = [];
  protocol = "";
  extensions = "";
  binaryType: BinaryType = "blob";
  bufferedAmount = 0;

  onopen: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent<string>) => void) | null = null;
  onclose: ((event: CloseEvent) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;

  constructor(url: string) {
    this.url = url;
    MockWebSocket.instances.push(this);
  }

  send(data: string): void {
    this.sent.push(JSON.parse(data) as WireEnvelope);
  }

  close(): void {
    this.readyState = MockWebSocket.CLOSED;
    this.onclose?.({ code: 1000, reason: "" } as CloseEvent);
  }

  open(): void {
    this.readyState = MockWebSocket.OPEN;
    this.onopen?.(new Event("open"));
  }

  emitMessage(message: WireEnvelope): void {
    this.onmessage?.({
      data: JSON.stringify(message),
    } as MessageEvent<string>);
  }

  static latest(): MockWebSocket {
    const socket = MockWebSocket.instances.at(-1);
    if (!socket) {
      throw new Error("expected an active websocket");
    }
    return socket;
  }

  static reset(): void {
    MockWebSocket.instances = [];
  }
}

describe("useSessionStream", () => {
  const realWebSocket = globalThis.WebSocket;

  beforeEach(() => {
    vi.useFakeTimers();
    MockWebSocket.reset();
    globalThis.WebSocket = MockWebSocket as unknown as typeof WebSocket;
  });

  afterEach(() => {
    globalThis.WebSocket = realWebSocket;
    MockWebSocket.reset();
    vi.useRealTimers();
  });

  const openSessionSocket = () => {
    act(() => {
      vi.advanceTimersByTime(50);
    });
    const socket = MockWebSocket.latest();
    act(() => {
      socket.open();
      socket.emitMessage({
        jsonrpc: "2.0",
        method: "session_status",
        params: {
          session_id: "session-1",
          state: "idle",
          seq: 1,
          updated_at: "2026-06-09T12:00:00Z",
        },
      });
      socket.emitMessage({
        jsonrpc: "2.0",
        method: "history_complete",
        id: "history-complete",
      });
    });
    return socket;
  };

  it("returns to ready after cancel and can send a follow-up prompt", async () => {
    const { result } = renderHook(() =>
      useSessionStream({ sessionId: "session-1" }),
    );
    const socket = openSessionSocket();

    await act(async () => {
      await result.current.sendMessage("first");
    });

    const promptCountBeforeCancel = socket.sent.filter(
      (msg) => msg.method === "prompt",
    ).length;
    expect(promptCountBeforeCancel).toBe(1);

    act(() => {
      result.current.cancel();
      socket.emitMessage({
        jsonrpc: "2.0",
        method: "event",
        params: {
          type: "StepInterrupted",
          payload: {},
        },
      });
      socket.emitMessage({
        jsonrpc: "2.0",
        method: "session_status",
        params: {
          session_id: "session-1",
          state: "idle",
          seq: 2,
          reason: "prompt_cancelled",
          updated_at: "2026-06-09T12:00:01Z",
        },
      });
    });

    expect(result.current.status).toBe("ready");

    await act(async () => {
      await result.current.sendMessage("second");
    });

    const promptMessages = socket.sent.filter((msg) => msg.method === "prompt");
    expect(promptMessages).toHaveLength(2);
    expect(
      (promptMessages[1].params as { user_input: string }).user_input,
    ).toBe("second");
  });

  it("updates one tool message while a background job streams to completion", () => {
    const { result } = renderHook(() =>
      useSessionStream({ sessionId: "session-1" }),
    );
    const socket = openSessionSocket();

    act(() => {
      socket.emitMessage({
        jsonrpc: "2.0",
        method: "event",
        params: {
          type: "ToolCall",
          payload: {
            type: "function",
            id: "tool-1",
            function: {
              name: "Shell",
              arguments: JSON.stringify({ command: "printf hi" }),
            },
          },
        },
      });
      socket.emitMessage({
        jsonrpc: "2.0",
        method: "event",
        params: {
          type: "ToolResult",
          payload: {
            tool_call_id: "tool-1",
            return_value: {
              is_error: false,
              output: "hi\n",
              message: "Background task is still running.",
              display: [],
              extras: {
                background: true,
                shell_id: "001",
                job_status: "running",
                tty: true,
              },
            },
          },
        },
      });
    });

    expect(result.current.messages).toHaveLength(1);
    expect(result.current.messages[0].toolCall?.state).toBe("input-available");
    expect(result.current.messages[0].isStreaming).toBe(true);
    expect(result.current.messages[0].toolCall?.output).toBe("hi\n");

    act(() => {
      socket.emitMessage({
        jsonrpc: "2.0",
        method: "event",
        params: {
          type: "ToolResult",
          payload: {
            tool_call_id: "tool-1",
            return_value: {
              is_error: false,
              output: "hi\nthere\n",
              message: "Background task is still running.",
              display: [],
              extras: {
                background: true,
                shell_id: "001",
                job_status: "running",
                tty: true,
              },
            },
          },
        },
      });
    });

    expect(result.current.messages).toHaveLength(1);
    expect(result.current.messages[0].toolCall?.output).toBe("hi\nthere\n");
    expect(result.current.messages[0].isStreaming).toBe(true);

    act(() => {
      socket.emitMessage({
        jsonrpc: "2.0",
        method: "event",
        params: {
          type: "ToolResult",
          payload: {
            tool_call_id: "tool-1",
            return_value: {
              is_error: false,
              output: "hi\nthere\ndone\n",
              message: "Background task completed.",
              display: [],
              extras: {
                background: true,
                shell_id: "001",
                job_status: "completed",
                tty: true,
              },
            },
          },
        },
      });
    });

    expect(result.current.messages).toHaveLength(1);
    expect(result.current.messages[0].toolCall?.state).toBe("output-available");
    expect(result.current.messages[0].toolCall?.output).toBe("hi\nthere\ndone\n");
    expect(result.current.messages[0].isStreaming).toBe(false);
  });
});
