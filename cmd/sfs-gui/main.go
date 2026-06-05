package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"sfs/internal/engine"
)

// showResultDetail displays a dialog showing the full text snippet and file path of a clicked result.
func showResultDetail(w fyne.Window, res engine.Result) {
	textLabel := widget.NewLabel(res.Text)
	textLabel.Wrapping = fyne.TextWrapWord

	scroll := container.NewScroll(textLabel)
	scroll.SetMinSize(fyne.NewSize(500, 300))

	filePathLabel := widget.NewLabel(res.FilePath)
	filePathLabel.Wrapping = fyne.TextWrapBreak

	header := container.NewVBox(
		widget.NewLabelWithStyle("Đường dẫn file:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		filePathLabel,
		widget.NewLabelWithStyle(fmt.Sprintf("Điểm số (Score): %.4f", res.Score), fyne.TextAlignLeading, fyne.TextStyle{Italic: true}),
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Nội dung đoạn trích:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)

	content := container.NewBorder(
		header,
		nil, nil, nil,
		scroll,
	)

	d := dialog.NewCustom("Chi tiết đoạn trích", "Đóng", content, w)
	d.Resize(fyne.NewSize(650, 500))
	d.Show()
}

func main() {
	myApp := app.New()
	myWindow := myApp.NewWindow("Tìm file")

	// Get root path from environment variable or default to CWD
	root := os.Getenv("SFS_ROOT")
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			root = "."
		}
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = root
	}

	// Status label at the bottom of the window
	statusLabel := widget.NewLabel("Đang tải công cụ tìm kiếm và mô hình...")

	// Result data slices
	var exactResults []engine.Result
	var suggestResults []engine.Result

	// List of Exact Results ("CHÍNH XÁC")
	exactList := widget.NewList(
		func() int {
			return len(exactResults)
		},
		func() fyne.CanvasObject {
			return container.NewVBox(
				widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
				widget.NewLabel(""),
			)
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			if id < 0 || id >= len(exactResults) {
				return
			}
			box := item.(*fyne.Container)
			titleLabel := box.Objects[0].(*widget.Label)
			snippetLabel := box.Objects[1].(*widget.Label)

			res := exactResults[id]
			// Display file name (base name) + score
			titleLabel.SetText(fmt.Sprintf("%s (Score: %.4f)", filepath.Base(res.FilePath), res.Score))

			// Format single-line snippet preview
			snippet := strings.ReplaceAll(res.Text, "\n", " ")
			runes := []rune(snippet)
			if len(runes) > 100 {
				snippet = string(runes[:100]) + "..."
			}
			snippetLabel.SetText(snippet)
		},
	)

	// List of Suggest Results ("GỢI Ý")
	suggestList := widget.NewList(
		func() int {
			return len(suggestResults)
		},
		func() fyne.CanvasObject {
			return container.NewVBox(
				widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
				widget.NewLabel(""),
			)
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			if id < 0 || id >= len(suggestResults) {
				return
			}
			box := item.(*fyne.Container)
			titleLabel := box.Objects[0].(*widget.Label)
			snippetLabel := box.Objects[1].(*widget.Label)

			res := suggestResults[id]
			// Display file name (base name) + score
			titleLabel.SetText(fmt.Sprintf("%s (Score: %.4f)", filepath.Base(res.FilePath), res.Score))

			// Format single-line snippet preview
			snippet := strings.ReplaceAll(res.Text, "\n", " ")
			runes := []rune(snippet)
			if len(runes) > 100 {
				snippet = string(runes[:100]) + "..."
			}
			snippetLabel.SetText(snippet)
		},
	)

	// Item selections
	exactList.OnSelected = func(id widget.ListItemID) {
		if id < 0 || id >= len(exactResults) {
			return
		}
		exactList.Unselect(id)
		showResultDetail(myWindow, exactResults[id])
	}

	suggestList.OnSelected = func(id widget.ListItemID) {
		if id < 0 || id >= len(suggestResults) {
			return
		}
		suggestList.Unselect(id)
		showResultDetail(myWindow, suggestResults[id])
	}

	// Search Entry
	searchEntry := widget.NewEntry()
	searchEntry.SetPlaceHolder("Nhập từ khóa tìm kiếm...")
	searchEntry.Disable() // Disable search entry while loading

	searchButton := widget.NewButton("Tìm kiếm", nil)
	searchButton.Disable() // Disable search button while loading

	indexButton := widget.NewButton("Chọn thư mục & Index", nil)
	indexButton.Disable()

	var eng *engine.Engine

	// Async engine loading
	go func() {
		// Initialize the engine: New(DefaultConfig(root, filepath.Join(root,".sfsindex")))
		e, err := engine.New(engine.DefaultConfig(absRoot, filepath.Join(absRoot, ".sfsindex")))
		if err != nil {
			statusLabel.SetText(fmt.Sprintf("Lỗi khởi tạo engine: %v", err))
			dialog.ShowError(err, myWindow)
			return
		}
		eng = e
		statusLabel.SetText(fmt.Sprintf("Đã tải xong engine. Thư mục gốc: %s", absRoot))
		searchEntry.Enable()
		searchButton.Enable()
		indexButton.Enable()
		myWindow.Canvas().Focus(searchEntry)
	}()

	performSearch := func() {
		query := strings.TrimSpace(searchEntry.Text)
		if query == "" {
			return
		}
		if eng == nil {
			statusLabel.SetText("Engine chưa sẵn sàng.")
			return
		}

		statusLabel.SetText("Đang tìm kiếm...")
		searchEntry.Disable()
		searchButton.Disable()
		indexButton.Disable()

		go func() {
			defer func() {
				searchEntry.Enable()
				searchButton.Enable()
				indexButton.Enable()
				myWindow.Canvas().Focus(searchEntry)
			}()

			results, err := eng.SearchRanked(query, 10)
			if err != nil {
				statusLabel.SetText(fmt.Sprintf("Lỗi tìm kiếm: %v", err))
				dialog.ShowError(err, myWindow)
				return
			}

			exactResults = results.Exact
			suggestResults = results.Suggest

			exactList.Refresh()
			suggestList.Refresh()

			statusLabel.SetText(fmt.Sprintf("Tìm kiếm hoàn tất. CHÍNH XÁC: %d kết quả | GỢI Ý: %d kết quả", len(exactResults), len(suggestResults)))
		}()
	}

	searchEntry.OnSubmitted = func(s string) {
		performSearch()
	}
	searchButton.OnTapped = performSearch

	indexButton.OnTapped = func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, myWindow)
				statusLabel.SetText(fmt.Sprintf("Lỗi chọn thư mục: %v", err))
				return
			}
			if uri == nil {
				return
			}
			path := uri.Path()

			indexButton.Disable()
			searchEntry.Disable()
			searchButton.Disable()
			statusLabel.SetText(fmt.Sprintf("Đang lập chỉ mục: %s ... (có thể mất vài phút)", path))

			go func() {
				defer func() {
					indexButton.Enable()
					searchEntry.Enable()
					searchButton.Enable()
				}()

				err := eng.Index(path)
				if err != nil {
					dialog.ShowError(err, myWindow)
					statusLabel.SetText(fmt.Sprintf("Lỗi lập chỉ mục: %v", err))
					return
				}
				statusLabel.SetText(fmt.Sprintf("Đã lập chỉ mục xong thư mục: %s. Giờ bạn có thể tìm kiếm.", path))
			}()
		}, myWindow)
	}

	// Layout Setup
	exactCol := container.NewBorder(
		widget.NewLabelWithStyle("CHÍNH XÁC", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		nil, nil, nil,
		exactList,
	)

	suggestCol := container.NewBorder(
		widget.NewLabelWithStyle("GỢI Ý", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		nil, nil, nil,
		suggestList,
	)

	listsGrid := container.NewGridWithColumns(2, exactCol, suggestCol)

	searchBar := container.NewBorder(nil, nil, nil, container.NewHBox(indexButton, searchButton), searchEntry)

	content := container.NewBorder(
		searchBar,
		statusLabel,
		nil, nil,
		listsGrid,
	)

	myWindow.SetContent(content)
	myWindow.Resize(fyne.NewSize(850, 600))

	// Clean up resources on window close/application exit
	myWindow.ShowAndRun()

	if eng != nil {
		eng.Close()
	}
}
