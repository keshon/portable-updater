# Running Portable Updater

Portable Updater maintains an existing application runtime inside a portable container. It does not install an application for the first time or relocate its settings.

## Requirements

- Windows x64 and a writable container directory.
- An existing runtime matching an [included adapter](../README.md#included-adapters), with its portable mode and user-data paths already configured.
- Network access to the adapter's release service and download host.
- A patched Go toolchain on Windows x64 to build from source; the module recommends Go 1.27.3 and has a Go 1.25 language floor. The resulting updater needs no installed Go, Python, PowerShell or 7-Zip.

The target application still has its own OS, hardware and driver requirements. Close it before updating or restoring its runtime.

## Set up a container

### First-time setup

Download and extract a Windows x64 release ZIP into a temporary folder, then double-click `portable-update.exe`. The executable bundles its profiles, schemas, launcher templates and license notices.

The console wizard asks for the existing container folder, application profile, runtime directory and snapshot paths. Choose the outer folder: for `F:\Projects\ai\koboldcpp\extracted`, the container is `F:\Projects\ai\koboldcpp` and the runtime is `extracted`. Runtime input accepts a relative folder or a full path inside the container. Selecting a recognized default runtime at the first prompt uses its parent container and explains the correction. An already configured container is identified without reinstalling it. It detects profiles at their example runtime paths; choose the profile and enter the relative runtime path for a custom layout. Snapshot defaults include only example paths that already exist. Review them against the application's actual user-data configuration; use comma-separated relative paths or `-` for no snapshots.

Before confirmation, setup checks the runtime entry point and required package/portable files and shows what it will install. It creates `util/portable-update` and both root command launchers, leaving the application and existing launch scripts in place. It does not run an update. Setup can run with only `portable-update.exe` already in `util/portable-update`. It creates missing files and reuses byte-identical bundled files and command launchers. It never overwrites a different existing file or an existing configuration. A configuration-free folder with old updater state is reported as a conflict, rather than inheriting that transaction state into a fresh setup.

If the default `update.json` beside the executable is missing, launching it with no arguments opens setup, including under `util/portable-update`. An explicit missing `--manifest` or `--rollback` reports an error instead. An existing invalid configuration also reports an error rather than starting over.

An installed updater normally runs updates directly. Use `portable-update.exe --setup` explicitly to open the wizard for another container. `--version` prints its build version.

### Build from source

With Go installed, run `build.cmd` from the repository root, then double-click the resulting executable to use the same wizard.

### Manual configuration

For scripted or custom deployments:

1. Create `util/portable-update` under the application container root. Copy the built `portable-update.exe`, `LICENSE` and `THIRD-PARTY-LICENSES.txt` into it.
2. Copy the selected adapter from `adapters` into `util/portable-update/adapters`, and the JSON schemas into `util/portable-update/schemas`.
3. Copy the matching file from `examples` to `util/portable-update/update.json`. Adjust its name, runtime, channel and snapshot paths to the existing installation.
4. Copy both command files from [scripts/launchers](../scripts/launchers) into the container root. Keep the application's launch scripts there too, pointing at the active runtime.

```text
Application/
  updater-update.cmd
  updater-rollback.cmd
  runtime/                     existing application files; actual name is configurable
  data/                        external user data, if configured by the application
  util/portable-update/
    portable-update.exe
    LICENSE
    THIRD-PARTY-LICENSES.txt
    update.json
    adapters/
    schemas/
  tmp/portable-update/         generated when the updater runs
```

Before the first update, back up the existing runtime and user data. Confirm that the application starts and can load its data through its usual launcher. An example configuration describes paths; it does not move data or enable portable mode.

## Container configuration

The [instance schema](../schemas/instance.schema.json) defines configuration fields. Paths use the following bases:

| Field | Meaning |
| --- | --- |
| `name` | Display name used in updater output |
| `adapter` | Adapter JSON path relative to `util/portable-update` |
| `runtime` | Active runtime directory relative to the container root |
| `channel` | Release selection; see the provider behavior below |
| `saves` | Paths relative to the container root to snapshot before replacement; use `[]` when none are needed |

Keep snapshot paths narrow: save directories, profiles or configuration, rather than models or entire game libraries. Snapshots totaling more than 2 GiB stop the update before replacement.

Adapters define package selection, executable validation and runtime-local paths to preserve. `preserve`, `required_package` and `required_state` paths are relative to the runtime. See the [adapter schema](../schemas/adapter.schema.json) for the format and [conventions](conventions.md#extending-support) for authoring rules.

### Release channels

Use the matching example's channel unless a different track is needed. Accepted channel names do not imply identical behavior across services.

| Provider | `stable` | `nightly` | `latest` |
| --- | --- | --- | --- |
| GitHub | Service's latest release, excluding prereleases | Newest matching prerelease among the fetched releases | Newest matching release, including prereleases, among the fetched releases |
| Forgejo / Codeberg | Service's latest release, excluding prereleases | Newest matching prerelease among the fetched releases | Service's latest release; no prerelease filter |
| JSON feed: RPCS3 adapter | Official latest Windows build | Same feed | Same feed |
| JSON feed: Dolphin adapter | `beta` feed | `dev` feed | `beta` feed |

The RPCS3 and Dolphin mappings are declared in their adapters using the generic JSON-feed provider. When upgrading an existing updater to this format, replace its binary and selected adapter together; older provider names are no longer accepted. Keep the instance configuration and generated state.

### Adapter-specific setup

- **shadPS4:** the adapter targets the emulator core in the configured version directory. The launcher GUI is separate.
- **KoboldCpp:** use the existing `extracted` directory as the runtime and launch `extracted/koboldcpp-launcher.exe` from model scripts. Keep models, user configuration and server data outside that directory. The example snapshots no data and does not configure model paths. The adapter selects the standard CUDA executable.

## Update and verify

Close the application, then double-click `updater-update.cmd`. The updater checks the release, retains resumable downloads, validates a candidate, snapshots configured data and copies protected runtime-local settings before replacing the runtime.

Windows file-version adapters keep an equal or newer installed version. Receipt-based adapters compare release metadata and the installed entry-point hash; without a matching receipt, a check may download and prepare the package before deciding it is already current.

After a successful update, launch the application normally and verify the operation that matters for that installation: load and save a game, open a document, or start the intended model. Package validation does not establish application or data-format compatibility.

Successful command files close automatically after a short delay. Errors keep the console open. Details are in `tmp/portable-update/update.log`.

For direct invocation from the container root:

```cmd
util\portable-update\portable-update.exe
util\portable-update\portable-update.exe --rollback
```

`--manifest` selects another configuration file; it must still reside under a container's `util/portable-update` directory. Without that flag, the updater reads `update.json` beside its executable.

## Restore the previous runtime

Close the application and double-click `updater-rollback.cmd`. Rollback restores the previous runtime and its runtime-local settings from the update moment. External user data remains at its current state. The displaced runtime is retained under `tmp/portable-update` as a rejected copy.

Rollback requires an applied update and its retained previous runtime. It is not a version-history browser. Launch the restored application and verify that it can still read the current external data.

Save snapshots are separate backup copies; rollback does not restore them automatically. To recover one, close the application, back up the current affected data, find the snapshot named by `state/update-state.json`, and inspect its container-relative paths. Copy only the intended files back to their corresponding container paths, then verify them through the application. Preserve the current copy until recovery is confirmed.

## Storage and cleanup

| Location | Contents and lifetime |
| --- | --- |
| `update.json`, `adapters/`, `schemas/` under `util/portable-update` | Operator configuration and definitions; retain them with the updater |
| `util/portable-update/state/installed.json` | Generated receipt for release/hash checks |
| `util/portable-update/state/update-state.json` | Generated transaction journal used for recovery and rollback |
| `util/portable-update/updater.lock` | Persistent lock file; the OS releases its lock handle when the process exits, including after a crash |
| `tmp/portable-update/cache` | Downloads and retained ranges |
| Other paths under `tmp/portable-update` | Candidate, previous and rejected runtimes, unpacked packages, protected package defaults, snapshots, journal history and logs |
| Configured user-data paths | Active application data; ownership and location depend on the application configuration |

When the application and updater are closed and no interrupted transaction needs recovery, the temporary workspace can be removed to reclaim space. This also removes rollback copies and snapshots. Keep any backup copies you still need elsewhere first. There is no automatic retention cleanup.

Keep the journal and retained folders together during recovery. Do not remove the lock file to bypass another updater process.

## Integrity and execution

GitHub and Forgejo package selection normally requires a published SHA-256. The explicit `when-published` policy allows an absent publisher digest; HTTPS delivery, archive checks and candidate validation still apply. A locally recorded cache hash detects changes on reuse and does not authenticate the publisher. RPCS3 requires its official feed's checksum; the Dolphin adapter explicitly permits its feed to omit a package digest.

Archive packages use ZIP/7z extraction. The `executable-unpack` format requires a published SHA-256, then runs the downloaded Windows x64 executable with the adapter's argument list in a separate temporary directory. The `{destination}` placeholder is replaced with the updater-owned extraction path, and no shell is used. KoboldCpp supplies `--unpack` through its adapter. Its Python runtime is bundled. This runs upstream code with the updater process's permissions; the working directory is not an OS sandbox. The extracted package is validated before staging. A timeout stops the direct unpack executable; descendant-process termination is not guaranteed.

## Troubleshooting

| Symptom | Check and next step |
| --- | --- |
| Setup reports an existing updater after an interrupted setup | Inspect `util/portable-update` and the two root updater command files. Setup reuses matching files and creates missing ones. Different existing files are reported as conflicts. Preserve any existing configuration and inspect the named conflict before retrying. |
| An active process blocks replacement | Close the application, including background instances, then rerun the updater. It does not terminate them for you. |
| A required portable setting is missing | Check `required_state` in the selected adapter and confirm that portable mode was configured in the active runtime. Restore or configure the actual setting before retrying. |
| Download fails or is interrupted | Read the log and check network access. Rerun to reuse retained ranges; incomplete downloads do not replace the runtime. |
| No matching package, ambiguous package, or missing checksum | Check the adapter, channel and upstream release assets. Keep the current runtime; a changed upstream format may need an adapter or provider change. |
| Snapshot exceeds the size limit | Review `saves` for models, caches or whole libraries. Keep only the intended save/configuration paths. |
| Update or rollback was interrupted | Rerun the updater with the journal and temporary workspace intact. Recognizable swap states are recovered automatically. |
| Recovery reports an ambiguous layout | Stop and preserve the journal, log and all retained runtime folders. Compare their paths and contents before any manual restoration; deleting the journal does not resolve which copy is valid. |
| Rollback reports no applied update or a missing previous runtime | Inspect the journal and retained folders. A first run, completed rollback or removed temporary workspace may leave no automatic rollback available. Use a separate backup if needed. |

Updater self-updates and whole-collection update commands are not implemented. Recovery handles recognized directory states, not disk corruption or guaranteed recovery from every power-loss condition.
