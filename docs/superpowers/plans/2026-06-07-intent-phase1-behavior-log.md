# Intent Engine — Giai đoạn 1: Behavior Log (nền móng) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ghi lặng lẽ hành vi người dùng (mở app, search, mở file) vào log local + lưu mtime của file khi index — nền móng cho B/C/D (hồ sơ, dự đoán, học) ở các giai đoạn sau.

**Architecture:** Package mới `internal/intent/` chứa behavior log (append-only `events.jsonl` trong `.sfsindex/`). Store thêm field `ModTime` để biết file vừa sửa (recency). API `POST /api/event` nhận event từ frontend. Frontend bắn event khi mở app + click kết quả. 100% local, không ra mạng.

**Tech Stack:** Go (encoding/json, os), HTTP handler trong webui, vanilla JS fetch trong index.html. Build cần `CGO_LDFLAGS="-L$TMPDIR/sfs-libs"` (symlink libs/ — xem docs/BUILD.md).

> **Lưu ý build/test:** dự án nằm trong path có khoảng trắng ("level 3") → clang CGO `-L` vỡ. TRƯỚC mọi lệnh go: `export CGO_LDFLAGS="-L$TMPDIR/sfs-libs"; ln -sfn "$(pwd)/libs" "$TMPDIR/sfs-libs"`. Chạy test office/intent KHÔNG cần model nên nhanh; test engine cần model (chậm).

---

## File Structure

| File | Trách nhiệm | Create/Modify |
|------|-------------|---------------|
| `internal/intent/event.go` | Định nghĩa struct Event + các loại event | Create |
| `internal/intent/log.go` | Append/Load events vào `events.jsonl`, rotate khi quá lớn | Create |
| `internal/intent/log_test.go` | Test append/load/rotate | Create |
| `internal/store/store.go` | Thêm field `ModTime time.Time` vào Chunk | Modify |
| `internal/engine/engine.go` | Set ModTime khi tạo Chunk lúc index (từ os.Stat) | Modify |
| `internal/webui/webui.go` | Handler `handleEvent` + route `/api/event` | Modify |
| `internal/webui/event_test.go` | Test handleEvent ghi đúng | Create |
| `internal/webui/assets/index.html` | Bắn event app_open (load) + open (click kết quả) | Modify |

---

## Task 1: Định nghĩa Event struct

**Files:**
- Create: `internal/intent/event.go`

- [ ] **Step 1: Tạo file event.go**

```go
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
```

- [ ] **Step 2: Build để chắc compile**

Run: `export CGO_LDFLAGS="-L$TMPDIR/sfs-libs"; ln -sfn "$(pwd)/libs" "$TMPDIR/sfs-libs"; go build ./internal/intent/ 2>&1 | grep -v "duplicate librar"`
Expected: không lỗi (package compile).

- [ ] **Step 3: Commit**

```bash
git add internal/intent/event.go
git commit -m "feat(intent): định nghĩa Event struct cho behavior log"
```

---

## Task 2: Behavior Log — append + load

**Files:**
- Create: `internal/intent/log.go`
- Test: `internal/intent/log_test.go`

- [ ] **Step 1: Viết test trước (failing)**

```go
package intent

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAppendAndLoad(t *testing.T) {
	dir := t.TempDir()
	l := NewLog(dir)

	e1 := Event{Time: time.Now(), Type: EventAppOpen}
	e2 := Event{Time: time.Now(), Type: EventSearch, Query: "vinamilk"}
	if err := l.Append(e1); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(e2); err != nil {
		t.Fatal(err)
	}

	got, err := l.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("muốn 2 event, được %d", len(got))
	}
	if got[1].Type != EventSearch || got[1].Query != "vinamilk" {
		t.Fatalf("event 2 sai: %+v", got[1])
	}
	// đúng file path
	if _, err := filepath.Abs(l.path); err != nil {
		t.Fatal(err)
	}
}

func TestLoadEmptyReturnsNil(t *testing.T) {
	l := NewLog(t.TempDir())
	got, err := l.Load()
	if err != nil {
		t.Fatalf("load file chưa tồn tại phải không lỗi, được: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("muốn rỗng, được %d", len(got))
	}
}
```

- [ ] **Step 2: Chạy test — phải FAIL**

