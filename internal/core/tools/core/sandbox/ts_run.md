Execute TypeScript/JavaScript code in an isolated throwaway sandbox (temp dir, wall-clock timeout, bounded output). Prefers bun; falls back to node.

Use this instead of bash when you need to compute something, transform data, or verify a snippet: the sandbox cannot touch the workspace (pass paths via the SANDBOX_WORKDIR env var when working_dir is set), always terminates at the timeout, and its output is capped so a runaway loop cannot flood the context.
