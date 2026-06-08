package reader

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

// PptxReader đọc PowerPoint .pptx (định dạng văn phòng cốt lõi của dân văn phòng).
//
// .pptx = file ZIP chứa XML (Open XML, cùng họ với .docx/.xlsx). Mỗi slide là
// ppt/slides/slideN.xml; text nằm trong các thẻ <a:t>. Đọc bằng stdlib
// (archive/zip + encoding/xml) — KHÔNG cần dependency ngoài (giữ triết lý 1 binary).
type PptxReader struct{}

func (r *PptxReader) Extensions() []string {
	return []string{".pptx"}
}

var slideNameRe = regexp.MustCompile(`^ppt/slides/slide(\d+)\.xml$`)

// Read trích toàn bộ text các slide theo đúng thứ tự slide1, slide2, ...
func (r *PptxReader) Read(path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("failed to open pptx (zip): %w", err)
	}
	defer zr.Close()

	// gom các slide kèm số thứ tự để sắp đúng thứ tự trình bày
	type slide struct {
		num int
		f   *zip.File
	}
	var slides []slide
	for _, f := range zr.File {
		if m := slideNameRe.FindStringSubmatch(f.Name); m != nil {
			n := 0
			fmt.Sscanf(m[1], "%d", &n)
			slides = append(slides, slide{num: n, f: f})
		}
	}
	sort.Slice(slides, func(i, j int) bool { return slides[i].num < slides[j].num })

	var b strings.Builder
	for _, s := range slides {
		txt, err := extractSlideText(s.f)
		if err != nil {
			continue // một slide hỏng không làm chết cả file
		}
		if txt != "" {
			b.WriteString(txt)
			b.WriteString("\n")
		}
	}
	return b.String(), nil
}

// extractSlideText đọc các thẻ <a:t> trong 1 slide XML.
func extractSlideText(f *zip.File) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}

	dec := xml.NewDecoder(strings.NewReader(string(data)))
	var sb strings.Builder
	inText := false
	for {
		tok, err := dec.Token()
		if err != nil {
			break // EOF hoặc lỗi → dừng, trả phần đã đọc
		}
		switch t := tok.(type) {
		case xml.StartElement:
			// thẻ text run: <a:t> (Local = "t")
			if t.Name.Local == "t" {
				inText = true
			}
		case xml.EndElement:
			if t.Name.Local == "t" {
				inText = false
				sb.WriteString(" ")
			}
		case xml.CharData:
			if inText {
				sb.Write(t)
			}
		}
	}
	return strings.TrimSpace(sb.String()), nil
}
