package reader

import (
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

// XLSXReader handles .xlsx files.
type XLSXReader struct{}

// Extensions returns the file extensions this reader handles.
func (r *XLSXReader) Extensions() []string {
	return []string{".xlsx"}
}

// Read extracts UTF-8 plain text from a .xlsx file.
func (r *XLSXReader) Read(path string) (string, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to open xlsx file: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	var builder strings.Builder
	sheets := f.GetSheetList()
	for _, sheet := range sheets {
		rows, err := f.GetRows(sheet)
		if err != nil {
			continue
		}
		for _, row := range rows {
			var cols []string
			for _, colCell := range row {
				cols = append(cols, colCell)
			}
			if len(cols) > 0 {
				builder.WriteString(strings.Join(cols, "\t"))
				builder.WriteString("\n")
			}
		}
	}
	return builder.String(), nil
}
