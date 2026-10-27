# Portable Updater

A Windows x64 updater for emulators in portable game folders. It finds an official release, downloads its portable package, preserves configured settings and saves, and retains the previous runtime for rollback.

Double-click `updater-update.cmd` to update or `updater-rollback.cmd` to restore the previous runtime. Running the updater needs no installed Go, Python, PowerShell or 7-Zip.

## Included adapters

| Emulator | Adapter | Container example |
| --- | --- | --- |
| DuckStation | [duckstation-windows.json](adapters/duckstation-windows.json) | [duckstation.json](examples/duckstation.json) |
| PCSX2 | [pcsx2-windows.json](adapters/pcsx2-windows.json) | [pcsx2.json](examples/pcsx2.json) |
| RPCS3 | [rpcs3-windows.json](adapters/rpcs3-windows.json) | [rpcs3.json](examples/rpcs3.json) |
| Cemu | [cemu-windows.json](adapters/cemu-windows.json) | [cemu.json](examples/cemu.json) |
| Xenia | [xenia-windows.json](adapters/xenia-windows.json) | [xenia.json](examples/xenia.json) |
| Dolphin | [dolphin-windows.json](adapters/dolphin-windows.json) | [dolphin.json](examples/dolphin.json) |
| shadPS4 | [shadps4-windows.json](adapters/shadps4-windows.json) | [shadps4.json](examples/shadps4.json) |
| DOSBox Pure Unleashed | [dosbox-pure-windows.json](adapters/dosbox-pure-windows.json) | [dosbox-pure.json](examples/dosbox-pure.json) |

The shadPS4 adapter updates the emulator core in the configured version directory. Its launcher GUI is separate.

## Set up a container

Start with an existing portable emulator installation in its own runtime directory. Enable its portable mode and configure its data paths before adding the updater.

1. Build the updater with Go 1.25 or later on Windows:

   ```cmd
   build.cmd
   ```

2. Copy `portable-update.exe` into the game's `util/portable-update` directory. Copy the selected adapter into its `adapters` subdirectory and the JSON schemas into its `schemas` subdirectory.
3. Copy the matching container example above to `util/portable-update/update.json`. Adjust `name`, `runtime`, `channel` and `saves` for that container. Paths in the examples describe sample layouts; they do not relocate existing data.
4. Copy both command files from [scripts/launchers](scripts/launchers) into the game root.

```text
Game/
  updater-update.cmd
  updater-rollback.cmd
  emulator/                    active runtime; name/path comes from update.json
  data/                        game data; paths depend on emulator configuration
  util/
    portable-update/
      portable-update.exe
      update.json
      adapters/
      schemas/
      state/                   installation receipt and recovery journal
  tmp/
    portable-update/           downloads, staging, snapshots, retained runtimes, logs
```

The **adapter** defines the release source, package selection, version policy, protected settings and required files for an emulator. The **container configuration** selects that adapter and supplies the runtime directory, release channel and save paths. `runtime` and `saves` are relative to the game root; `adapter` is relative to `util/portable-update`. Protected and required paths inside an adapter are relative to the runtime. The [schemas](schemas) describe the accepted fields.

For another emulator using an existing provider and version policy, add an adapter JSON file and a container configuration. A new release service or version policy requires a code change; JSON adapters do not execute shell commands or installers.

## Update and restore

Close the game and emulator, then double-click `updater-update.cmd`. Current installations are kept when the selected version or installation receipt matches. Otherwise the updater downloads and validates a package, prepares a candidate, snapshots configured saves and copies protected settings before replacing the runtime.

Downloads resume retained ranges after an interruption. Version checks use Windows executable versions or release/hash receipts, depending on the adapter; the first check for a receipt-based installation may download a package for comparison.

`updater-rollback.cmd` restores the previous runtime and its settings from the update moment. External saves retain their current progress. Check save/load compatibility after changing emulator versions.

Successful command files close automatically. Errors keep the console open. Details are in `tmp/portable-update/update.log`.

## Integrity and storage

Adapters normally require an upstream SHA-256. The explicit `when-published` policy permits packages without a published hash, using HTTPS delivery, archive integrity checks and executable validation. A locally recorded hash checks subsequent cache reads; it is not a publisher checksum.

Updater files and recovery metadata stay under `util/portable-update`. Working files and retained runtime copies stay under `tmp/portable-update`. Removing that temporary directory discards cached downloads, save snapshots and rollback copies; active saves remain in their configured game data paths. Keep the workspace while an update is running.

Save snapshots above 2 GiB stop installation. Snapshot paths should identify save/configuration data, rather than entire game libraries. Protected runtime-local files are also retained with the previous runtime.

Interrupted swaps with recognizable directory states are recovered on the next run. Recovery does not guarantee protection from disk corruption or power failure. Updater self-updates, automatic backup cleanup and a whole-collection update command are not implemented.

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for build/check commands and [architecture](docs/architecture.md) for package responsibilities.

## License

[MIT](LICENSE), copyright 2026 Innokentiy Sokolov. Dependency license notices are in [THIRD-PARTY-LICENSES.txt](THIRD-PARTY-LICENSES.txt).
