# Install from Source

## Requirements

- Go 1.24 or later
- git
- make

## Steps

```bash
git clone https://github.com/Jim876633/claude-chats-delete.git
cd claude-chats-delete

# Build and install to ~/.local/bin/c
make install

# Make sure ~/.local/bin is in your PATH
export PATH="$HOME/.local/bin:$PATH"
```

## Other targets

```bash
make build     # build binary to ./c (without installing)
make uninstall # remove ~/.local/bin/c
```