Run: `export CGO_LDFLAGS="-L$TMPDIR/sfs-libs"; ln -sfn "$(pwd)/libs" "$TMPDIR/sfs-libs"; go test ./internal/intent/ -run TestAppend 2>&1 | grep -v "duplicate librar"`
Expected: FAIL — `NewLog` undefined.

- [ ] **Step 3: Viết log.go**

```go
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
	mu   sync.Mutex
	path string
}

// NewLog tạo Log ghi vào <dir>/events.jsonl.
func NewLog(dir string) *Log {
	return &Log{path: filepath.Join(dir, "events.jsonl")}
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
	return nil
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
```

- [ ] **Step 4: Chạy test — phải PASS**

Run: `export CGO_LDFLAGS="-L$TMPDIR/sfs-libs"; ln -sfn "$(pwd)/libs" "$TMPDIR/sfs-libs"; go test ./internal/intent/ -run "TestAppend|TestLoadEmpty" -v 2>&1 | grep -v "duplicate librar"`
Expected: PASS cả 2.

- [ ] **Step 5: Commit**

```bash
git add internal/intent/log.go internal/intent/log_test.go
git commit -m "feat(intent): behavior log append-only (events.jsonl)"
```

---

## Task 3: Rotate log khi quá lớn

**Files:**
- Modify: `internal/intent/log.go`
- Test: `internal/intent/log_test.go`

- [ ] **Step 1: Thêm test rotate (failing)**

Thêm vào `log_test.go`:

```go
func TestRotateKeepsRecent(t *testing.T) {
	dir := t.TempDir()
	l := NewLog(dir)
	l.maxEvents = 10 // ngưỡng nhỏ để test

	for i := 0; i < 25; i++ {
		if err := l.Append(Event{Time: time.Now(), Type: EventSearch, Query: "q"}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := l.Load()
	if err != nil {
		t.Fatal(err)
	}
	// sau rotate phải còn <= maxEvents (giữ gần đây)
	if len(got) > 10 {
		t.Fatalf("rotate hỏng: còn %d event, phải <= 10", len(got))
	}
	if len(got) == 0 {
		t.Fatal("rotate xoá sạch — phải giữ event gần đây")
	}
}
```

- [ ] **Step 2: Chạy test — phải FAIL**

Run: `export CGO_LDFLAGS="-L$TMPDIR/sfs-libs"; ln -sfn "$(pwd)/libs" "$TMPDIR/sfs-libs"; go test ./internal/intent/ -run TestRotate 2>&1 | grep -v "duplicate librar"`
Expected: FAIL — `l.maxEvents` không tồn tại.

- [ ] **Step 3: Thêm rotate vào log.go**

Sửa struct + NewLog + Append trong `log.go`:

```go
type Log struct {
	mu        sync.Mutex
	path      string
	maxEvents int // rotate khi vượt; 0 = mặc định 5000
}

func NewLog(dir string) *Log {
	return &Log{path: filepath.Join(dir, "events.jsonl"), maxEvents: 5000}
}
```

Sửa cuối hàm `Append`, NGAY TRƯỚC `return nil`:

```go
	// rotate: nếu vượt ngưỡng, giữ lại nửa sau (event gần đây nhất).
	f.Close() // đóng trước khi đếm/ghi lại (đã defer nhưng cần đóng sớm để rotate)
	if l.maxEvents > 0 {
		if err := l.rotateIfNeeded(); err != nil {
			return err
		}
	}
	return nil
```

LƯU Ý: bỏ dòng `defer f.Close()` cũ trong Append (vì giờ Close thủ công trước rotate). Thêm method:

```go
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
```

Vì đã bỏ `defer f.Close()`, sửa các nhánh `return err` giữa chừng trong Append để đóng f trước (hoặc đơn giản: giữ `defer f.Close()` NHƯNG gọi `f.Close()` thêm trước rotate là idempotent — Close 2 lần trên *os.File trả lỗi nhưng vô hại; để chắc, dùng biến `closed`). Cách gọn nhất: giữ defer, và trong rotate mở lại file riêng (như code trên đã làm — nó tự Open/Close riêng). VẬY: KHÔNG cần bỏ defer. Xoá dòng `f.Close()` trong đoạn thêm ở trên, chỉ giữ:

```go
	if l.maxEvents > 0 {
		if err := l.rotateIfNeeded(); err != nil {
			return err
		}
	}
	return nil
```

(rotateIfNeeded tự mở file mới để đọc, không đụng f đang defer.)

