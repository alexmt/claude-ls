# claude-ls

A terminal UI for managing Claude Code sessions.

![demo](https://github.com/user-attachments/assets/78d747d2-5846-485e-8cac-ee142dd43b8c)


## The Problem

Claude Code links sessions to directory paths — `~/.claude/projects/` uses encoded directory names (e.g. `-Users-alex-root-myproject`). This creates three friction points:

1. **Rename a directory** → sessions become orphaned, silently detached from the project
2. **Switch between projects** → no quick way to see what you were working on across all of them
3. **No session overview** → finding a specific conversation means digging through UUIDs and raw JSONL

## Solution

`claude-ls` reads `~/.claude/projects/` directly — no extra files stored. It gives you a fast interactive view of all your sessions, lets you preview conversations, surface orphaned sessions from renamed directories, and resume any session with one keypress.

## Installation

**Homebrew (recommended):**
```bash
brew tap alexmt/tap
brew install claude-ls
```

**Build from source:**
```bash
git clone https://github.com/alexmt/claude-ls.git
cd claude-ls
make build
# binary is at dist/claude-ls
```

## Usage

```
claude-ls    # open interactive TUI
```

## Interactive TUI

Running `claude-ls` opens a split-pane terminal UI:

```
┌─ claude-ls ────────────────────────────────┬─ preview ──────────────────────────┐
│ » auth-middleware-refactor  2h ago  42     │ auth-middleware-refactor           │
│   ~/root/myproject                         │ ~/root/myproject  •  2 hours ago   │
│ » obsidian-sync-debug       1d ago  18     │ ───────────────────────────────────│
│   ~/root/other                             │ Claude: I'll create tests in...    │
│ ────────────────────────────────────────── │                                    │
│ > hopeful-coding-turing     3d ago  91     │ You: looks good, now write tests   │
│   ~/root/personal                          │                                    │
│   lazy-morning-knuth         5d ago  7     │ Claude: Sure. The current impl in  │
│   ~/root/akuity                            │ middleware/auth.go:47 uses...      │
│ ✗ wandering-fox             1w ago  33     │ [tool: Read middleware/auth.go]    │
│   ~/root/old-name (gone)                   │                                    │
│                                            │ You: can you refactor the auth     │
│                                            │ middleware to use the new token    │
│                                            │ validator?                         │
└────────────────────────────────────────────┴────────────────────────────────────┘
 enter resume  r rename  m move  d delete  g/G top/bottom  tab focus  q quit
```

**Keybindings:**

| Key | Action |
|-----|--------|
| `↑ / ↓` or `j / k` | Navigate session list |
| `g / G` | Jump to top / bottom of list |
| `/` | Search sessions by title and last message |
| `enter` | Resume selected session in Claude |
| `r` | Rename selected session (inline input, supports spaces) |
| `m` | Move session to another project (picker with live filter) |
| `d` | Delete selected session (confirmation required) |
| `tab` | Switch focus between list and preview pane |
| `j / k` | Scroll preview pane (when focused) |
| `s` | Open settings |
| `q` | Quit |

**Settings** — `[s]` opens a settings overlay in the preview pane. Currently one option:

- **Dangerously skip permissions** — when enabled, `--dangerously-skip-permissions` is passed to `claude --resume` every time you open a session. Toggle with `enter` or `space`. Settings are saved to `~/.config/claude-ls/settings.json`.

**Search** — `[/]` enters search mode. Type to filter by session title or last message snippet; matches are highlighted inline. The status bar shows the query and result count. `↑/↓` navigate filtered results, `enter` resumes, `esc` exits and returns the cursor to the full list.

**Named sessions** — sessions renamed with `[r]` — sort to the top, marked with `»`. The name is written directly to the session JSONL file as a `custom-title` entry (same format Claude Code uses internally). No extra files stored by claude-ls.

**Move** — `[m]` opens a project picker in the preview pane. Type to filter by path, `↑/↓` to navigate, `enter` to move. Moves the session JSONL and subagent directory to the target project.

**Orphaned sessions** — sessions whose original project directory no longer exists — are shown with a `✗` marker and dimmed path. They are still resumable.

**Preview pane** — shows the conversation tail (most recent messages first). Switch focus with `tab`, scroll with `j / k`.

## Data Model

`claude-ls` reads everything from `~/.claude/projects/` with no extra state:

| Field | Source |
|-------|--------|
| Project path | Directory name decoded (`-Users-alex-root-myproject` → `/Users/alex/root/myproject`) |
| Session ID | JSONL filename (UUID) |
| Slug | `slug` field in JSONL |
| Custom name | `customTitle` field in `custom-title` entries (written by `/rename` or `claude-ls`) |
| First message | `~/.claude/history.jsonl` `display` field |
| Last active | Timestamp of last JSONL entry |
| Orphaned | Project path does not exist on disk |

### `~/.claude/projects/` structure

```
~/.claude/projects/
└── -Users-alex-root-myproject/         # encoded project path
    ├── c7b5480d-...-3d4ef5.jsonl       # session (one per conversation)
    ├── a1b2c3d4-...-ffffff.jsonl
    └── a1b2c3d4-.../                   # subagent data for that session
        ├── subagents/
        └── tool-results/
```

Each session JSONL is newline-delimited JSON. Relevant entry types:

```json
{ "type": "user",         "slug": "tender-seeking-milner", "timestamp": "...", "message": { "role": "user", "content": "..." } }
{ "type": "assistant",    "slug": "tender-seeking-milner", "timestamp": "...", "message": { "role": "assistant", "content": [...] } }
{ "type": "custom-title", "customTitle": "my session name", "sessionId": "..." }
```

## Stack

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — TUI framework
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) — terminal styling

## Build

```
make build    # outputs dist/claude-ls
```
