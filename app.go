package main

import (
	"context"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Window geometry. Mini is deliberately small enough that the 180px dial plus its
// drag strip is the entire contents — anything larger stops reading as a widget
// and starts reading as a window you forgot to close.
const (
	normalWidth  = 420
	normalHeight = 620
	miniWidth    = 220
	miniHeight   = 250
	// miniMargin is the gap from the screen corner when the widget parks itself.
	miniMargin = 24
)

// App is the bound object. Every method here is callable from the frontend as
// window.go.main.App.<Name>.
type App struct {
	ctx context.Context
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// Started by the Run key rather than by a person: come up out of the way.
	// A full-size window appearing over whatever you opened the laptop to do is
	// an irritant, and an irritant is what gets an autostart entry deleted.
	if launchedByAutostart() {
		wruntime.WindowMinimise(ctx)
	}
}

// SetMini shrinks the window to the timer and pins it above everything else.
//
// Always-on-top is applied only in mini mode. A full-size window that refuses to
// go behind anything is an irritant rather than a commitment device, and the
// point of the widget is to stay visible while you work in something else.
func (a *App) SetMini(mini bool) {
	if !mini {
		wruntime.WindowSetAlwaysOnTop(a.ctx, false)
		wruntime.WindowSetSize(a.ctx, normalWidth, normalHeight)
		wruntime.WindowCenter(a.ctx)
		return
	}
	wruntime.WindowSetSize(a.ctx, miniWidth, miniHeight)
	wruntime.WindowSetAlwaysOnTop(a.ctx, true)
	a.parkTopRight()
}

// parkTopRight drops the widget into the top-right corner of the primary screen,
// the way a sticky note lands where you left it rather than in the middle of
// whatever you were reading.
func (a *App) parkTopRight() {
	screens, err := wruntime.ScreenGetAll(a.ctx)
	if err != nil {
		return // leave it where it is; a widget in the wrong corner still works
	}
	for _, s := range screens {
		if !s.IsPrimary {
			continue
		}
		wruntime.WindowSetPosition(a.ctx, s.Size.Width-miniWidth-miniMargin, miniMargin)
		return
	}
}

// Minimise and Quit exist because the window is frameless: with no OS chrome,
// the only close button is the one the page draws.
func (a *App) Minimise() { wruntime.WindowMinimise(a.ctx) }
func (a *App) Quit()     { wruntime.Quit(a.ctx) }