- [ ] **Step 4: Chạy test — phải PASS**

Run: `export CGO_LDFLAGS="-L$TMPDIR/sfs-libs"; ln -sfn "$(pwd)/libs" "$TMPDIR/sfs-libs"; go test ./internal/intent/ -v 2>&1 | grep -v "duplicate librar" | grep -E "PASS|FAIL|ok"`
Expected: PASS cả TestAppend, TestLoadEmpty, TestRotate.

- [ ] **Step 5: Commit**

```bash
git add internal/intent/log.go internal/intent/log_test.go
git commit -m "feat(intent): rotate events.jsonl khi vượt 5000 event"
```

---

## Task 4: Store thêm ModTime (recency cần)

**Files:**
- Modify: `internal/store/store.go:6-14`
- Modify: `internal/engine/engine.go` (chỗ tạo store.Chunk, ~dòng 535)

- [ ] **Step 1: Thêm field ModTime vào Chunk**

Sửa `internal/store/store.go`, struct Chunk — thêm dòng ModTime:

```go
type Chunk struct {
	ID            int64
	FilePath      string
	Text          string // bản gốc giữ dấu tiếng Việt
	NormText      string // bản bỏ dấu + lowercase (cho BM25/khớp chữ)
	Offset        int    // vị trí đoạn trong file (để đọc lại đúng đoạn)
	IsBoilerplate bool   // cờ đoạn-khung-chung (template), set bởi DedupeFinder
	Vector        []float32
	ModTime       int64 // Unix giây — thời điểm file sửa lần cuối (recency cho intent)
}
```

LƯU Ý: dùng `int64` (Unix giây) thay `time.Time` để gob encode gọn + tương thích ngược (chunk cũ không có field → ModTime=0, vô hại).

- [ ] **Step 2: Set ModTime khi index — sửa engine.go**

Trong `internal/engine/engine.go`, đoạn walk file (trong indexThrottled, chỗ đọc file ~dòng 353 `reader.ReadFile(path)`), LẤY mtime. Tìm chỗ append vào `pending` (struct pendingChunk) và chỗ tạo `store.Chunk{}`.

Trước hết thêm field vào `pendingChunk` struct (đầu file ~dòng 234):

```go
type pendingChunk struct {
	filePath string
	text     string
	normText string
	offset   int
	modTime  int64
}
```

Trong walk, sau khi `reader.ReadFile(path)` thành công, lấy mtime từ `info` (FileInfo có sẵn trong walk callback):

```go
		mt := info.ModTime().Unix()
```

Khi append pendingChunk (tìm dòng `pending = append(pending, pendingChunk{`), thêm `modTime: mt,`:

```go
			pending = append(pending, pendingChunk{
				filePath: path,
				text:     ch.Text,
				normText: normalize.Normalize(ch.Text),
				offset:   ch.Offset,
				modTime:  mt,
			})
```

Khi tạo store.Chunk (dòng ~535), thêm `ModTime: pc.modTime,`:

```go
				storeChunks[i] = store.Chunk{
					ID:            id,
					FilePath:      pc.filePath,
					Text:          pc.text,
					NormText:      pc.normText,
					Offset:        pc.offset,
					Vector:        vectors[i],
					IsBoilerplate: finder.IsBoilerplate(pc.text),
					ModTime:       pc.modTime,
				}
```

- [ ] **Step 3: Build — phải compile**

Run: `export CGO_LDFLAGS="-L$TMPDIR/sfs-libs"; ln -sfn "$(pwd)/libs" "$TMPDIR/sfs-libs"; go build ./... 2>&1 | grep -v "duplicate librar" | grep -iE "error|cannot" || echo "BUILD OK"`
Expected: BUILD OK.

- [ ] **Step 4: Test không phá store/engine**

Run: `export CGO_LDFLAGS="-L$TMPDIR/sfs-libs"; ln -sfn "$(pwd)/libs" "$TMPDIR/sfs-libs"; go test ./internal/store/ -short 2>&1 | grep -v "duplicate librar" | tail -2`
Expected: `ok  sfs/internal/store`.

- [ ] **Step 5: Commit**

```bash
git add internal/store/store.go internal/engine/engine.go
git commit -m "feat(store): lưu ModTime của file khi index (recency cho intent)"
```

---

## Task 5: API endpoint POST /api/event

