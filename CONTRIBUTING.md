# Development

Use Go 1.25 or later on Windows x64. Dependencies are pinned in `go.mod` and `go.sum`.

```cmd
build.cmd
build.cmd check
```

The default command builds `portable-update.exe`. `check` runs vet, compilation and tests. `build.cmd test` runs tests; `build.cmd fmt` formats Go sources.

The direct build command is:

```cmd
go build -trimpath -ldflags="-s -w" -o portable-update.exe ./cmd/portable-updater
```

Windows checks run on pushes and pull requests through [.github/workflows/test.yml](.github/workflows/test.yml).

## Source layout

```text
cmd/portable-updater/   executable entry point
internal/              implementation packages and their tests
adapters/              emulator definitions
schemas/               adapter and container JSON schemas
examples/              container configurations
scripts/launchers/     command files copied into game roots
docs/                  architecture
```

See [architecture](docs/architecture.md) for package boundaries. Generated executables, logs and local files under `tmp/` are ignored by Git.

Tests cover download retry/resume, byte ranges and digests, version decisions, path/archive boundaries, process locks, preservation, rollback and interrupted swaps. They use fixtures and do not launch games or certify save-format compatibility. Changing an adapter also needs an official-package update/no-op/rollback check on a disposable container.
