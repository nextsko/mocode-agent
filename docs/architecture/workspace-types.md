# Workspace Types

`workspace.Workspace` is the single frontend facade. (The legacy
`proto.Workspace` wire DTO was removed with the in-process HTTP transport.)

| Type | Package | Role |
|------|---------|------|
| `workspace.Workspace` | `internal/workspace` | Frontend interface (TUI, WeChat) |

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
