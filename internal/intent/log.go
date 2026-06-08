package intent

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Log là behavior log append-only, lưu mỗi event 1 dòng JSON trong events.jsonl.
// Append-only = không sửa lịch sử, an toàn, dễ replay cho dự đoán/đánh giá.
type Log struct {
	mu        sync.Mutex
	path      string
	maxEvents int // rotate khi vượt; 0 = mặc định 5000
}

// NewLog tạo Log ghi vào <dir>/events.jsonl.
func NewLog(dir string) *Log {
	return &Log{path: filepath.Join(dir, "events.jsonl"), maxEvents: 5000}
}

// Append ghi thêm 1 event (1 dòng JSON). An toàn nhiều goroutine.
func (l *Log) Append(e Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	if l.maxEvents > 0 {
		if err := l.rotateIfNeeded(); err != nil {
			return err
		}
	}
	return nil
}

// rotateIfNeeded đếm số dòng; nếu > maxEvents, ghi lại chỉ giữ maxEvents/2 dòng cuối.
func (l *Log) rotateIfNeeded() error {
	// đếm nhanh không khoá lại (đang trong Append đã giữ mu)
	f, err := os.Open(l.path)
	if err != nil {
		return err
	}
	var lines [][]byte
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		b := make([]byte, len(sc.Bytes()))
		copy(b, sc.Bytes())
		lines = append(lines, b)
	}
	f.Close()
	if len(lines) <= l.maxEvents {
		return nil
	}
	keep := lines[len(lines)-l.maxEvents/2:] // giữ nửa ngưỡng, gần đây nhất
	tmp := l.path + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	for _, b := range keep {
		out.Write(b)
		out.Write([]byte{'\n'})
	}
	out.Close()
	return os.Rename(tmp, l.path)
}

// Load đọc toàn bộ event. File chưa tồn tại → trả nil (không lỗi).
func (l *Log) Load() ([]Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	f, err := os.Open(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024) // dòng dài (path unicode)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(line, &e); err != nil {
			continue // dòng hỏng không làm chết cả log
		}
		out = append(out, e)
	}
	return out, sc.Err()
}
