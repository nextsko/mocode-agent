# HTTP Control Plane

Mocode exposes three distinct HTTP surfaces. They share logging via
`internal/httputil` but serve different audiences.

| Surface | Package | Audience | API prefix |
|---------|---------|----------|------------|
| Chat Web UI | `internal/web` | Browser chat | `/api/*` + WebSocket |
| Admin settings | `internal/admin` | Local config/MCP/WeChat | `/api/*` |
| Backend RPC | `internal/server` + `internal/backend` | Remote client/daemon | `/v1/*` |

## Security defaults

- **Web**: binds to `127.0.0.1` by default; WebSocket origins restricted to local hosts.
- **Admin**: binds to `127.0.0.1`; API routes require bearer token when started.
- **Server**: Unix socket / named pipe by default.

## Shared middleware

- `httputil.RequestLogging` — structured access logs
- `httputil.BearerAuth` — token gate for admin API
- `httputil.IsLoopbackRequest` — guards sensitive config mutations (e.g. Yolo)

## Frontend build

- React source: `web/`
- Go embed target: `internal/web/dist/` (built via `vite` with `outDir`)

## Related

- [workspace-types.md](./workspace-types.md)
- [internal/README.md](../../internal/README.md)
