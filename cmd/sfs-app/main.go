package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"sfs/internal/engine"
	"sfs/internal/webui"

	"github.com/getlantern/systray"
	"github.com/webview/webview_go"
	"golang.design/x/hotkey"
)

var (
	isBarVisible bool   = true
	currentView  string = "search"
)

func main() {
	sfsRoot := os.Getenv("SFS_ROOT")
	var cfg engine.Config
	if sfsRoot != "" {
		absRoot, err := filepath.Abs(sfsRoot)
		if err != nil {
			log.Fatalf("Error determining absolute root path of SFS_ROOT: %v", err)
		}
		cfg = engine.DefaultConfig(absRoot, filepath.Join(absRoot, ".sfsindex"))
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatalf("Error getting current working directory: %v", err)
		}
		cfg = engine.DefaultConfig("", filepath.Join(cwd, ".sfsindex"))
	}

	port := os.Getenv("SFS_PORT")
	if port == "" {
		port = "8765"
	}

	srv, err := webui.Start(cfg, port)
	if err != nil {
		log.Fatalf("Failed to start web server: %v", err)
	}
	defer srv.Shutdown()

	// Open webview window
	w := webview.New(true)
	defer w.Destroy()

	w.SetTitle("better-recoll")
	w.SetSize(640, 90, webview.HintNone)

	// Make the window floating initially
	makeFloating(w.Window())

	// Bind folder picker
	w.Bind("pickFolder", func() string {
		cmd := exec.Command("osascript", "-e", `POSIX path of (choose folder with prompt "Chọn thư mục để lập chỉ mục")`)
		out, err := cmd.Output()
		if err != nil {
			// User cancelled or error occurred
			return ""
		}
		return strings.TrimSpace(string(out))
	})

	// Bind window operations
	w.Bind("resizeWindow", func(width, height int) {
		w.Dispatch(func() {
			resizeWindow(w.Window(), width, height)
		})
	})

	w.Bind("hideBar", func() {
		w.Dispatch(func() {
			hideWindow(w.Window())
			isBarVisible = false
		})
	})

	// Run systray in a goroutine
	go func() {
		onReady := func() {
			systray.SetTitle("🔍")
			systray.SetTooltip("SFS Search Engine")

			mOpen := systray.AddMenuItem("Mở tìm kiếm", "Mở tìm kiếm")
			mSetting := systray.AddMenuItem("Cài đặt", "Cài đặt")
			mQuit := systray.AddMenuItem("Thoát", "Thoát")

			for {
				select {
				case <-mOpen.ClickedCh:
					w.Dispatch(func() {
						currentView = "search"
						makeFloating(w.Window())
						w.Navigate("http://localhost:" + port)
						w.Eval("showView('search')")
						showWindow(w.Window())
						bringToFront()
						isBarVisible = true
					})
				case <-mSetting.ClickedCh:
					w.Dispatch(func() {
						currentView = "settings"
						makeNormalWindow(w.Window())
						w.Navigate("http://localhost:" + port)
						w.Eval("showView('setting')")
						centerWindow(w.Window())
						showWindow(w.Window())
						bringToFront()
						isBarVisible = true
					})
				case <-mQuit.ClickedCh:
					systray.Quit()
				}
			}
		}

		onExit := func() {
			w.Dispatch(func() {
				w.Destroy()
				os.Exit(0)
			})
		}

		systray.Run(onReady, onExit)
	}()

	// Register global hotkey in a goroutine
	go func() {
		time.Sleep(1 * time.Second)

		// ⌘⇧Space hay bị macOS chiếm (input source / Finder search) → app không nhận.
		// Dùng ⌃⌥Space (Control+Option+Space) — ít xung đột hơn.
		hk := hotkey.New([]hotkey.Modifier{hotkey.ModCtrl, hotkey.ModOption}, hotkey.KeySpace)
		if err := hk.Register(); err != nil {
			log.Printf("hotkey: failed to register: %v", err)
			return
		}
		defer hk.Unregister()

		log.Printf("hotkey registered: Control+Option+Space")
		for range hk.Keydown() {
			log.Printf("hotkey pressed → toggle floating bar")
			w.Dispatch(func() {
				// Toggle dựa trên trạng thái THẬT của cửa sổ (visible + focused),
				// KHÔNG dùng cờ isBarVisible — cờ đó lệch khi click ra ngoài làm
				// cửa sổ mất focus mà không ai cập nhật, khiến lần bấm đầu lại "ẩn"
				// một cửa sổ vốn đã khuất → cảm giác "hotkey không ăn".
				if windowVisible(w.Window()) && currentView != "settings" {
					hideWindow(w.Window())
					isBarVisible = false
				} else {
					// Luôn HIỆN + focus (đúng kiểu Spotlight: bấm là bật lên).
					currentView = "search"
					makeFloating(w.Window())
					w.Navigate("http://localhost:" + port)
					w.Eval("showView('search')")
					showWindow(w.Window())
					bringToFront()
					isBarVisible = true
				}
			})
		}
	}()

	w.Navigate("http://localhost:" + port)
	w.Dispatch(func() {
		w.Eval("showView('search')")
	})
	w.Run()
}

func bringToFront() {
	pid := os.Getpid()
	script := fmt.Sprintf(`tell application "System Events" to set frontmost of (first process whose unix id is %d) to true`, pid)
	cmd := exec.Command("osascript", "-e", script)
	_ = cmd.Run()
}