**Files:**
- Modify: `internal/webui/webui.go` (thêm handleEvent + route)
- Test: `internal/webui/event_test.go`

- [ ] **Step 1: Viết test (failing)**

Create `internal/webui/event_test.go`:

```go
package webui

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleEventAccepts(t *testing.T) {
	body := []byte(`{"type":"app_open"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/event", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handleEvent(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("muốn 200, được %d (%s)", w.Code, w.Body.String())
	}
}

func TestHandleEventRejectsGet(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/event", nil)
	w := httptest.NewRecorder()
	handleEvent(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET phải 405, được %d", w.Code)
	}
}
```

- [ ] **Step 2: Chạy test — phải FAIL**

Run: `export CGO_LDFLAGS="-L$TMPDIR/sfs-libs"; ln -sfn "$(pwd)/libs" "$TMPDIR/sfs-libs"; go test ./internal/webui/ -run TestHandleEvent 2>&1 | grep -v "duplicate librar"`
Expected: FAIL — `handleEvent` undefined.

- [ ] **Step 3: Thêm import + biến log + handler**

Trong `internal/webui/webui.go`:

Thêm import `"sfs/internal/intent"` và `"time"` (nếu chưa có) vào khối import.

Thêm biến package-level (gần các biến global khác, ~dòng 36):

```go
var behaviorLog *intent.Log
```

Trong `Start()` (sau khi có `cfg`, gần `globalConfig = cfg`), khởi tạo:

```go
	behaviorLog = intent.NewLog(cfg.IndexPath)
```

Thêm handler (gần các handler khác):

```go
// handleEvent nhận 1 event hành vi từ frontend và ghi vào behavior log (local).
// Im lặng nuốt lỗi ghi (không để hỏng UX vì log) nhưng trả 200 nếu nhận hợp lệ.
func handleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var e intent.Event
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	if behaviorLog != nil {
		_ = behaviorLog.Append(e) // lỗi ghi không làm hỏng UX
	}
	w.WriteHeader(http.StatusOK)
}
```

Đăng ký route (cạnh các `mux.HandleFunc` ~dòng 1121):

```go
	mux.HandleFunc("/api/event", handleEvent)
```

- [ ] **Step 4: Chạy test — phải PASS**

Run: `export CGO_LDFLAGS="-L$TMPDIR/sfs-libs"; ln -sfn "$(pwd)/libs" "$TMPDIR/sfs-libs"; go test ./internal/webui/ -run TestHandleEvent -v 2>&1 | grep -v "duplicate librar" | grep -E "PASS|FAIL|ok"`
Expected: PASS cả 2.

- [ ] **Step 5: Commit**

```bash
git add internal/webui/webui.go internal/webui/event_test.go
git commit -m "feat(webui): POST /api/event ghi behavior log"
```

---

## Task 6: Frontend bắn event (app_open + open)

**Files:**
- Modify: `internal/webui/assets/index.html`

- [ ] **Step 1: Thêm hàm gửi event + bắn app_open lúc load**

Trong `<script>` của index.html, thêm hàm helper (gần đầu script, sau khai báo biến):

```javascript
        // Gửi event hành vi về behavior log (local, fire-and-forget — không chặn UI).
        function sendEvent(ev) {
            try {
                fetch('/api/event', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(ev),
                    keepalive: true
                }).catch(() => {}); // lỗi mạng local không làm phiền user
            } catch (e) {}
        }
        // Bắn app_open ngay khi trang load (user mở app).
        sendEvent({ type: 'app_open' });
```

- [ ] **Step 2: Bắn event 'open' khi user click 1 kết quả**

Tìm `createCardHTML` (hàm render mỗi card kết quả). Mỗi card có đường dẫn file. Thêm: khi user click card, gọi `sendEvent({type:'open', path: ..., fromQuery: ...})`.

Cách an toàn (không phá render hiện có): dùng event delegation. Sau hàm `renderResults`, thêm 1 lần (ngoài renderResults, ở scope script):

```javascript
        // Bắt click trên mọi card kết quả → ghi event 'open' (file user thật sự mở).
        document.addEventListener('click', function (e) {
            const card = e.target.closest('[data-filepath]');
            if (card) {
                sendEvent({
                    type: 'open',
                    path: card.getAttribute('data-filepath'),
                    fromQuery: (document.getElementById('search-input') || {}).value || ''
                });
            }
        });
