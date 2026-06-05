package reader

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/ledongthuc/pdf"
)

// PDFReader handles .pdf files
type PDFReader struct{}

// Extensions returns the file extensions this reader handles.
func (r *PDFReader) Extensions() []string {
	return []string{".pdf"}
}

// Read extracts UTF-8 plain text from a .pdf file.
func (r *PDFReader) Read(path string) (text string, err error) {
	defer func() {
		if recoveryVal := recover(); recoveryVal != nil {
			err = fmt.Errorf("recovered from panic in ledongthuc/pdf reader: %v", recoveryVal)
		}
	}()

	_, lookErr := exec.LookPath("pdftotext")
	if lookErr == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "pdftotext", "-enc", "UTF-8", path, "-")
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if runErr := cmd.Run(); runErr != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return "", fmt.Errorf("pdftotext execution timed out: %w", ctx.Err())
			}
			return "", fmt.Errorf("pdftotext failed (stderr: %q): %w", stderr.String(), runErr)
		}
		return stdout.String(), nil
	}

	return r.readFallback(path)
}

func (r *PDFReader) readFallback(path string) (string, error) {
	f, readerObj, err := pdf.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open pdf file: %w", err)
	}
	defer f.Close()

	plainTextReader, err := readerObj.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("failed to get pdf plain text: %w", err)
	}

	var buf bytes.Buffer
	_, err = buf.ReadFrom(plainTextReader)
	if err != nil {
		return "", fmt.Errorf("failed to read pdf text buffer: %w", err)
	}

	return buf.String(), nil
}
