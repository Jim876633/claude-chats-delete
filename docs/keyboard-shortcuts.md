# Keyboard Shortcuts

> You can also press `?` inside the TUI to open this reference as an overlay.

## Navigation

| Key | Action |
|-----|--------|
| `↑` / `k` | Move cursor up |
| `↓` / `j` | Move cursor down |
| `f` / `PgDn` | Page down |
| `b` / `PgUp` | Page up |
| `F` | Half-page down |
| `B` | Half-page up |
| `g` / `Home` | Jump to top |
| `G` / `End` | Jump to bottom |

## Selection

| Key | Action |
|-----|--------|
| `Space` | Toggle selection for current item |
| `a` | Select / deselect all |

## Actions

| Key | Action |
|-----|--------|
| `d` | Delete selected chats (auto-selects cursor item if nothing selected). In grouped view, pressing `d` on a project header auto-selects every chat in that project. |
| `p` | Open preview modal for the chat under cursor |
| `c` | Copy current chat UUID to clipboard |
| `r` | Refresh chat list (reload from disk) |
| `m` | Toggle grouped-by-project mode |
| `?` | Show keyboard shortcut overlay |
| `q` / `Ctrl+C` | Quit |

## Grouped Mode

| Key | Action |
|-----|--------|
| `Enter` | Expand / collapse project |
| `e` | Expand all projects |
| `w` | Collapse all projects |

## Delete Confirmation

| Key | Action |
|-----|--------|
| `Enter` | Confirm deletion |
| `Esc` / `n` | Cancel (auto-selections made by `d` are reverted; explicit `Space` selections are preserved) |

## Preview Modal

| Key | Action |
|-----|--------|
| `↑/↓` `k/j` | Scroll content |
| `f/b` | Page down / up |
| `g/G` | Jump to top / bottom |
| Any other key | Close preview |

## Bulk Delete Workflow

1. Press `a` to select all chats.
2. Use `Space` to deselect chats you want to keep.
3. Press `d` then `Enter` to delete.

## Scroll Indicator

When the list is longer than the visible area, a scroll indicator shows:
```
[1-20/150]
```
