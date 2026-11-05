# Architecture

`cmd/portable-updater` passes arguments and output streams to `internal/app`. An executable without its default configuration opens first-time setup. Explicit manifest and rollback requests report missing configuration instead. The application layer otherwise loads configuration, owns the updater lock and log, and dispatches `Engine.Update` or `Engine.Restore`.

## Components

| Package | Responsibility |
| --- | --- |
| `internal/app` | CLI flags, process lock, log lifetime and operation dispatch |
| `internal/setup` | Bounded application detection, review/edit flow and non-overwriting setup |
| `internal/console` | Optional terminal accents, with plain logs and redirected output |
| `internal/config` | Container/adapter models, JSON loading and path/policy validation |
| `internal/release` | GitHub, Forgejo and declarative JSON-feed release discovery |
| `internal/download` | Independent range connections, resume, cache and digest verification |
| `internal/updater` | Version decisions, package preparation, preservation, replacement and restoration |
| `internal/platform` | Windows executable version resources, running processes and OS-owned locks |
| `internal/files` | Container boundaries, file copying, archive extraction and hashes |
| `internal/httpx` | Shared HTTP transport defaults |

Release protocol types belong to `release`. Configuration refers to those source descriptions; providers do not depend on container configuration or installation state. The downloader accepts a selected release asset and does not select releases or replace installations. Installation helpers and fault-injection points remain private to the updater package.

Release feeds use adapter-defined HTTPS endpoints, JSON Pointers, exact artifact filters and download URL prefixes. The `json-feed` provider has no application-specific endpoints or selectors; RPCS3 and Dolphin are adapter definitions. GitHub and Forgejo remain implementations of repository release protocols.

## Configuration and state ownership

The container root is derived from the configuration's location under `util/portable-update`. Runtime and snapshot paths are relative to that root; adapter paths are relative to the updater home. The [operating guide](running.md#container-configuration) owns field meanings and operator setup.

| State | Owner and purpose |
| --- | --- |
| Container and adapter JSON | Operator-owned policy: runtime, release selection, preservation and snapshot paths |
| Installation receipt | Generated release identity, asset metadata and entry-point hash used for current-version decisions |
| Transaction journal | Generated phase and candidate/previous/snapshot paths used for swap recovery and rollback |
| Active runtime | Application binaries and any configured runtime-local state |
| External user data | Application-owned state outside the runtime; runtime swaps do not replace it |
| Temporary workspace | Downloads, preparation output, retained runtimes, snapshots, history and logs |

The receipt answers whether the selected release appears installed. The journal answers which filesystem transition happened. They are not interchangeable. `updater.New` loads the journal and rejects candidate/previous paths that do not match this runtime's retained-copy naming rules. This is validation, not authentication of locally writable metadata.

All replacement directories stay on the container volume. Updater home and working storage are separate from the active runtime. See [storage and cleanup](running.md#storage-and-cleanup) for exact locations and retention consequences.

## Update flow

1. Recover a recognized interrupted swap, then check active processes and required portable state.
2. Hash the installed entry point and query the configured release provider. A numeric Windows version comparison or matching release/hash receipt can finish without a download.
3. Download or reuse a cached package and verify its integrity according to the provider and adapter policy.
4. Prepare a clean candidate. Archive packages use ZIP/7z extraction. Executable-unpack packages run the verified release asset with declarative adapter arguments, temporary files under the working directory and a bounded subprocess runtime.
5. Stage and validate the candidate's required contents and entry point. A receipt-based installation without a matching receipt can compare candidate files to the installed runtime, excluding protected paths, and record that it is current without replacing it.
6. Snapshot configured data, move aside package defaults at protected paths, and copy the live runtime's protected settings into the candidate.
7. Record the transition, rename the active runtime to a previous copy, and rename the candidate into place. Record the applied phase and installation receipt.

The application layer supplies an eight-minute context for release lookup, downloads and package preparation. An executable-unpack subprocess has a five-minute bound within that context. The subprocess deadline terminates the direct executable; it does not guarantee termination of descendant processes. Synchronous file copying and filesystem swaps do not enforce that deadline. See [integrity and execution](running.md#integrity-and-execution) for checksum policy and the consequences of executing an unpacker. Working-directory separation is not process sandboxing.

## Replacement and recovery

Preparation failures leave the active runtime in place and may retain incomplete output for inspection. Automatic candidate preparation keeps the last applied journal on disk until replacement begins. During replacement, the journal advances through `swapping`, `old_moved` and `applied`; a failed update that leaves the active entry point unchanged retains the earlier applied rollback record. Explicit staging can also persist a `staged` record. A failed candidate rename attempts to put the previous runtime back immediately.

Recovery examines both the journal phase and the existence of active, candidate and previous directories. It restores a displaced runtime or recognizes an already completed transition when the layout is known. Ambiguous combinations stop with an error instead of choosing a copy. Filesystem renames and a journal reduce the scope of interrupted swaps; they do not guarantee durability against disk corruption or every power-loss condition.

Rollback renames the current runtime to a rejected copy and the previous runtime into place. Its `rolling_back` journal phase allows recovery of recognizable interrupted rollback layouts. Runtime-local protected data returns with the old runtime; external data remains current. Snapshots are retained copies and are not automatically restored by rollback.

This mechanism retains the most recent applied transaction for the automatic rollback action. Other retained copies and history are diagnostic artifacts, not a selectable version-history API. Deleting the working directory can leave a journal referring to missing rollback files. Operational recovery and verification steps belong in [troubleshooting](running.md#troubleshooting).

## Verification boundaries

Tests live beside their owning packages and use fixtures and private fault-injection points. They check transaction behavior, not application compatibility. Disposable official-package checks supplement them for adapters and package preparation. The [contributor guide](conventions.md#verification-for-changes) owns those procedures; private reports belong under ignored `tmp/verification`.

## Distribution and setup

The root `bundle.go` embeds the canonical adapters, examples, schemas, launcher templates and license notices. Profiles are derived from those files rather than a second application catalog. Setup reads an existing installation and prepares a review before writing. A single validated candidate skips application/profile/path questions. Ambiguous detection requires a choice. Backup and channel edits stay in the reviewable plan until explicit installation confirmation. Installation creates missing files exclusively and reuses byte-identical existing assets, including the running executable in its final directory. A different existing file is a conflict. On a returned error it removes only the files and empty directories created by that invocation; reused files are retained. Process termination may leave partial setup files. Matching files can be reused on retry, but an existing configuration or orphaned transaction state requires attention rather than being overwritten.

The release packager creates a Windows ZIP containing the executable, notices and a short start guide, plus a separate SHA-256 file. A tagged release workflow runs checks before publishing; manually dispatched builds upload an artifact without creating a public release.
