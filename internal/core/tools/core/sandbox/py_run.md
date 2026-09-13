Execute Python code in an isolated throwaway sandbox (temp dir, wall-clock timeout, bounded output). Prefers uv, which resolves the optional deps list into an EPHEMERAL environment (nothing is installed globally); plain python is the fallback (deps then unavailable).

Use this instead of bash for Python computation, data processing, or quick experiments: the sandbox cannot touch the workspace (pass paths via the SANDBOX_WORKDIR env var when working_dir is set), always terminates at the timeout, and its output is capped so a runaway loop cannot flood the context.
