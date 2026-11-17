# ruben-term

A terminal app for Windows and macOS, built for people who write code all day.

It does what a normal terminal does (tabs, copy/paste, themes), and adds the things
you usually keep in other windows: what's running on which port, your SSH servers,
your projects, and one-click buttons to start tools like Claude or opencode in the
folder you're working in.

It's a single small program (about 13 MB), not an Electron app.

## Features

**Tabs and split panes.** Open as many tabs as you like, and split any of them into
side-by-side or stacked panes. New tabs and panes open in the folder you're already
in, and each tab is named after its folder or the program running in it.

**One-click launchers.** Buttons at the top right start a program in a new tab, in
the current folder. By default they are `Claude` and `opencode`, and you can change
them to any command, such as `pnpm dev` or `ssh myserver`.

**Command palette.** Press `Ctrl+Shift+P` (`⌘K` on Mac) and type to find any tab,
project, server, port, launcher or action.

**Side panels:**
- **Ports** lists every program listening on a port, which project it belongs to, and
  the Docker container behind it. Click a port to open it in the browser, or stop the
  program.
- **SSH** lists your servers. Click one to connect in a new tab.
- **Projects** lists the folders you work in, with git repos first. Click one to open a
  tab there, or start it straight away with a launcher.

**Picks up where you left off.** Close the app and reopen it to get the same tabs,
panes and folders back.

**Works with WSL.** On Windows you can open any WSL distro, PowerShell or cmd. `code .`
and `open` work from inside WSL.

**Git shortcuts built in.** Short commands like `gst`, `gco`, `gp` and `gpr`. Type
`ghelp` to see the full list.

**Settings screen.** Choose your font (from the fonts installed on your computer),
theme, default shell, launchers, servers and project folders without editing any file.
You can open it with `Ctrl+,` or the gear icon.

**The usual terminal features** are there too: find in scrollback, clickable links,
zoom, light and dark themes, and a dot on any tab that wants your attention.

## Main shortcuts

| | Windows | macOS |
|---|---|---|
| New tab | `Ctrl+Shift+T` | `⌘T` |
| Split right / down | `Ctrl+Shift+D` / `Ctrl+Shift+E` | `⌘D` / `⌘⇧D` |
| Move between panes | `Ctrl+Alt+arrows` | `⌘⌥arrows` |
| Close pane | `Ctrl+Shift+W` | `⌘W` |
| Command palette | `Ctrl+Shift+P` | `⌘K` |
| Ports / SSH / Projects panel | `Ctrl+Shift+1` / `2` / `3` | `⌃1` / `⌃2` / `⌃3` |
| Find | `Ctrl+Shift+F` | `⌘F` |
| Settings | `Ctrl+,` | `⌘,` |

Any other key combination goes straight to the shell, so `Ctrl+C`, `Ctrl+R` and the
rest behave as usual.

## Your shell setup is left alone

ruben-term runs its own zsh configuration, which lives in `~/.config/ruben-term`.
It reads your own settings from `~/.zshrc` (PATH, aliases and so on) but never
changes that file, and it doesn't affect any other terminal you use. To add something
only for ruben-term, put it in `~/.config/ruben-term/zsh/local.zsh`.

## Install

**Windows.** From WSL, with Go installed:

```bash
make tools      # once
make install    # installs it and adds it to the Start menu
```

**macOS.** With Go and the Xcode command line tools installed:

```bash
make tools
make mac        # then drag build/bin/ruben-term.app to Applications
```

Ready-made builds are also produced by GitHub Actions. They aren't signed, so on
macOS you have to right-click the app and choose Open the first time you run it.
The macOS version hasn't been tested on a real Mac yet.

## For developers

The app is written in Go with [Wails](https://wails.io). The terminal itself is
[xterm.js](https://xtermjs.org), drawn in the system's own web view (WebView2 on
Windows, WKWebView on macOS). Run the tests with `make test`.
