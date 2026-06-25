# Contributing to Mocode

Thank you for contributing. Start with [AGENTS.md](AGENTS.md) for architecture,
build commands, and code style.

## Quick Start

```bash
go build -buildvcs=false -o bin/mocode .
go test ./internal/store/...
task fmt
task lint:fix
```

## Post-Change Pipeline

Follow the **Post-Change Local Pipeline** in [AGENTS.md](AGENTS.md):

1. Build
2. Test touched packages
3. Format (`task fmt`)
4. Lint (`task lint:fix`)
5. Review diff
6. Update docs if conventions changed
7. Semantic commit with emoji prefix (see `internal/config/templates/modes/git.md`)

## Navigation

- **Architecture**: [AGENTS.md](AGENTS.md) + [internal/README.md](internal/README.md)
- **TUI development**: [internal/ui/AGENTS.md](internal/ui/AGENTS.md)
- **Dev notes**: [docs/dev-notes/](docs/dev-notes/)
- **Structure governance**: [docs/dev-notes/structure-governance-baseline.md](docs/dev-notes/structure-governance-baseline.md)

## Commit Format

```
<emoji> <type>(<scope>): <subject>

详细描述：
- change 1
- change 2
```

See `internal/config/templates/modes/git.md` for emoji + type mapping.
