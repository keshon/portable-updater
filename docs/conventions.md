# Development conventions

## Build and checks

Use a patched Go toolchain on Windows x64. The module language floor is Go 1.25; `go.mod` recommends Go 1.27.3, which Go can download automatically. Dependencies are pinned in `go.mod` and `go.sum`. Run these commands from the repository root:

```cmd
build.cmd
build.cmd check
```

The default command builds `portable-update.exe` with `CGO_ENABLED=0`. `check` runs vet, compilation and tests. `build.cmd test` runs tests; `build.cmd fmt` formats Go sources. The direct executable build is:

```cmd
go build -trimpath -ldflags="-s -w" -o portable-update.exe ./cmd/portable-updater
```

[Windows CI](../.github/workflows/test.yml) runs `go test -timeout 2m ./...`, `go vet ./...`, a vulnerability scan with `govulncheck`, and an executable build on pushes and pull requests. Formatting is a contributor practice; that workflow has no formatting check.

## Source layout

| Path | Responsibility |
| --- | --- |
| `cmd/portable-updater` | Thin executable entry point |
| `cmd/package` | Release ZIP and checksum packaging |
| `bundle.go` | Embedded canonical distribution assets |
| `internal` | Implementation packages and their tests |
| `adapters` | Reusable application definitions |
| `examples` | Container configurations paired with adapters |
| `schemas` | JSON schema definitions for editing and distribution |
| `scripts/launchers` | Command files copied into container roots |
| `docs` | Operating guide, architecture and contributor conventions |

See [architecture](architecture.md) for package boundaries. Keep generated executables, logs and private verification artifacts under ignored paths. Test probe programs with standalone `main` functions must not share a directory: Go package discovery includes directories even when Git ignores them.

## Extending support

For an application using an existing provider, package format and version policy:

1. Add an adapter under `adapters` and a matching container configuration under `examples`. Follow the existing `<application>-windows.json` and `<application>.json` naming practice.
2. Select the exact portable Windows x64 artifact. An asset expression must distinguish architectures, installers and alternate builds. Use paths supported by the upstream package and actual portable installation.
3. Set a friendly `display_name` in the adapter; custom adapters without it fall back to their identifier. Identify runtime-local settings for `preserve`, package contents for `required_package` and pre-existing portable settings for `required_state`. Snapshot user-data paths belong in the container example's `saves` list.
4. Use the existing checksum policy unless the provider supplies no publisher digest. Explain any `when-published` exception in review; a locally computed hash is not publisher verification.
5. Perform the automated and disposable-container checks below, then add the adapter/example links to the README catalog. Put application-specific setup in the operating guide.

The [adapter schema](../schemas/adapter.schema.json) and [instance schema](../schemas/instance.schema.json) define the document formats. Runtime loading is implemented in `internal/config`; the updater does not run a general JSON Schema validator. When adding a field, keep the schema, Go model, loader validation, examples and affected documentation consistent.

For a JSON release endpoint, use the declarative feed fields below before adding a provider. For repository release assets split across multiple archives, `source.companion_patterns` selects up to four required companion archives from the same release. Each expression must match exactly one different asset. Companions require archive preparation and `artifact-sha256` identity, merge at the candidate root without replacing files, and participate in receipt comparison. The [llama.cpp adapter](../adapters/llamacpp-windows.json) is an example.

A new release protocol belongs in `internal/release`; a new package preparation method belongs in `internal/updater`. Register supported values in configuration validation and schemas. Package implementations use fixed behavior. The `executable-unpack` format runs only the verified download, with an adapter argument array and exactly one `{destination}` placeholder. It cannot select another program or invoke a shell. Keep application-specific flags and executable names in adapters.

### JSON release feeds

Use `source.provider: "json-feed"` for a single release document. The [RPCS3 adapter](../adapters/rpcs3-windows.json) shows a nested object and published SHA-256; the [Dolphin adapter](../adapters/dolphin-windows.json) shows an artifact array and HEAD size lookup.

| `source.feed` field | Contract |
| --- | --- |
| `channels` | Explicit `stable`, `latest` and/or `nightly` HTTPS endpoints; undefined channels fail. `{os_version}` expands to the URL-escaped Windows version. No other placeholders are supported. |
| `version`, `updated_at` | Document-relative JSON Pointers; version is required, updated identity is optional. Selected values must be nonempty strings. |
| `asset` | Document-relative JSON Pointer to an object or artifact array. |
| `match` | Asset-relative JSON Pointers mapped to exact string values. All predicates must match; exactly one artifact must remain. |
| `url` | Asset-relative JSON Pointer to a package HTTPS URL. Its basename supplies the filename. |
| `size` / `size_from_head` | Exactly one source: an asset-relative pointer to an integer byte count, or `true` to request Content-Length with HEAD. Size must be positive and at most 2 GiB. |
| `sha256` | Optional asset-relative pointer to 64 hexadecimal digits. Required unless `source.checksum` explicitly permits an unpublished digest. When declared, a missing or malformed value fails even with that policy. |
| `allowed_download_prefixes` | Nonempty list of HTTPS directory URLs ending in `/`. The selected URL must begin with one. This constrains the initial URL; HTTPS redirects use the shared client policy. |

