# Claude Code Chats Delete TUI

**Browse and delete Claude Code chat sessions** with an interactive terminal UI.

Forked from [ataleckij/claude-chats-delete](https://github.com/ataleckij/claude-chats-delete).

Browse, select, and bulk delete chat histories stored in your `~/.claude` directory.

<img src="./demo.gif" />

## Features

- Browse chat sessions across all projects
- Toggle grouped-by-project view with collapsible project headers
- Preview chat content in a modal overlay
- Bulk delete with full on-disk cleanup (subagents, tool-results, file-history, todos, tasks, plans, agent memory, and more)
- Copy chat UUID to clipboard
- Full keyboard shortcut reference overlay (`?`)
- Keyboard-driven interface with vim keys and fast page navigation

## Installation

### Build from Source

Requires Go 1.24+.

```bash
git clone https://github.com/Jim876633/claude-chats-delete.git
cd claude-chats-delete
make install
```

This builds the binary as `c` and installs it to `~/.local/bin/c`.

Make sure `~/.local/bin` is in your `PATH`:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

### Other make targets

```bash
make build     # build binary to ./c
make uninstall # remove ~/.local/bin/c
```

## Usage

```bash
c
```

On first run you'll be prompted to specify your Claude directory. Configuration is saved to `~/.config/claude-chats/config.json`.

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `↑/↓` `k/j` | Move cursor |
| `f/b` | Page down / up |
| `F/B` | Half-page down / up |
| `g/G` | Jump to top / bottom |
| `Space` | Select / deselect current item |
| `a` | Select / deselect all |
| `d` | Delete selected (auto-selects cursor item if nothing selected) |
| `p` | Preview chat content |
| `c` | Copy chat UUID to clipboard |
| `r` | Refresh chat list |
| `m` | Toggle grouped-by-project mode |
| `?` | Show full keyboard shortcut reference |
| `q` / `Ctrl+C` | Quit |

**Grouped mode only:**

| Key | Action |
|-----|--------|
| `Enter` | Expand / collapse project |
| `e` | Expand all projects |
| `w` | Collapse all projects |

See [docs/deletion-behavior.md](docs/deletion-behavior.md) for what gets deleted per chat.

## License

[MIT](LICENSE)
