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

if [ "$1" = remove ]; then
  rm -f "$lnk"
  rm -rf "$dir"
  printf '%s\n' "Removed $dir_win and its Start menu shortcut."
  exit 0
fi

exe=$(dirname "$0")/../build/bin/ruben-term.exe
[ -f "$exe" ] || { echo "No build yet: make windows"; exit 1; }
mkdir -p "$dir"
cp "$exe" "$dir/ruben-term.exe" 2>/dev/null || {
  printf '%s\n' "Could not replace $dir_win\\ruben-term.exe: close ruben-term and run it again."
  exit 1
}
ps "\$s = (New-Object -ComObject WScript.Shell).CreateShortcut('$lnk_win'); \$s.TargetPath = '$dir_win\\ruben-term.exe'; \$s.WorkingDirectory = \$env:USERPROFILE; \$s.Description = 'ruben-term'; \$s.Save()"
printf '%s\n' "Installed: $dir_win\\ruben-term.exe"
echo "Start menu: ruben-term (right click → Pin to taskbar)"
