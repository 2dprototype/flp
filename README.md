# FLP Tool (Go)

A Go-native toolkit for FL Studio `.flp` project files: semantic diffing, inspection, visualisation, editing, git integration, and more.

> **Port credit:** This project is a Go port of [dawhubapp/flpdiff](https://github.com/dawhubapp/flpdiff). The original TypeScript implementation provided the semantic diff model, canonical projection, comparison logic, and much of the mutation API that this port faithfully reproduces. Huge thanks to the upstream authors.

Two programs ship together:

| Program | Type | Purpose |
| --- | --- | --- |
| **`flp`** | CLI | Diff, inspect, git setup/verify, and a JSON-RPC bridge for scripting. |
| **`flpgui`** | GUI | Desktop toolbox: visualiser, editor, browsers, MIDI export, and the same diff/inspect features with a window. |

---

## Features

- **Semantic diff** — compare two `.flp` projects and get a human-readable, hierarchical report of what changed: metadata, channels, patterns, notes, mixer inserts, arrangements, tracks, clips, automation keyframes, and opaque blobs.
- **Project inspector** — view any project in three formats:
  - `text` — a friendly summary
  - `canonical` — the deterministic, line-oriented `# flp canonical v1` representation
  - `json` — the full `FlpInfoJson` projection (mirrors the Python `flp-info` shape)
- **Visualiser** (GUI) — a scrollable, zoomable canvas with five views:
  - Arrangement (tracks, clips, beat grid)
  - Piano roll (per-pattern notes with a keyboard gutter)
  - Channels
  - Patterns (with note-density mini-histogram)
  - Mixer (inserts + effect slots)
- **Editor** (GUI) — in-memory editing of tempo, time signature, channels, patterns, mixer inserts, arrangements, and tracks. Save back to `.flp` with a byte-identical round-trip for untouched events.
- **Browsers** (GUI) — dedicated list/detail inspectors for channels, patterns, and mixer inserts.
- **MIDI export** (GUI) — export any pattern to a standard `.mid` file, one MIDI channel per FL channel.
- **Git integration** — one-click setup of `git diff` support for `.flp` files (local, global, or `textconv` mode) plus a verification report. Also available as a CLI command.
- **JSON-RPC bridge** (CLI) — drive the entire read/write API from any language by piping a single JSON object to stdin.

---

### Dependencies

| Module | Purpose |
| --- | --- |
| `github.com/gonutz/wui/v2` | Native Windows GUI (GUI only) |
| `github.com/mattn/go-colorable` | ANSI → Win32 console translation (CLI only) |
| `github.com/mattn/go-isatty` | Terminal detection (CLI only) |
| `gitlab.com/gomidi/midi/v2` | MIDI file writing (GUI only) |

---

## `flp` — command-line interface

```
flp [options] <A.flp> <B.flp>     Compare two projects
flp info <file.flp> [options]     Inspect a single project
flp git-setup [options]           Configure git's FLP diff driver
flp git-verify                    Check the current git setup
flp bridge                        JSON-RPC (one request on stdin)
```

### Diff

```sh
flp before.flp after.flp
flp -v mix-v1.flp mix-v2.flp       # show every clip change, not collapsed
flp --no-color before.flp after.flp
```

Exit codes are git-friendly: `0` if identical, `1` if they differ, `2` on error.

### Info

```sh
flp info song.flp                  # text summary
flp info song.flp -f json          # full FlpInfoJson
flp info song.flp -f canonical     # deterministic line-oriented form
```

### Git integration

```sh
flp git-setup                      # local scope, external-diff driver
flp git-setup --global             # all repos
flp git-setup --textconv           # use git's native textconv instead
flp git-setup --lfs                # also track *.flp with Git LFS
flp git-verify                     # diagnose what's configured
```

### Bridge

The bridge reads one JSON request from stdin and writes one JSON response to stdout. It is designed for scripting and language bindings.

```sh
echo '{"kind":"get_tempo","args":{"path":"song.flp"}}' | flp bridge
```

Read kinds include `describe`, `get_tempo`, `list_channels`, `list_mixer`, `list_patterns`, `list_plugins`, `list_arrangements`, `list_tracks`, `list_clips`, and the `find_*` lookups. Write kinds cover tempo, names, colors, routing, notes, controllers, patterns, channels, mixer slots, plugin params, clip edits, song arrangement, plugin instantiation, and more — see `bridge.go` for the full list.

### Environment

| Variable | Effect |
| --- | --- |
| `NO_COLOR` | If set and non-empty, disables ANSI colours |
| `TERM=dumb` | Also disables ANSI colours |

Colours are auto-disabled when stdout is not a TTY, so piping into a file or another program is safe.

### Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Projects identical (diff), or command succeeded |
| `1` | Projects differ (diff) |
| `2` | Invalid arguments, parse error, or I/O error |

---

## `flpgui` — desktop GUI

```
flpgui                 # open with no project loaded
flpgui path/to/x.flp   # open with that project preloaded
flpgui --help          # show usage
```

If a path is given on the command line and it parses successfully, the window opens with that project already loaded — all tools are immediately available without going through the file picker.

### Dashboard

Once a project is loaded the dashboard shows:

- Project file name
- Tempo, PPQ
- Channel / pattern / mixer-insert / arrangement counts

From the sidebar you can launch any of the built-in tools:

- Visualizer
- Edit Project
- Diff Two Projects
- Inspect JSON/Canon
- Channel Browser
- Pattern Browser
- Mixer Browser
- Git Integration
- About