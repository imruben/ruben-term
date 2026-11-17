# ruben-term

A small terminal for Windows and macOS, made for development work: tabs, split panes,
a command palette, and panels for what is listening on which port, your SSH hosts and
your projects. Claude, opencode or anything else opens in one click, in the folder you
are in.

One 13 MB program. No Electron: the window is the system's own web view (WebView2 on
Windows, WKWebView on macOS), the terminal is [xterm.js](https://xtermjs.org), and the
rest is Go ([Wails](https://wails.io)).

## What it does

| | |
|---|---|
| **Tabs** | A new tab opens in the folder of the pane you are in. The tab is named after the folder, or the program that is running. |
| **Panes** | Split a tab in two, and each half again, as many times as you want: `Ctrl+Shift+D` to the right, `Ctrl+Shift+E` below. The new pane opens in the same folder. Drag the line between two panes to resize them (double click: equal), `Ctrl+Alt+arrows` to move between them, `Ctrl+Shift+Enter` to make one fill the tab and back. The tab shows how many panes it has. |
| **Launchers** | The buttons at the top right (`Claude`, `opencode`: yours to change). Each opens a new tab **in the current folder** and runs its command; when the program ends you are left in the shell. |
| **Command palette** | `Ctrl+Shift+P` (`⌘K`). Type to find a launcher, an SSH host, a project, a port or an action. On a project, `Shift+Enter` opens it with the first launcher. |
| **Ports** | Its own panel (`Ctrl+Shift+1`). What is listening, which program, and **which project it runs in**; ports published by Docker show the container. Click to open `http://localhost:<port>`, `stop` to end the program (asks once; `Shift`+click is `kill -9`). |
| **SSH** | Its own panel (`Ctrl+Shift+2`). The servers you added in Settings, then the hosts of `~/.ssh/config`. Click: `ssh` to it in a new tab. |
| **Projects** | Its own panel (`Ctrl+Shift+3`). One group per project folder of Settings, git repos first; click a group's name to fold it. Type to filter (`Enter` opens the first match; a group's name shows only that group). Click a project: a tab there. Hover: open it with a launcher. |
| **Git shortcuts** | `gst`, `gl`, `gp`, `gco`, `gpr`, `gprc`, `gci`… the same names as in terminal-setup. `ghelp` lists them. |
| **Shells** | Windows: every WSL distro, PowerShell and cmd (the `⌄` next to `+`). macOS: your login shell. The panels look at the machine of the pane you are in: WSL ports in a WSL tab, Windows ports in a PowerShell tab. |
| **Settings** | `Ctrl+,` (`⌘,`) or the gear: project folders (with a folder picker), servers, launchers, theme, default shell, and the font: a list of the fonts on your computer, monospaced first, with a preview. Saved to `config.json`, so a new computer is set up from the app. |
| **Also** | `code .` and `open <file|folder|url>` from WSL, find in the scrollback, clickable links, copy/paste, zoom, dark and light themes, a dot on a tab that rang the bell while you were elsewhere. |

## Keys

| | Windows | macOS |
|---|---|---|
| New tab | `Ctrl+Shift+T` | `⌘T` |
| Split right · split down | `Ctrl+Shift+D` · `Ctrl+Shift+E` | `⌘D` · `⌘⇧D` |
| Move to the pane on that side | `Ctrl+Alt+←↑↓→` | `⌘⌥←↑↓→` (`⌘[` · `⌘]`: previous · next) |
| Pane over the whole tab, and back | `Ctrl+Shift+Enter` | `⌘⇧Enter` |
| Close the pane (the tab, when it is the last one) | `Ctrl+Shift+W` | `⌘W` |
| Next · previous tab | `Ctrl+Tab` · `Ctrl+Shift+Tab` | same, or `⌘⇧]` · `⌘⇧[` |
| Tab 1…9 | `Alt+1`…`9` | `⌘1`…`9` |
| Command palette | `Ctrl+Shift+P` | `⌘K` or `⌘⇧P` |
| Ports · SSH · Projects | `Ctrl+Shift+1` · `2` · `3` | `⌃1` · `⌃2` · `⌃3` |
| Close the panel / open the last one | `Ctrl+Shift+B` | `⌘B` |
| Find | `Ctrl+Shift+F` | `⌘F` |
| Copy · paste | `Ctrl+Shift+C` · `Ctrl+Shift+V`; also `Ctrl+C` with a selection, `Ctrl+V`, and right click | `⌘C` · `⌘V` |
| Bigger · smaller · normal text | `Ctrl++` · `Ctrl+-` · `Ctrl+0` | `⌘+` · `⌘-` · `⌘0` |
| Settings | `Ctrl+,` (`Ctrl+S` saves) | `⌘,` (`⌘S` saves) |

Everything else goes to the shell: `Ctrl+C`, `Ctrl+R`, `Ctrl+W`, `Alt+…` work as always.

## It does not share anything with terminal-setup

[terminal-setup](../terminal-setup) configures zsh through `~/.zshrc`, so every
terminal that starts zsh gets it (tmux, the monitor, its prompt). ruben-term does not:

- It starts zsh with `ZDOTDIR=~/.config/ruben-term/zsh`, so zsh reads **that folder's**
  `.zshrc` and never `~/.zshrc` by itself. Windows Terminal is not touched and keeps
  working exactly as before; nothing in `terminal-setup` or in your `~/.zshrc` changes.
- From your `~/.zshrc` it takes only **your own lines** (PATH, nvm, Go, aliases): the
  block between `# >>> terminal-setup >>>` and `# <<< terminal-setup <<<` is skipped.
  That is why `claude`, `opencode` and `node` are found but there is no tmux, no
  starship and none of terminal-setup's functions.
  With `"loadUserRc": false` not even that is read: a completely bare shell.
- It has its own history (`~/.config/ruben-term/zsh/history`), its own prompt (folder,
  git branch, `❯` that turns red after a failed command) and its own completion cache.
- The git shortcuts (`zsh/git.zsh`) are ruben-term's **own copy** of the ones in
  terminal-setup: the same names and behaviour, nothing read from that repo.

Files in `~/.config/ruben-term` (inside WSL on Windows):

| File | |
|---|---|
| `launch.sh`, `zsh/.zshrc`, `zsh/.zshenv`, `zsh/.zprofile`, `zsh/git.zsh`, `bash/bashrc` | Written by the app each time it starts (from [`shell/`](shell)): edit them in the repo, not there. |
| `zsh/local.zsh` (or `bash/local.bash`) | **Yours**: aliases, exports and anything else only for ruben-term. Loaded last. |
| `zsh/history` | Its history. |

zsh-autosuggestions, zsh-syntax-highlighting, fzf and zoxide are used when they are
installed on the system; none is required. The pickers of `gco` and `gpr` use fzf, the
`gpr…` and `gci` shortcuts need [gh](https://cli.github.com), and diffs go through
delta when it is installed and git has no pager of its own.

**WSL and Windows programs.** With `appendWindowsPath=false` in `/etc/wsl.conf`,
Windows programs are not on `PATH` inside WSL, so `code` is not found by itself.
ruben-term's zsh defines `code` (VS Code, found under `/mnt/c` on first use) and `open`
(Explorer, the default program or the browser). For another Windows program, add a
function to `zsh/local.zsh`.

## Config

Everything here can be changed in **Settings** (`Ctrl+,`), which writes this file. To
edit it by hand: *Open the config file* in the palette, then *Reload the config*.

- Windows: `%AppData%\ruben-term\config.json`
- macOS: `~/.config/ruben-term/config.json`

```jsonc
{
  "fontFamily": "\"Cascadia Code\", \"JetBrainsMono Nerd Font\", monospace",
  "fontSize": 13,
  "lineHeight": 1.2,
  "theme": "dark",                    // dark · light · system
  "colors": { "cursor": "#ff9e64" },  // any xterm.js theme colour: background, red, brightBlue…
  "scrollback": 10000,
  "cursorBlink": false,
  "copyOnSelect": false,
  "defaultProfile": "",               // "Ubuntu-24.04", "powershell", "cmd" · empty: the first one
  "loadUserRc": true,                 // read your ~/.zshrc (never its terminal-setup block)
  "launchers": [                      // the first three are buttons, all are in the palette
    { "name": "Claude", "command": "claude" },
    { "name": "opencode", "command": "opencode" },
    { "name": "Claude (continue)", "command": "claude --continue" },
    { "name": "Dev server", "command": "pnpm dev" }
  ],
  "projectRoots": ["~/dev/projects"], // each one is a group in Projects (empty on a new install)
  "panel": "",                        // the panel to start with: ports · ssh · projects · none
  "servers": [                        // SSH servers kept here, not in ~/.ssh/config
    { "name": "vps", "hostName": "203.0.113.7", "user": "deploy", "port": "2222", "identity": "~/.ssh/id_ed25519" }
  ]
}
```

A launcher is any command line, so a server you enter often can be one too:
`{ "name": "vps logs", "command": "ssh vps -t 'docker logs -f app'" }`.

## Install

**Windows** (from WSL, with Go installed):

```bash
make tools      # once: the wails command
make install    # builds, copies the .exe to %LOCALAPPDATA%\Programs\ruben-term, adds it to the Start menu
```

It needs the WebView2 runtime, which Windows 11 includes. `make uninstall` removes the
program and the shortcut.

**macOS** (on the Mac, with Go and the Xcode command line tools):

```bash
make tools
make mac        # build/bin/ruben-term.app → drag it to Applications
```

Or take the builds from GitHub Actions (`.github/workflows/build.yml` builds both on
every push). They are not signed: on macOS, the first time, right click → Open (or
`xattr -dr com.apple.quarantine ruben-term.app`).

> The macOS build has not been run on a real Mac yet. What it shares with Windows and
> Linux is tested (the shells, the panels, the parsers of `lsof`); the window itself
> (the traffic lights inside the tab strip, `⌘` keys) is the part to look at first.

## Layout

```
main.go          the window (Wails options)
app.go           what the window can call: sessions, ports, ssh, projects, config
profile.go       the shells of this machine; how to run things inside WSL
session.go       one pty per tab, with flow control
ports.go         listeners: ss (Linux), lsof (macOS), netstat (Windows), docker ps
devinfo.go       ~/.ssh/config and the project folders
config.go        config.json (read, checked, saved by Settings) and the window size
fonts.go         the fonts installed on the computer (Settings → Font)
shell/           what ruben-term's zsh and bash load (installed in ~/.config/ruben-term);
                 git.zsh: the git and GitHub shortcuts
frontend/        index.html, style.css, app.js (tabs, panes, panels, palette): no build step.
                 vendor/ is xterm.js as published
scripts/         the Windows install
```

## Testing

```bash
make test                                        # go vet (Linux + Windows) and the unit tests
RT_REAL=1 go test -run TestRealShell -v .        # a real tab against your own home
```

The window can drive itself, to check it without hands: `RUBEN_TERM_AUTORUN` holds
steps separated by ` ;; `, each an action (`new`, `close`, `splitRight`, `splitDown`,
`focusLeft`, `zoom`, `ports`, `ssh`, `projects`, `palette`, `find`…) or `sleep:<ms>`,
`type:<text>` (`\r` is Enter), `launch:<n>`, `project:<name>`, `profile:<id>`,
`palette:<text>`, `filter:<text>`. From WSL:

```bash
RUBEN_TERM_AUTORUN='projects ;; sleep:2000 ;; project:gilito ;; splitRight ;; launch:0' \
WSLENV=RUBEN_TERM_AUTORUN/w ./build/bin/ruben-term.exe
```

## Not there yet

Restoring the tabs and panes of the last run, dragging tabs to reorder them, and moving a
pane to another place.
