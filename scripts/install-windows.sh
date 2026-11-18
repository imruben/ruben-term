#!/bin/sh
# From WSL: puts build/bin/ruben-term.exe in %LOCALAPPDATA%\Programs\ruben-term and adds
# a Start menu shortcut. `install-windows.sh remove` takes both out (your config stays).
set -e
PS=/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe
[ -x "$PS" ] || { echo "This runs from WSL (no $PS)"; exit 1; }
ps() { "$PS" -NoProfile -Command "$1" | tr -d '\r'; }

dir_win="$(ps '[Environment]::GetFolderPath("LocalApplicationData")')\\Programs\\ruben-term"
lnk_win="$(ps '[Environment]::GetFolderPath("Programs")')\\ruben-term.lnk"
dir=$(wslpath -u "$dir_win")
lnk=$(wslpath -u "$lnk_win")

# Windows does not let a running .exe be replaced or deleted (from WSL that shows as
# "Input/output error"), so both ways first check that ruben-term is closed.
running() {
  /mnt/c/Windows/System32/tasklist.exe /FI "IMAGENAME eq ruben-term.exe" /NH 2>/dev/null | grep -qi 'ruben-term.exe'
}
if running; then
  printf '%s\n' "ruben-term is running: close every ruben-term window and run it again."
  printf '%s\n' "(Run this from another terminal, not from inside ruben-term.)"
  exit 1
fi

if [ "$1" = remove ]; then
  rm -f "$lnk"
  rm -rf "$dir"
  # what the setup installer adds: its entry in Settings → Apps
  ps 'Remove-Item -Path "HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\ruben-term" -Recurse -ErrorAction SilentlyContinue'
  printf '%s\n' "Removed $dir_win and its Start menu shortcut. Your settings are kept."
  exit 0
fi

exe=$(dirname "$0")/../build/bin/ruben-term.exe
[ -f "$exe" ] || { echo "No build yet: make windows"; exit 1; }
mkdir -p "$dir"
cp "$exe" "$dir/ruben-term.exe" 2>/dev/null || {
  printf '%s\n' "Could not copy to $dir_win\\ruben-term.exe."
  exit 1
}
ps "\$s = (New-Object -ComObject WScript.Shell).CreateShortcut('$lnk_win'); \$s.TargetPath = '$dir_win\\ruben-term.exe'; \$s.WorkingDirectory = \$env:USERPROFILE; \$s.Description = 'ruben-term'; \$s.Save()"
printf '%s\n' "Installed: $dir_win\\ruben-term.exe"
echo "Start menu: ruben-term (right click → Pin to taskbar)"
