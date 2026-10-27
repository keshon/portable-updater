# Portable Updater

A Windows updater for emulator-based portable game folders. Double-click `updater-update.cmd` to check for a release and install it. Use `updater-rollback.cmd` to restore the previous emulator.

The executable runs without installed Go, Python, PowerShell or 7-Zip. Each game carries its own configuration, adapter, logs and backups. Updater files live in `util/portable-update`; working files and retained runtime copies live in `tmp/portable-update`.

## Source layout

```text
cmd/portable-updater/   executable entry point
internal/              application and implementation packages
adapters/              reusable emulator definitions
schemas/               adapter and container JSON schemas
examples/              container configuration examples
scripts/launchers/     Windows launcher templates
docs/                  architecture and operation details
tmp/                    local verification output (ignored)
```

See [architecture](docs/architecture.md) for package responsibilities and dependency boundaries.

## Build

On Windows amd64 with Go 1.25 or later:

```cmd
build.cmd
build.cmd check
```

The first command produces `portable-update.exe`. `check` runs vet, compilation and tests; `test` and `fmt` are also available. The direct build command is:

```cmd
go build -trimpath -ldflags="-s -w" -o portable-update.exe ./cmd/portable-updater
```

Dependencies are pinned in `go.mod` and `go.sum`. Their license notices are in [THIRD-PARTY-LICENSES.txt](THIRD-PARTY-LICENSES.txt).

## Game container

```text
Game/
  updater-update.cmd
  updater-rollback.cmd
  pcsx2/                       active emulator and portable settings
  data/                        saves and other game data
  util/
    portable-update/
      portable-update.exe
      update.json              container paths and release channel
      adapters/
      schemas/
      state/                   installation receipt and recovery journal
  tmp/
    portable-update/           cache, staging, previous runtimes, snapshots, logs
```

The [container configuration](examples/update.json) selects an adapter, runtime folder, channel and save paths. An adapter supplies the official release source, asset pattern, version policy, protected settings and required files. Definitions use relative paths and do not execute shell commands or installers.

The supplied adapters cover PCSX2, RPCS3, Cemu, Xenia, Dolphin, shadPS4 and DOSBox Pure. Each has passed an official-package download, update, no-op and rollback cycle on a disposable Windows container, with protected-state hash checks. These checks do not certify gameplay or save-format compatibility. The shadPS4 adapter updates the core under its launcher version directory; it does not update the launcher GUI.

## Operation

The updater refuses a running emulator, selects an official release and downloads its portable ZIP or 7z package. Downloads retain completed ranges and partial chunks for retry. It checks the package, prepares a separate candidate, snapshots configured saves and copies protected settings before swapping runtime folders. The previous runtime remains available for rollback. Recognizable interrupted swaps are recovered on the next run.

PCSX2 compares numeric Windows executable versions. Adapters with `artifact-sha256` identify installations through the selected release and executable hash; their first check may need to download the package for comparison.

An adapter normally requires an upstream SHA-256. Where a publisher does not supply one, `when-published` explicitly permits HTTPS delivery with archive integrity and executable checks. A locally recorded hash protects subsequent cache reads; it does not substitute for a published checksum.

Successful launchers close automatically. Errors keep the console open; details go to `tmp/portable-update/update.log`. Rollback restores the previous runtime and its settings from the update moment. External saves retain their current progress.

## Limits and verification

Tests cover downloader retries/resume, digest and byte-range checks, version decisions, path/archive boundaries, process locks, preservation, rollback and interrupted swaps. Tests do not launch games or prove save compatibility.

- Windows amd64 only. Linux testing is deferred.
- No updater self-update, whole-collection update command or registry/profile isolation.
- Save snapshots above 2 GiB stop installation.
- Removing `tmp/portable-update` discards cache, snapshots and rollback copies. Active saves stay in their configured game data paths. Keep the workspace while an update is running.
- Journal recovery cannot guarantee recovery from disk corruption or power failure.
