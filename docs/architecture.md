# Architecture

`cmd/portable-updater` is the process entry point. It passes arguments and output streams to `internal/app`; update behavior does not live in the command package.

| Package | Responsibility |
| --- | --- |
| `internal/app` | CLI flags, process lock, log lifetime and operation dispatch |
| `internal/config` | Container/adapter models, JSON loading and path/policy validation |
| `internal/release` | GitHub, Forgejo, RPCS3 and Dolphin release discovery |
| `internal/download` | Independent range connections, resume, cache and digest verification |
| `internal/updater` | Version decisions, preparation, preservation, transactional replacement and restoration |
| `internal/platform` | Windows executable version resources, running processes and OS-owned locks |
| `internal/files` | Container boundaries, file copying, archive extraction and file hashes |
| `internal/httpx` | Shared HTTP transport defaults |

Release protocol types belong to `release`. Configuration refers to those source descriptions; release providers do not depend on container configuration or installation state. The downloader accepts a selected release asset and does not select releases or replace installations.

`updater.New` loads the transaction journal after configuration is validated. A journal naming an unrelated runtime directory is rejected. `Engine.Update` and `Engine.Restore` are the application-facing operations. Installation helpers and fault-injection points remain private to the updater package.

Tests live with their owning packages. Source-project verification reports and probe output are local files under ignored `tmp/verification`; they are not runtime configuration. Per-game runtime configuration, caches, logs and journals stay in the game container.

Adapter JSON files are reusable emulator rules. Container JSON files select an adapter and provide the runtime and save paths. Schemas remain at the project root for editing/distribution; launcher templates live in `scripts/launchers`.

The container root is derived from the configuration under `util/portable-update`. That directory holds the binary, adapters, schemas, lock and persistent installation/recovery metadata. `tmp/portable-update` holds downloads, clean candidates, previous/rejected runtimes, unpacked packages, save snapshots, history and logs. Replacement stays on the container volume. Shared utilities and the active runtime keep their existing paths.