```

LƯU Ý: cần card có attribute `data-filepath`. Trong `createCardHTML` (dòng ~548),
thẻ ngoài cùng là (dòng ~574):

```html
<div class="card card-compact bg-base-100 border border-base-300 hover:border-primary/50 transition-colors cursor-pointer" onclick="showDetailFromJSON(this.querySelector('.filename-link').getAttribute('data-json'))">
```

Thêm `data-filepath="${item.filePath}"` vào thẻ div này (ngay sau `class="..."`):

```html
<div data-filepath="${item.filePath}" class="card card-compact bg-base-100 border border-base-300 hover:border-primary/50 transition-colors cursor-pointer" onclick="showDetailFromJSON(this.querySelector('.filename-link').getAttribute('data-json'))">
```

Ô search input có id chính xác là `search-input` (đã xác nhận, dòng 157) — dùng đúng id này trong event delegation ở trên.

- [ ] **Step 3: Verify thủ công — chạy app, mở, click, xem log**

```bash
export CGO_LDFLAGS="-L$TMPDIR/sfs-libs"; ln -sfn "$(pwd)/libs" "$TMPDIR/sfs-libs"
go build -o /tmp/sfs-server-p1 ./cmd/sfs-server 2>&1 | grep -v "duplicate librar" | grep -iE "error" || echo "build OK"
rm -f .sfsindex/events.jsonl
SFS_ROOT="$(pwd)" SFS_PORT=8765 /tmp/sfs-server-p1 > /tmp/p1.log 2>&1 &
sleep 4
# giả lập app_open + open qua API (frontend sẽ tự bắn khi mở browser thật)
curl -s -X POST localhost:8765/api/event -H 'Content-Type: application/json' -d '{"type":"app_open"}' >/dev/null 2>&1 || python3 -c "import urllib.request,json;urllib.request.urlopen(urllib.request.Request('http://localhost:8765/api/event',data=b'{\"type\":\"app_open\"}',headers={'Content-Type':'application/json'},method='POST'))"
sleep 1
cat .sfsindex/events.jsonl
lsof -ti:8765 | xargs kill
```
Expected: `events.jsonl` có dòng `{"t":"...","type":"app_open"}`.

- [ ] **Step 4: Commit**

```bash
git add internal/webui/assets/index.html
git commit -m "feat(ui): bắn event app_open + open về behavior log"
```

---

## Task 7: Verify toàn bộ Giai đoạn 1 + gitignore

**Files:**
- Modify: `.gitignore` (events.jsonl đã trong .sfsindex/ — đã ignore, xác nhận)

- [ ] **Step 1: Xác nhận events.jsonl không bị commit**

Run: `grep -E "\.sfsindex|events" .gitignore`
Expected: `.sfsindex/` có trong .gitignore (events.jsonl nằm trong đó → tự ignore). Nếu KHÔNG, thêm `.sfsindex/` vào .gitignore.

- [ ] **Step 2: Full suite — không phá gì**

Run: `export CGO_LDFLAGS="-L$TMPDIR/sfs-libs"; ln -sfn "$(pwd)/libs" "$TMPDIR/sfs-libs"; go test ./internal/... -short -timeout 400s 2>&1 | grep -v "duplicate librar" | grep -E "^(ok|FAIL)"`
Expected: mọi package `ok`, không `FAIL`.

- [ ] **Step 3: Commit (nếu .gitignore sửa)**

```bash
git add .gitignore
git commit -m "chore: gitignore events.jsonl (behavior log không commit)"
```

---

## Definition of Done (Giai đoạn 1)

- [ ] `internal/intent/` có Event + Log (append/load/rotate), test pass.
- [ ] Chunk lưu ModTime; index set đúng từ os.Stat.
- [ ] `POST /api/event` ghi vào `.sfsindex/events.jsonl`.
- [ ] Frontend bắn `app_open` lúc load + `open` lúc click kết quả.
- [ ] events.jsonl KHÔNG commit (trong .sfsindex/, đã gitignore).
- [ ] `go test ./internal/...` toàn xanh.

**Sau Giai đoạn 1:** có dữ liệu hành vi thật trong events.jsonl + mtime mỗi file →
đủ nguyên liệu cho Giai đoạn 2 (predictor đơn giản: recency + frequency → "mở app
thấy file vừa sửa" = wow đầu tiên).
