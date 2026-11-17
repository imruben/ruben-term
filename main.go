package main

import (
	"embed"
	"log"
	"runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend
var assets embed.FS

func main() {
	app := NewApp()
	st := loadState()

	opts := &options.App{
		Title:            "ruben-term",
		Width:            st.Width,
		Height:           st.Height,
		MinWidth:         480,
		MinHeight:        300,
		BackgroundColour: &options.RGBA{R: 14, G: 15, B: 17, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.startup,
		OnBeforeClose:    app.beforeClose,
		OnShutdown:       app.shutdown,
		Bind:             []any{app},
		// Windows: no title bar, the tab strip is the title bar (drag it, double-click it).
		Frameless: runtime.GOOS == "windows",
		Windows: &windows.Options{
			Theme:                windows.Dark,
			DisablePinchZoom:     true,
			IsZoomControlEnabled: false,
		},
		// macOS: the traffic lights sit inside the tab strip.
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
			About:    &mac.AboutInfo{Title: "ruben-term", Message: "A small terminal for developers."},
		},
	}
	if runtime.GOOS == "darwin" {
		// the standard menus give ⌘Q, ⌘C / ⌘V and ⌘M their usual meaning
		opts.Menu = menu.NewMenuFromItems(menu.AppMenu(), menu.EditMenu(), menu.WindowMenu())
	}
	if err := wails.Run(opts); err != nil {
		log.Fatal(err)
	}
}
