package reader

import (
	"fmt"
	"os"
	"strings"
)

// RtfReader đọc .rtf (Rich Text Format) — định dạng văn phòng phổ biến, là plain
// text với control word kiểu \b \i \par {...}. Trích text bằng cách bỏ control
// words + nhóm điều khiển, giữ lại nội dung. Thuần Go, không dependency.
type RtfReader struct{}

func (r *RtfReader) Extensions() []string {
	return []string{".rtf"}
}

func (r *RtfReader) Read(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read rtf: %w", err)
	}
	return stripRTF(string(data)), nil
}

// stripRTF gỡ control words RTF, trả plain text. Xử lý: \word control words,
// \par/\line → xuống dòng, \' hex escape, group {...}, bỏ header font/color table.
func stripRTF(s string) string {
	var out strings.Builder
	i := 0
	n := len(s)
	depth := 0
	// các nhóm cần bỏ HẲN nội dung (bảng font/màu/info — không phải text)
	skipGroupDepth := -1
	for i < n {
		c := s[i]
		switch c {
		case '\\':
			// control word hoặc escape
			if i+1 < n {
				nc := s[i+1]
				if nc == '\\' || nc == '{' || nc == '}' {
					if skipGroupDepth < 0 {
						out.WriteByte(nc)
					}
					i += 2
					continue
				}
				if nc == '\'' && i+3 < n {
					// \'hh — bỏ qua byte mã hoá (giữ đơn giản, không decode codepage)
					i += 4
					continue
				}
				// control word: \ + chữ cái* + số tuỳ chọn + (dấu cách kết thúc)
				j := i + 1
				for j < n && isAlpha(s[j]) {
					j++
				}
				word := s[i+1 : j]
				// tham số số (có thể âm)
				if j < n && (s[j] == '-' || isDigit(s[j])) {
					j++
					for j < n && isDigit(s[j]) {
						j++
					}
				}
				// một dấu cách sau control word bị nuốt
				if j < n && s[j] == ' ' {
					j++
				}
				switch word {
				case "par", "line", "sect", "page":
					if skipGroupDepth < 0 {
						out.WriteByte('\n')
					}
				case "tab":
					if skipGroupDepth < 0 {
						out.WriteByte('\t')
					}
				case "fonttbl", "colortbl", "stylesheet", "info", "pict", "object":
					// nhóm chứa metadata/binary — bỏ tới hết group hiện tại
					skipGroupDepth = depth
				}
				i = j
				continue
			}
			i++
		case '{':
			depth++
			i++
		case '}':
			if skipGroupDepth >= 0 && depth == skipGroupDepth+1 {
				skipGroupDepth = -1
			}
			depth--
			i++
		case '\r', '\n':
			i++ // newline thật trong RTF không có nghĩa; \par mới xuống dòng
		default:
			if skipGroupDepth < 0 {
				out.WriteByte(c)
			}
			i++
		}
	}
	// gọn khoảng trắng thừa
	return strings.TrimSpace(out.String())
}

func isAlpha(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }
func isDigit(b byte) bool { return b >= '0' && b <= '9' }
