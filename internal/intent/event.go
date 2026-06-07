// Package intent ghi và phân tích hành vi người dùng để dự đoán nhu cầu —
// "bộ não thấu hiểu". 100% LOCAL: mọi dữ liệu nằm trong .sfsindex, không ra mạng.
package intent

import "time"

// EventType là loại sự kiện hành vi.
type EventType string

const (
	EventAppOpen          EventType = "app_open"          // user mở app
	EventSearch           EventType = "search"            // user gõ + search
	EventOpen             EventType = "open"              // user click mở 1 file kết quả
	EventSuggestionClick  EventType = "suggestion_click"  // click 1 gợi ý chủ động
	EventSuggestionIgnore EventType = "suggestion_ignore" // bỏ qua toàn bộ gợi ý
)

// Event là một sự kiện hành vi có dấu thời gian. Các field tuỳ loại (omitempty).
type Event struct {
	Time      time.Time `json:"t"`
	Type      EventType `json:"type"`
	Query     string    `json:"query,omitempty"`     // search/suggestion: từ khoá
	Path      string    `json:"path,omitempty"`      // open/suggestion_click: file
	FromQuery string    `json:"fromQuery,omitempty"` // open: mở từ query nào
	Rank      int       `json:"rank,omitempty"`      // suggestion_click: vị trí (1-based)
	Shown     []string  `json:"shown,omitempty"`     // suggestion_ignore: các path đã hiện
}
