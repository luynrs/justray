<img src=".github/assets/contributing.png" width="480" alt="contributing">

Keep changes small, test them, and don't add AI slop :)

### Workflow

Use an issue for bugs, questions, and bigger ideas. Small fixes can go straight into a PR

Create a branch from `main` using one of the prefixes below:

| Type | Branch | Commit | Use |
| --- | --- | --- | --- |
| Feature | `feat/` | `feat:` | New functionality |
| Fix | `fix/` | `fix:` | Bug fixes |
| Performance | `perf/` | `perf:` | Performance improvements |
| Refactor | `refactor/` | `refactor:` | Code changes without behavior changes |
| Documentation | `docs/` | `docs:` | Documentation |
| Chore | `chore/` | `chore:` | Maintenance and releases |
| CI | `ci/` | `ci:` | GitHub Actions |

For example:

```bash
git switch main
git pull --ff-only
git switch -c fix/macos-tun
```

Use the matching prefix for commits and PR titles:

```text
feat: add auto-connect
fix: restore routes on macOS
perf: reduce startup time
refactor: simplify config loading
docs: update installation guide
chore: prepare release
ci: update release workflow
```

Scopes are optional:

```text
feat(tui): better routing settings
fix(tun): dns hijacking
refactor(config): simplify validation
```

Pick the most important type if a change touches several areas.

### Before opening a PR

```bash
go vet -tags with_quic,with_utls,with_gvisor,with_grpc,with_xhttp,badlinkname ./...
go test -tags with_quic,with_utls,with_gvisor,with_grpc,with_xhttp,badlinkname ./...
golangci-lint run --build-tags with_quic,with_utls,with_gvisor,with_grpc,with_xhttp,badlinkname ./...
```

Push your branch and open a PR against `main`

Describe what changed and how it was tested. Link an issue with `Closes #67` when relevant

Issues and PRs without a valid prefix may be closed automatically.