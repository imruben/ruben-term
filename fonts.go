package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// installedFonts asks the system for its font families: PowerShell on Windows,
// AppKit (through osascript) on macOS, fontconfig elsewhere.
func installedFonts() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		ps := lookPath("powershell.exe")
		if ps == "" {
			ps = filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
		}
		cmd = exec.CommandContext(ctx, ps, "-NoProfile", "-NonInteractive", "-Command",
			"[Console]::OutputEncoding=[Text.Encoding]::UTF8; Add-Type -AssemblyName System.Drawing; "+
				"(New-Object System.Drawing.Text.InstalledFontCollection).Families | ForEach-Object Name")
	case "darwin":
		cmd = exec.CommandContext(ctx, "osascript", "-l", "JavaScript", "-e",
			"ObjC.import('AppKit'); ObjC.deepUnwrap($.NSFontManager.sharedFontManager.availableFontFamilies).join('\\n')")
	default:
		cmd = exec.CommandContext(ctx, "fc-list", ":", "family")
	}
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return fontFamilies(string(out)), nil
}

// weights are the words GDI adds to a family name for each weight ("Cascadia Code
// Light"); the web view knows those as weights of the family itself.
var weights = []string{"thin", "extralight", "ultralight", "light", "semilight", "regular", "book",
	"medium", "semibold", "demibold", "bold", "extrabold", "ultrabold", "black", "heavy",
	"extra light", "semi light", "semi bold", "extra bold"}

// fontFamilies cleans the list: one name per family (fontconfig gives "A,B" for the
// names of one family in several languages), weights folded into their family, sorted.
func fontFamilies(out string) []string {
	seen := map[string]bool{}
	var names []string
	for line := range strings.SplitSeq(out, "\n") {
		name, _, _ := strings.Cut(line, ",")
		name = strings.TrimSpace(strings.ReplaceAll(name, `\-`, "-"))
		if name == "" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@") || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		names = append(names, name)
	}
	var res []string
	for _, n := range names {
		low := strings.ToLower(n)
		folded := false
		for _, w := range weights {
			if base, ok := strings.CutSuffix(low, " "+w); ok && seen[base] {
				folded = true
				break
			}
		}
		if !folded {
			res = append(res, n)
		}
	}
	sort.Slice(res, func(i, j int) bool { return strings.ToLower(res[i]) < strings.ToLower(res[j]) })
	return res
}
