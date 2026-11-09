// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

// Command finplan is a personal financial planner. It shows the Graph tab of the record in its SQLite
// database, the user's own or, with --demo, a throwaway one holding an invented family, and edits its
// projections in the Projections tab; the other tabs are placeholders. With a subcommand (see finplan help) it instead reads the database and prints JSON, without a window.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"time"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/AllenDang/cimgui-go/implot"

	"github.com/joeriedel/finplan/internal/backend"
	"github.com/joeriedel/finplan/internal/store"
)

const (
	// idleWaitMs bounds how long the loop sleeps waiting for an event when nothing needs redrawing.
	idleWaitMs = 500
	// settleFrames is how many frames are drawn after input so ImGui's one-frame-late state settles.
	settleFrames = 3
	// pollEvery is how often the database is checked for changes made by other programs.
	pollEvery = time.Second
)

func init() {
	// SDL and OpenGL must stay on the thread that created the window.
	runtime.LockOSThread()
}

func main() {
	if isCLI(os.Args[1:]) {
		os.Exit(runCLI(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
	}
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: finplan [--demo]\n       finplan [--demo | --db <path>] <command> ...   (run finplan help)")
		flag.PrintDefaults()
	}
	demo := flag.Bool("demo", false, "use a throwaway database holding an invented family instead of your own")
	flag.Parse()
	if err := run(*demo); err != nil {
		log.Fatal(err)
	}
}

// openStore opens the user's database, or builds the demo one. The returned function releases it.
func openStore(demo bool) (*store.Store, func(), error) {
	if demo {
		return openDemo()
	}
	path, err := databasePath(os.Getenv, os.UserHomeDir)
	if err != nil {
		return nil, nil, err
	}
	st, err := openDatabase(path)
	if err != nil {
		return nil, nil, err
	}
	return st, func() { st.Close() }, nil
}

func run(demo bool) error {
	st, closeStore, err := openStore(demo)
	if err != nil {
		return err
	}
	defer closeStore()
	version, err := st.DataVersion()
	if err != nil {
		return err
	}
	poll := newVersionPoll(pollEvery, version)
	app, err := newApp(st)
	if err != nil {
		return err
	}

	imgui.CreateContext()
	defer imgui.DestroyContext()
	implot.CreateContext()
	defer implot.DestroyContext()

	title := "finplan"
	if demo {
		title = "finplan (demo)"
	}
	if err := backend.Init(title, 1280, 800); err != nil {
		return fmt.Errorf("backend init: %w", err)
	}
	defer backend.Shutdown()

	loadFont()
	applySlate(imgui.CurrentStyle())
	applySlatePlot()
	scale := float32(1)
	uiScale = scale

	frames := settleFrames
	for {
		timeout := idleWaitMs
		if frames > 0 {
			timeout = 0
		}
		events := backend.WaitEvents(timeout)
		if events&backend.EventQuit != 0 {
			return nil
		}
		if events&backend.EventInput != 0 {
			frames = settleFrames
		}
		if poll.due(time.Now()) {
			if v, err := st.DataVersion(); err != nil {
				log.Printf("data version: %v", err)
			} else if poll.changed(v) {
				app.reload()
				frames = settleFrames
			}
		}
		if frames == 0 {
			continue
		}
		frames--

		if s := backend.UIScale(); s != scale {
			// Scaling is not repeatable on a live style (sizes are truncated), so rebuild it from defaults.
			cur := imgui.CurrentStyle()
			base := imgui.NewStyle()
			applySlate(base)
			base.ScaleAllSizes(s)
			base.SetFontSizeBase(cur.FontSizeBase())
			base.SetFontScaleMain(cur.FontScaleMain())
			base.SetFontScaleDpi(s)
			*cur.CData = *base.CData
			base.Destroy()
			scale = s
			uiScale = s
		}

		backend.NewFrame()
		imgui.NewFrame()
		app.draw()
		imgui.Render()
		backend.Render()
		if app.quit {
			return nil
		}
	}
}
