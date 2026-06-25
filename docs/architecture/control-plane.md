# HTTP Control Plane

Mocode exposes one HTTP surface for local use. Admin middleware lives in
`internal/admin`.

| Surface | Package | Audience | API prefix |
|---------|---------|----------|------------|
| Admin settings | `internal/admin` | Local config/MCP/WeChat | `/api/*` |

## Security defaults

- **Admin**: binds to `127.0.0.1`; API routes require bearer token when started.

## Admin middleware

- `requestLogging` — structured access logs
- `bearerAuth` — token gate for admin API

## Related

- [workspace-types.md](./workspace-types.md)
- [internal/README.md](../../internal/README.md)
