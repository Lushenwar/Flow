// Command flow is the desktop shell: a Wails window wrapping the static UI export.
//
// It owns no authority. Every rule, every countdown, and every refusal comes from
// flowd over the loopback API — close this window mid-session and enforcement is
// unaffected, which is the same contract the browser UI has always had.
//
// It lives at the module root rather than under cmd/ because go:embed cannot
// reach upward out of its own directory, and the assets it needs are in ui/out.
// Wails' own tooling assumes a root main.go too, so this is the path of least
// structure rather than a deviation worth arguing with.
package main

import (
	"embed"
	"log"
	"net/http"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

// ui/out is the Next.js static export. `all:` so the export's dot-prefixed
// files come along; without it the build embeds a directory that is missing
// exactly the pieces the app router needs.
//
//go:embed all:ui/out
var assets embed.FS

func main() {
	app := &App{}

	// Frameless because the app draws its own title bar. That is not decoration:
	// mini mode has to lose the OS chrome to read as a sticky note rather than a
	// shrunken program, and Frameless is a build-time option in Wails v2 — it
	// cannot be toggled at runtime, so the full-size window wears a custom bar too.
	err := wails.Run(&options.App{
		Title:     "Flow",
		Width:     normalWidth,
		Height:    normalHeight,
		MinWidth:  miniWidth,
		MinHeight: miniHeight,
		Frameless: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
			// Everything under /api is the daemon's, reached same-origin through
			// the proxy. Everything else is the embedded export.
			Middleware: func(next http.Handler) http.Handler {
				proxy := daemonProxy()
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasPrefix(r.URL.Path, "/api/") {
						proxy.ServeHTTP(w, r)
						return
					}
					next.ServeHTTP(w, r)
				})
			},
		},
		BackgroundColour: &options.RGBA{R: 246, G: 246, B: 247, A: 1},
		OnStartup:        app.startup,
		Bind:             []any{app},
		Windows: &windows.Options{
			// Follow the OS light/dark setting, so the window frame does not
			// fight the palette the page picks with prefers-color-scheme.
			Theme: windows.SystemDefault,
		},
	})
	if err != nil {
		log.Fatalf("flow: %v", err)
	}
}