Pointers use JSON Pointer escaping (`~0` for `~`, `~1` for `/`) and numeric array indices. They are selectors, not expressions or scripts. JSON-feed definitions cannot also contain repository, host or asset-pattern fields. Keep each upstream URL, channel mapping, field name and artifact predicate in its adapter.

Ordinary tests use fixtures. To resolve the official RPCS3 and Dolphin feeds without downloading packages, set `PORTABLE_UPDATER_LIVE_TEST=1` and run `go test -v -run TestJSONFeedOfficialLive ./internal/release`. These opt-in tests require network access and do not validate an installed application.

## Rules and their enforcement

| Tier | Rule and verification |
| --- | --- |
| Enforced | `internal/config.Load` rejects unknown JSON fields, unsupported versions/providers/package formats and invalid configured paths. JSON schemas describe additional editing constraints; do not assume every schema constraint is checked by the loader. |
| Enforced | Path, tree and archive helpers reject unsafe paths and links in the operations they validate. Existing tests cover traversal, overlaps and Windows short-name aliases; this is not a general sandbox. |
| Enforced | Process guards reject replacement while an inspected executable is active in a protected runtime. The Windows lock prevents concurrent updater processes using the same updater home. |
| Enforced | Candidate validation checks required package entries and the configured executable's architecture or version. It does not prove that the application starts or preserves data compatibility. |
| Invariant | Keep release discovery, downloading and installation separate. Moving filesystem replacement into a provider bypasses the transaction's preservation and recovery flow. |
| Invariant | Keep user-data preservation and runtime replacement explicit. Treating model libraries or application data as disposable runtime contents can lose them during replacement. |
| Practice | Keep the command entry point thin, tests beside their owning packages and adapter rules specific to the package they support. |

## Verification for changes

Automated tests cover the setup review/edit flow, ambiguous and custom layouts, cancellation, plain/color output separation, download retry/resume, ranges and digests, release/version decisions, path and archive boundaries, locking, preservation, rollback and interrupted swaps. Executable-unpack tests substitute a runner with fixture arguments; they do not download or execute a live release in CI.

For an adapter or package-format change, also use a disposable copy of a representative container:

1. Record the starting runtime and protected data. Close its processes.
2. Update from an official package and inspect the selected asset and log.
3. Confirm protected data is unchanged and run an application-specific smoke check.
4. Repeat the update and confirm it reports current without replacing the runtime.
5. Roll back and verify the previous runtime and expected data behavior.

Record what the manual check covered. A `--help` check does not validate model inference; package tests do not certify emulator save compatibility. Keep private reports under ignored `tmp/verification` rather than in current user instructions.

## Documentation pattern

Follow the repository copy of the [shared documentation skill](../.agents/skills/docs-pattern/SKILL.md), using Melodix as its reference. The README introduces the project and links to guides; `running.md` owns operations, `architecture.md` owns system behavior, and this file owns contributor workflows. Keep these documents in English and give each subject one maintained home.

No README generator or documentation-specific CI check is currently configured here. Verify relative links, heading fragments, commands and source-backed claims when editing documentation. Reuse the shared pattern without importing another project's generators or convention-test machinery.

The dated [application audit](internal/app-audit-2026-11-05.md) records the November 2026 review, verification scope and remaining findings. Current behavior belongs in the operating and architecture guides.

## Release packaging and demos

Build a versioned executable from the repository root, then package it:

```cmd
go build -trimpath -ldflags="-s -w -X=github.com/keshon/portable-updater/internal/app.Version=v0.1.0" -o portable-update.exe ./cmd/portable-updater
go run ./cmd/package -version v0.1.0
```

Set `CGO_ENABLED=0` for release builds. The packager verifies the executable entry point and reported version before writing a ZIP, `SHA256SUMS.txt` and release-note text under ignored `dist`. It refuses to overwrite an existing ZIP. Use a fresh output directory with `-out` for another candidate build.

The [release workflow](../.github/workflows/release.yml) tests, vets, scans vulnerabilities, builds and packages on Windows. A `v*` tag publishes a GitHub release from that tag after checks pass. Manual dispatch builds an artifact only. Confirm the source revision, archive contents, checksum and extracted executable's version before publishing a tag. Test first-time setup from an extracted archive as well as updating an existing container.

README examples should show actual console output or direct recordings of the application. Do not render logs inside an invented interface. Keep verification summaries separate from program output, and distinguish captured output from manual annotations.
