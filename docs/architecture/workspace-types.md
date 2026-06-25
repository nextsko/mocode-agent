# Workspace Types

Two layers share the name "Workspace". They are intentionally separate.

| Type | Package | Role |
|------|---------|------|
| `workspace.Workspace` | `internal/workspace` | Frontend interface (TUI, WeChat) |
| `proto.Workspace` | `internal/proto` | Wire DTO for admin JSON APIs |

## Data flow

```
TUI / Gateway
       │
       ▼
workspace.Workspace  (AppWorkspace)
       │
       └── AppWorkspace ──► app.App ──► store
```

## When editing

- Add a user-facing capability → extend `workspace.Workspace` interface first.
- Implement locally → `AppWorkspace` delegates to `app.App`.

## Related

- [control-plane.md](./control-plane.md)
