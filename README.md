# claude-ls

A terminal UI for managing Claude Code sessions.

## The Problem

Claude Code links sessions to directory paths — `~/.claude/projects/` uses encoded directory names (e.g. `-Users-alex-root-myproject`). This creates three friction points:

1. **Rename a directory** → sessions become orphaned, silently detached from the project
2. **Switch between projects** → no quick way to see what you were working on across all of them
3. **No session overview** → finding a specific conversation means digging through UUIDs and raw JSONL

## Solution

`claude-ls` reads `~/.claude/projects/` directly. It gives you a fast interactive view of all your sessions, lets you preview conversations, surface orphaned sessions from renamed directories, and resume any session with one keypress. The only extra state it stores is session names you explicitly set, in `~/.claude/claude-ls.json`.

## Usage

```
claude-ls              # open interactive TUI (primary mode)
claude-ls orphaned     # list sessions whose project directory no longer exists
claude-ls search <q>   # search across session first messages
claude-ls resume <id>  # resume a session by slug or UUID
```

## Interactive TUI

Running `claude-ls` opens a split-pane terminal UI:

```
┌─ claude-ls ────────────────────────────────┬─ preview ──────────────────────────┐
│ » auth-middleware-refactor  2h ago  42 │ hopeful-coding-turing               │
│   ~/root/myproject                     │ ~/root/personal  •  3 days ago      │
│ » obsidian-sync-debug       1d ago  18 │ ─────────────────────────────────── │
│   ~/root/other                         │ You: can you refactor the auth      │
│ ────────────────────────────────────── │ middleware to use the new token      │
│ > hopeful-coding-turing     3d ago  91 │ validator?                          │
│   ~/root/personal                      │                                     │
│   lazy-morning-knuth         5d ago  7 │ Claude: Sure. The current impl in   │
│   ~/root/akuity                        │ middleware/auth.go:47 uses...        │
│ ✗ wandering-fox             1w ago  33 │ [tool: Read middleware/auth.go]      │
│   ~/root/old-name (gone)               │                                     │
│                                        │ You: looks good, now write tests    │
│                                        │                                     │
│                                        │ Claude: I'll create tests in...     │
└────────────────────────────────────────┴─────────────────────────────────────┘
 [enter] resume  [r] rename  [/] search  [o] orphaned  [tab] focus  [q] quit
```

**Keybindings:**

| Key | Action |
|-----|--------|
| `↑ / ↓` | Navigate session list |
| `enter` | Resume selected session in Claude |
| `r` | Rename selected session (opens inline input) |
| `/` | Search sessions |
| `o` | Toggle showing orphaned sessions only |
| `tab` | Switch focus between list and preview pane |
| `j / k` | Scroll preview pane (when focused) |
| `q` | Quit |

**Named sessions** — sessions whose slug doesn't match the auto-generated `adj-adj-name` pattern — sort to the top, marked with `»`. Rename with `[r]`: claude-ls forks `claude -p --resume <id> '/rename <new-name>'` and lets Claude update its own data. No extra files stored by claude-ls.

**Orphaned sessions** — sessions whose original project directory no longer exists — are shown with a ✗ marker. They are still resumable.

## Data Model

`claude-ls` derives everything from `~/.claude/projects/`:

| Field | Source |
|-------|--------|
| Project path | Directory name (decoded from `-Users-alex-root-myproject` → `/Users/alex/root/myproject`) |
| Session ID | JSONL filename (UUID) |
| Slug | `slug` field in JSONL (e.g. `tender-seeking-milner`) |
| First message | `~/.claude/history.jsonl` `display` field |
| Last active | Last entry timestamp in session JSONL |
| Message count | Line count of session JSONL |
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

Each session JSONL is newline-delimited JSON. Relevant fields per line:

```json
{
  "type": "user" | "assistant" | "system" | ...,
  "uuid": "...",
  "sessionId": "...",
  "timestamp": "2026-04-05T01:39:13.414Z",
  "slug": "tender-seeking-milner",
  "cwd": "/Users/alex/root/myproject",
  "message": { "role": "user", "content": "..." }
}
```

## Stack

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — TUI framework
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) — terminal styling
- [Bubbles](https://github.com/charmbracelet/bubbles) — list + viewport components

## Status

Early development. Planned build order:

- [ ] JSONL parser + data model
- [ ] CLI skeleton (`orphaned`, `search`, `resume`)
- [ ] TUI layout (split pane, navigation)
- [ ] Session renaming (`~/.claude/claude-ls.json`)
