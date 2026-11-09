# foveatedsway

Ctrl-Z (SIGSTOP) applications in sway when the workspace hasn't been accessed in a while, and resume (SIGCONT) when it becomes visible again. Inspired by Firefox draining battery and fan noise in the background.

Accounts for:
- Inhibitors
- Multiple windows per single processes
- Sticky windows

## Environment variables
```env
IGNORE_APP_IDS=foot,thunar
WORKSPACE_TIMEOUT=2m # default 90s
GO_LOG=debug # debug, [info], warn, error
```

## Installation
```sh
go install github.com/jtagcat/foveatedsway@latest
```

## Usage
```sh
exec env IGNORE_APP_IDS=foot,thunar foveatedsway
```

## TODO
- More aggressively resuspend, if window/workspace is accessed only briefly.
- Indicate window is suspended (needs saving state for borders)
