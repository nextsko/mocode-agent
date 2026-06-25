# Workspace Types

Three different types share the name "Workspace". They are intentionally
separate layers.

| Type | Package | Role |
|------|---------|------|
| `workspace.Workspace` | `internal/workspace` | Frontend interface (TUI, web, WeChat) |
| `backend.Workspace` | `internal/backend` | Running in-process workspace instance |
| `proto.Workspace` | `internal/proto` | Wire DTO for client/server RPC |

## Data flow

```
TUI / Web / Gateway
       │
       ▼
workspace.Workspace  (AppWorkspace or ClientWorkspace)
       │
       ├── AppWorkspace ──► app.App ──► store
       │
       └── ClientWorkspace ──► client.Client ──► server ──► backend.Workspace
```

## When editing

- Add a user-facing capability → extend `workspace.Workspace` interface first.
- Implement locally → `AppWorkspace` delegates to `app.App`.
- Implement remotely → `ClientWorkspace` maps to proto calls.

## Related

- [control-plane.md](./control-plane.md)
