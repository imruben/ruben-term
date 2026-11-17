package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	wails "github.com/wailsapp/wails/v2/pkg/runtime"
)

// version is set by the release workflow (-ldflags "-X main.version=1.2.3"). A build
// made by hand is "dev", and a dev build never asks to update.
var version = "dev"

// The code is private, so the installers are published to a public repo that holds
// only releases (see .github/workflows/build.yml).
const releasesRepo = "imruben/ruben-term-releases"

// Update is what CheckUpdate found.
type Update struct {
	Current    string `json:"current"`
	Latest     string `json:"latest"`
	Newer      bool   `json:"newer"`      // Latest is newer than Current
	Notes      string `json:"notes"`      // the release notes
	Page       string `json:"page"`       // the release on GitHub
	CanInstall bool   `json:"canInstall"` // Windows: the app downloads and runs the installer itself
}

type releaseAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"` // "sha256:…"
}

type release struct {
	TagName string         `json:"tag_name"`
	HTMLURL string         `json:"html_url"`
	Body    string         `json:"body"`
	Assets  []releaseAsset `json:"assets"`
}

// CheckUpdate asks GitHub for the latest release.
func (a *App) CheckUpdate() (Update, error) {
	u := Update{Current: version}
	if version == "dev" {
		return u, nil
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+releasesRepo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ruben-term/"+version)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return u, fmt.Errorf("could not check for updates: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return u, nil // nothing released yet
	}
	if res.StatusCode != http.StatusOK {
		return u, fmt.Errorf("could not check for updates: GitHub answered %s", res.Status)
	}
	var rel release
	if err := json.NewDecoder(res.Body).Decode(&rel); err != nil {
		return u, fmt.Errorf("could not check for updates: %w", err)
	}
	u.Latest = strings.TrimPrefix(rel.TagName, "v")
	u.Page = rel.HTMLURL
	u.Notes = rel.Body
	u.Newer = newerVersion(u.Latest, version)

	a.mu.Lock()
	defer a.mu.Unlock()
	a.installer = nil
	if runtime.GOOS == "windows" && u.Newer {
		for _, as := range rel.Assets {
			if strings.HasSuffix(as.Name, "-setup.exe") {
				as := as
				a.installer = &as
				u.CanInstall = true
				break
			}
		}
	}
	return u, nil
}

// InstallUpdate (Windows) downloads the installer CheckUpdate found, starts it and
// quits. The installer waits for this window to close, installs in silence and opens
// ruben-term again, which brings back the same tabs.
func (a *App) InstallUpdate() error {
	a.mu.Lock()
	as := a.installer
	a.mu.Unlock()
	if as == nil || runtime.GOOS != "windows" {
		return errors.New("no update to install")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Minute)
	defer cancel()
	path, err := download(ctx, as)
	if err != nil {
		return fmt.Errorf("could not download the update: %w", err)
	}
	cmd := exec.Command(path, "/S", "/relaunch")
	if err := cmd.Start(); err != nil {
		os.Remove(path)
		return fmt.Errorf("could not start the installer: %w", err)
	}
	cmd.Process.Release()
	go func() {
		time.Sleep(300 * time.Millisecond) // let the window hear back first
		wails.Quit(a.ctx)
	}()
	return nil
}

// download saves the asset to a temporary file and checks its size and sha256.
func download(ctx context.Context, as *releaseAsset) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", as.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "ruben-term/"+version)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", errors.New(res.Status)
	}
	f, err := os.CreateTemp("", "ruben-term-*-setup.exe")
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), res.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && as.Size > 0 && n != as.Size {
		err = fmt.Errorf("got %d bytes, expected %d", n, as.Size)
	}
	if want, ok := strings.CutPrefix(as.Digest, "sha256:"); err == nil && ok && !strings.EqualFold(want, hex.EncodeToString(h.Sum(nil))) {
		err = errors.New("the file does not match its checksum")
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// newerVersion: is a (like "1.10.0") newer than b? Numbers are compared one by one;
// anything after a "-" is ignored.
func newerVersion(a, b string) bool {
	pa, pb := versionParts(a), versionParts(b)
	for i := range 3 {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func versionParts(v string) [3]int {
	var out [3]int
	v, _, _ = strings.Cut(strings.TrimPrefix(strings.TrimSpace(v), "v"), "-")
	for i, s := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(s)
	}
	return out
}
