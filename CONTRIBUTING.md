<img src=".github/assets/contributing.png" width="480" alt="contributing">

Keep changes small, test them, and don't add AI slop :)

### Workflow

Use an issue for bugs, questions, and bigger ideas. Small fixes can go straight into a PR

Create a branch from `main`:

```bash
git switch main
git pull --ff-only
git switch -c fix/macos-tun
```

Branch prefixes:

| Prefix | Use |
| --- | --- |
| `feat/` | New functionality |
| `fix/` | Bug fix |
| `refactor/` | No behavior change |
| `docs/` | Documentation |
| `chore/` | Maintenance and releases |
| `ci/` | GitHub Actions |

Before opening a PR:

```bash
go vet ./...
go test ./...
golangci-lint run ./...
```

Push the branch and open a PR against `main`. Link an issue with `Closes #67` when relevant!

### Commit prefixes

Use one of these prefixes:

| Prefix | Use |
| --- | --- |
| `feat:` | New functionality |
| `fix:` | Bug fix |
| `refactor:` | No behavior change |
| `docs:` | Documentation |
| `chore:` | Maintenance and releases |
| `ci:` | GitHub Actions |

Pick the most important prefix if a change touches several areas. In the PR, say what changed and how it was tested. Issues/PR's without prefix will be automatically closed
