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
	"os/signal"
	"runtime"
	"strings"
	"syscall"
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
		fmt.Fprintln(os.Stderr, "usage: finplan [--demo]\n       finplan [--demo] --screenshot <out.png> [--tab <name>] [--frames N] [--size WxH] [--select <name>] [--open <picker>] [--check-ids]\n       finplan [--demo | --db <path>] <command> ...   (run finplan help)")
		flag.PrintDefaults()
	}
	demo := flag.Bool("demo", false, "use a throwaway database holding an invented family instead of your own")
	out := flag.String("screenshot", "", "draw one frame sequence without waiting for input, save the last frame to this PNG file and exit")
	tab := flag.String("tab", "Graph", "with --screenshot: the tab to show, one of "+strings.Join(tabs, ", "))
	frames := flag.Int("frames", 10, "with --screenshot: how many frames to draw before saving")
	size := flag.String("size", "1280x800", "with --screenshot: the window size in pixels, WxH")
	sel := flag.String("select", "", "with --screenshot: the projection, asset or account to select, by name (Projections, Assets and Accounts tabs)")
	open := flag.String("open", "", "with --screenshot: a picker to show open, one of add-line (Graph tab), target (Projections tab, needs --select) and member (Assets tab, needs --select)")
	checkIDs := flag.Bool("check-ids", false, "with --screenshot: after saving, move the mouse over the whole window and fail if two visible widgets share an ID")
	flag.Parse()
	var shot *shotOptions
	if *out != "" {
		var err error
		if shot, err = newShotOptions(*out, *tab, *frames, *size, *sel, *open); err != nil {
			log.Fatal(err)
		}
		shot.checkIDs = *checkIDs
	} else {
		bad := ""
		flag.Visit(func(f *flag.Flag) {
			if f.Name != "demo" {
				bad = f.Name
			}
		})
		if bad != "" {
			log.Fatalf("--%s needs --screenshot", bad)
		}
	}
	if err := run(*demo, shot); err != nil {
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

// run shows the window until it is closed or a SIGINT or SIGTERM arrives, or, with shot set, draws the screenshot.
func run(demo bool, shot *shotOptions) error {
	// Notify before the demo folder exists, so a signal during setup is still handled, and before backend.Init: SDL installs its own SIGINT handler only over the default one.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stop)

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

	if shot != nil {
		if err := shot.selectItem(app); err != nil {
			return err
		}
		openRequest = shot.openID
		if shot.open == "member" {
			app.assets.add.kind = memberAccount
		}
		setDefaultVideoDriver()
	}
	imgui.CreateContext()
	defer imgui.DestroyContext()
	implot.CreateContext()
	defer implot.DestroyContext()

	title := "finplan"
	if demo {
		title = "finplan (demo)"
	}
	width, height := 1280, 800
	shotLeft := 0
	if shot != nil {
		width, height, shotLeft = shot.width, shot.height, shot.frames
	}
	if err := backend.Init(title, width, height); err != nil {
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
		if shot != nil {
			timeout = 0
		}
		select {
		case <-stop:
			return nil
		default:
		}
		events := backend.WaitEvents(timeout)
		if events&backend.EventQuit != 0 {
			if shot != nil {
				return fmt.Errorf("the window was closed before the screenshot was taken")
			}
			return nil
		}
		if shot != nil {
			app.forceTab, frames = shot.tab, 1
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
		if shotLeft == 1 {
			img, err := backend.RenderCapture()
			if err != nil {
				return fmt.Errorf("read the frame back: %w", err)
			}
			if err := writePNG(shot.out, img); err != nil {
				return err
			}
			if shot.checkIDs {
				return scanIDs(app)
			}
			return nil
		}
		shotLeft--
		backend.Render()
		if app.quit {
			return nil
		}
	}
}
