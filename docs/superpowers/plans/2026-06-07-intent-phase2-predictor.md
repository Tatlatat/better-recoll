# Intent Engine — Giai đoạn 2: Predictor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development hoặc superpowers:executing-plans. Steps dùng checkbox (`- [ ]`).

**Goal:** Khi user mở app (ô search trống), hiện top-5 file họ có khả năng cần BÂY GIỜ — xếp hạng bằng công thức toán thuần (cosine khớp mối quan tâm + recency + frequency). "Khoảnh khắc wow": mở app là thấy file cần.

**Architecture:** Package `internal/intent` thêm `profile.go` (interest vector từ events) + `predictor.go` (công thức điểm). Store thêm `FileEntries()` gom chunk→file (path + vector trung bình + mtime). API `GET /api/predict` trả top-5. UI hiện khối "GỢI Ý CHO BẠN" khi ô search trống. 100% local, toán thuần, theo `docs/agy-tasks/SCORING_DESIGN.md`.

**Tech Stack:** Go (math, sort), HTTP handler webui, vanilla JS. Build cần CGO symlink (path khoảng trắng).

> **LƯU Ý BUILD/TEST:** TRƯỚC mọi lệnh go: `export CGO_LDFLAGS="-L$TMPDIR/sfs-libs"; ln -sfn "/Users/tatlatat/Documents/level 3/libs" "$TMPDIR/sfs-libs"`. Test intent/store nhanh (không model); test webui/engine cần model (chậm).

> **CÔNG THỨC GỐC:** `docs/agy-tasks/SCORING_DESIGN.md`. GĐ2 làm phần CỐT LÕI: cosine + recency (half-life 24h) + frequency (BM25 saturation). Bỏ qua time-profile (Gaussian) và feedback-loop (Passive-Aggressive) — để GĐ3/4. Trọng số CỐ ĐỊNH: w_cos=0.20, w_rec=0.60, w_freq=0.20 (gộp w_time vào freq cho v2).

---

## File Structure

| File | Trách nhiệm | Create/Modify |
|------|-------------|---------------|
| `internal/store/file_store.go` | Thêm `FileEntries()` — gom chunk thành file (path, vector TB, mtime) | Modify |
| `internal/store/file_store_test.go` | Test FileEntries gom đúng | Create (hoặc thêm) |
| `internal/intent/profile.go` | InterestVector từ events + FileStat (open count) | Create |
| `internal/intent/profile_test.go` | Test interest vector + file stats | Create |
| `internal/intent/predictor.go` | Predict(entries, profile, k) → top-k theo công thức | Create |
| `internal/intent/predictor_test.go` | Test xếp hạng đúng (recency/cosine/frequency) | Create |
| `internal/webui/webui.go` | Handler `handlePredict` + route `/api/predict` | Modify |
| `internal/webui/assets/index.html` | Hiện gợi ý khi ô search trống + bắn suggestion_click | Modify |

---

## Task 1: Store.FileEntries — gom chunk thành file

**Files:**
- Modify: `internal/store/file_store.go`
- Create: `internal/store/fileentries_test.go`

- [ ] **Step 1: Viết test (failing)**

Create `internal/store/fileentries_test.go`:

```go
package store

import (
	"testing"
)

func TestFileEntriesGroupsByPath(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 2 chunk cùng file A, 1 chunk file B
	fs.Add([]Chunk{
		{ID: 1, FilePath: "/a.txt", Vector: []float32{1, 0}, ModTime: 100},
		{ID: 2, FilePath: "/a.txt", Vector: []float32{0, 1}, ModTime: 100},
		{ID: 3, FilePath: "/b.txt", Vector: []float32{1, 1}, ModTime: 200},
	})

	entries := fs.FileEntries()
	if len(entries) != 2 {
		t.Fatalf("muốn 2 file, được %d", len(entries))
	}
	// tìm file A
	var a *FileEntry
	for i := range entries {
		if entries[i].Path == "/a.txt" {
			a = &entries[i]
		}
	}
	if a == nil {
		t.Fatal("không thấy /a.txt")
	}
	// vector trung bình của A = ([1,0]+[0,1])/2 = [0.5,0.5]
	if a.Vector[0] != 0.5 || a.Vector[1] != 0.5 {
		t.Fatalf("vector TB sai: %v", a.Vector)
	}
	if a.ModTime != 100 {
		t.Fatalf("mtime sai: %d", a.ModTime)
	}
}
```

LƯU Ý: nếu `fs.Add` chưa nhận `[]Chunk`, kiểm chữ ký thật trong file_store.go (grep `func (fs *FileStore) Add`). Nếu Add nhận từng chunk hoặc tên khác, sửa test gọi cho đúng chữ ký hiện có.

- [ ] **Step 2: Chạy test — phải FAIL**

Run: `export CGO_LDFLAGS="-L$TMPDIR/sfs-libs"; ln -sfn "/Users/tatlatat/Documents/level 3/libs" "$TMPDIR/sfs-libs"; go test ./internal/store/ -run TestFileEntries 2>&1 | grep -v "duplicate librar"`
Expected: FAIL — `FileEntry`/`FileEntries` undefined.

- [ ] **Step 3: Thêm FileEntry + FileEntries vào file_store.go**

Thêm vào `internal/store/file_store.go`:

```go
// FileEntry là một FILE (gom từ nhiều chunk): vector trung bình của các chunk +
// mtime mới nhất. Dùng cho predictor (xếp hạng theo file, không theo chunk).
type FileEntry struct {
	Path    string
	Vector  []float32 // trung bình L2-chưa-chuẩn-hoá của các chunk vector
	ModTime int64     // mtime lớn nhất trong các chunk của file
}

// FileEntries gom các chunk thành danh sách file duy nhất. Vector mỗi file =
// trung bình vector các chunk (đại diện ngữ nghĩa toàn file). ModTime = max.
func (fs *FileStore) FileEntries() []FileEntry {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	type acc struct {
		sum     []float32
		n       int
		modTime int64
	}
	byPath := make(map[string]*acc)
	for _, c := range fs.chunks {
		a := byPath[c.FilePath]
		if a == nil {
			a = &acc{sum: make([]float32, len(c.Vector))}
			byPath[c.FilePath] = a
		}
		for i := range c.Vector {
			if i < len(a.sum) {
				a.sum[i] += c.Vector[i]
			}
		}
		a.n++
		if c.ModTime > a.modTime {
			a.modTime = c.ModTime
		}
	}
	out := make([]FileEntry, 0, len(byPath))
	for path, a := range byPath {
		vec := make([]float32, len(a.sum))
		for i := range a.sum {
			vec[i] = a.sum[i] / float32(a.n)
		}
		out = append(out, FileEntry{Path: path, Vector: vec, ModTime: a.modTime})
	}
	return out
}
```

- [ ] **Step 4: Chạy test — phải PASS**

Run: `export CGO_LDFLAGS="-L$TMPDIR/sfs-libs"; ln -sfn "/Users/tatlatat/Documents/level 3/libs" "$TMPDIR/sfs-libs"; go test ./internal/store/ -run TestFileEntries -v 2>&1 | grep -v "duplicate librar" | grep -E "PASS|FAIL|ok"`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/file_store.go internal/store/fileentries_test.go
git commit -m "feat(store): FileEntries gom chunk thành file (vector TB + mtime)"
```

---

## Task 2: Profile — InterestVector + FileStats từ events

**Files:**
- Create: `internal/intent/profile.go`
- Create: `internal/intent/profile_test.go`

- [ ] **Step 1: Viết test (failing)**

Create `internal/intent/profile_test.go`:

```go
package intent

import (
	"testing"
	"time"
)

func TestFileStatsCountsOpens(t *testing.T) {
	now := time.Now()
	events := []Event{
		{Time: now, Type: EventOpen, Path: "/a.txt"},
		{Time: now, Type: EventOpen, Path: "/a.txt"},
		{Time: now, Type: EventOpen, Path: "/b.txt"},
	}
	p := BuildProfile(events, now)
	if p.FileStats["/a.txt"].OpenCount != 2 {
		t.Fatalf("a phải mở 2 lần, được %d", p.FileStats["/a.txt"].OpenCount)
	}
	if p.FileStats["/b.txt"].OpenCount != 1 {
		t.Fatalf("b phải mở 1 lần, được %d", p.FileStats["/b.txt"].OpenCount)
	}
}

func TestInterestVectorWeightsRecent(t *testing.T) {
	now := time.Now()
	// 2 search: 1 mới (vector A), 1 cũ 10h (vector B). Interest phải nghiêng về A.
	events := []Event{
		{Time: now.Add(-10 * time.Hour), Type: EventSearch, Query: "cũ"},
		{Time: now, Type: EventSearch, Query: "mới"},
	}
	// embedFn giả: "mới"→[1,0], "cũ"→[0,1]
	embed := func(q string) []float32 {
		if q == "mới" {
			return []float32{1, 0}
		}
		return []float32{0, 1}
	}
	p := BuildProfileWithEmbed(events, now, embed)
	// interest[0] (thành phần "mới") phải > interest[1] (thành phần "cũ")
	if p.InterestVector[0] <= p.InterestVector[1] {
		t.Fatalf("interest phải nghiêng về 'mới': %v", p.InterestVector)
	}
}
```

- [ ] **Step 2: Chạy test — phải FAIL**

Run: `export CGO_LDFLAGS="-L$TMPDIR/sfs-libs"; ln -sfn "/Users/tatlatat/Documents/level 3/libs" "$TMPDIR/sfs-libs"; go test ./internal/intent/ -run "TestFileStats|TestInterest" 2>&1 | grep -v "duplicate librar"`
Expected: FAIL — `BuildProfile` undefined.

- [ ] **Step 3: Viết profile.go**

```go
package intent

import (
	"math"
	"time"
)

// FileStat là thống kê per-file (cho frequency/recency).
type FileStat struct {
	OpenCount  int
	LastOpened time.Time
}

// Profile là hồ sơ ngữ nghĩa người dùng tại 1 thời điểm.
type Profile struct {
	InterestVector []float32            // tâm điểm quan tâm (trung bình có trọng số, decay)
	FileStats      map[string]*FileStat // path -> thống kê
}

// interestHalfLife = 2h: hành động cũ hơn nhanh chóng mất giá trị ("session mềm").
const interestHalfLifeHours = 2.0

// BuildProfile xây profile từ events. KHÔNG có interest vector (cần embed) — chỉ
// file stats. Dùng khi không cần ngữ nghĩa (vd test, hoặc predictor recency-only).
func BuildProfile(events []Event, now time.Time) Profile {
	return BuildProfileWithEmbed(events, now, nil)
}

// BuildProfileWithEmbed xây profile đầy đủ. embed(query) trả vector cho 1 search
// query (nil = bỏ qua interest vector). Trọng số mỗi event = exp(-ln2 * age/2h).
func BuildProfileWithEmbed(events []Event, now time.Time, embed func(string) []float32) Profile {
	p := Profile{FileStats: make(map[string]*FileStat)}
	lambda := math.Ln2 / interestHalfLifeHours

	var sum []float32
	for _, e := range events {
		switch e.Type {
		case EventOpen, EventSuggestionClick:
			if e.Path != "" {
				st := p.FileStats[e.Path]
				if st == nil {
					st = &FileStat{}
					p.FileStats[e.Path] = st
				}
				st.OpenCount++
				if e.Time.After(st.LastOpened) {
					st.LastOpened = e.Time
				}
			}
		case EventSearch:
			if embed != nil && e.Query != "" {
				v := embed(e.Query)
				ageH := now.Sub(e.Time).Hours()
				if ageH < 0 {
					ageH = 0
				}
				w := float32(math.Exp(-lambda * ageH))
				if sum == nil {
					sum = make([]float32, len(v))
				}
				for i := range v {
					if i < len(sum) {
						sum[i] += w * v[i]
					}
				}
			}
		}
	}
	p.InterestVector = l2normalize(sum)
	return p
}

func l2normalize(v []float32) []float32 {
	if len(v) == 0 {
		return v
	}
	var n float64
	for _, x := range v {
		n += float64(x) * float64(x)
	}
	n = math.Sqrt(n)
	if n == 0 {
		return v
	}
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(float64(x) / n)
	}
	return out
}
```

- [ ] **Step 4: Chạy test — phải PASS**

Run: `export CGO_LDFLAGS="-L$TMPDIR/sfs-libs"; ln -sfn "/Users/tatlatat/Documents/level 3/libs" "$TMPDIR/sfs-libs"; go test ./internal/intent/ -run "TestFileStats|TestInterest" -v 2>&1 | grep -v "duplicate librar" | grep -E "PASS|FAIL|ok"`
Expected: PASS cả 2.

- [ ] **Step 5: Commit**

```bash
git add internal/intent/profile.go internal/intent/profile_test.go
git commit -m "feat(intent): Profile — interest vector (decay 2h) + file stats"
```

---

## Task 3: Predictor — công thức điểm + xếp hạng

**Files:**
- Create: `internal/intent/predictor.go`
- Create: `internal/intent/predictor_test.go`

- [ ] **Step 1: Viết test (failing)**

Create `internal/intent/predictor_test.go`:

```go
package intent

import (
	"testing"
	"time"
)

// File vừa sửa (recency cao) phải xếp trên file cũ, khi profile trống (cold start).
func TestPredictRecencyWins(t *testing.T) {
	now := time.Now()
	files := []FileCandidate{
		{Path: "/cũ.txt", Vector: []float32{1, 0}, ModTime: now.Add(-100 * time.Hour).Unix()},
		{Path: "/mới.txt", Vector: []float32{1, 0}, ModTime: now.Add(-1 * time.Hour).Unix()},
	}
	prof := Profile{FileStats: map[string]*FileStat{}} // cold start
	got := Predict(files, prof, now, 2)
	if len(got) != 2 || got[0].Path != "/mới.txt" {
		t.Fatalf("file mới phải top-1, được: %+v", got)
	}
}

// Khi có interest vector, file khớp ngữ nghĩa được cộng điểm.
func TestPredictCosineHelps(t *testing.T) {
	now := time.Now()
	mt := now.Add(-50 * time.Hour).Unix() // cùng recency
	files := []FileCandidate{
		{Path: "/khớp.txt", Vector: []float32{1, 0}, ModTime: mt},
		{Path: "/lệch.txt", Vector: []float32{0, 1}, ModTime: mt},
	}
	prof := Profile{
		InterestVector: []float32{1, 0}, // quan tâm hướng [1,0]
		FileStats:      map[string]*FileStat{},
	}
	got := Predict(files, prof, now, 2)
	if got[0].Path != "/khớp.txt" {
		t.Fatalf("file khớp ngữ nghĩa phải top-1, được: %+v", got)
	}
}
```

- [ ] **Step 2: Chạy test — phải FAIL**

Run: `export CGO_LDFLAGS="-L$TMPDIR/sfs-libs"; ln -sfn "/Users/tatlatat/Documents/level 3/libs" "$TMPDIR/sfs-libs"; go test ./internal/intent/ -run TestPredict 2>&1 | grep -v "duplicate librar"`
Expected: FAIL — `FileCandidate`/`Predict` undefined.

- [ ] **Step 3: Viết predictor.go**

```go
package intent

import (
	"math"
	"sort"
	"time"
)

// FileCandidate là 1 file ứng viên để xếp hạng (từ store.FileEntries).
type FileCandidate struct {
	Path    string
	Vector  []float32
	ModTime int64 // Unix giây
}

// Prediction là 1 file được dự đoán + điểm + lý do ngắn.
type Prediction struct {
	Path   string  `json:"path"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

// Trọng số cố định cho GĐ2 (xem SCORING_DESIGN; w_time gộp vào sau).
const (
	wCos      = 0.20
	wRec      = 0.60
	wFreq     = 0.20
	recHalfLifeHours = 24.0 // recency half-life 24h
	freqK            = 5.0  // BM25 saturation
)

// Predict xếp hạng files theo công thức toán thuần, trả top-k.
// score = wCos·cosine(interest, file) + wRec·recency + wFreq·frequency.
func Predict(files []FileCandidate, prof Profile, now time.Time, k int) []Prediction {
	if k <= 0 {
		return nil
	}
	lambda := math.Ln2 / recHalfLifeHours
	preds := make([]Prediction, 0, len(files))

	for _, f := range files {
		// recency: exp decay theo max(mtime, lastOpened)
		ref := f.ModTime
		if st := prof.FileStats[f.Path]; st != nil && !st.LastOpened.IsZero() {
			if lo := st.LastOpened.Unix(); lo > ref {
				ref = lo
			}
		}
		ageH := now.Sub(time.Unix(ref, 0)).Hours()
		if ageH < 0 {
			ageH = 0
		}
		sRec := math.Exp(-lambda * ageH)

		// frequency: BM25 saturation count/(count+k)
		var sFreq float64
		if st := prof.FileStats[f.Path]; st != nil {
			c := float64(st.OpenCount)
			sFreq = c / (c + freqK)
		}

		// cosine: dot product (vector đã ~chuẩn hoá); max(0,·)
		var sCos float64
		if len(prof.InterestVector) > 0 {
			var dot float64
			for i := range f.Vector {
				if i < len(prof.InterestVector) {
					dot += float64(f.Vector[i]) * float64(prof.InterestVector[i])
				}
			}
			if dot > 0 {
				sCos = dot
			}
		}

		score := wCos*sCos + wRec*sRec + wFreq*sFreq
		preds = append(preds, Prediction{
			Path:   f.Path,
			Score:  score,
			Reason: reasonFor(sRec, sFreq, sCos),
		})
	}

	sort.SliceStable(preds, func(i, j int) bool { return preds[i].Score > preds[j].Score })
	if len(preds) > k {
		preds = preds[:k]
	}
	return preds
}

// reasonFor trả lý do ngắn dựa yếu tố trội nhất.
func reasonFor(rec, freq, cos float64) string {
	if rec >= freq && rec >= cos {
		return "vừa truy cập gần đây"
	}
	if freq >= cos {
		return "bạn hay mở file này"
	}
	return "liên quan việc bạn đang làm"
}
```

- [ ] **Step 4: Chạy test — phải PASS**

Run: `export CGO_LDFLAGS="-L$TMPDIR/sfs-libs"; ln -sfn "/Users/tatlatat/Documents/level 3/libs" "$TMPDIR/sfs-libs"; go test ./internal/intent/ -run TestPredict -v 2>&1 | grep -v "duplicate librar" | grep -E "PASS|FAIL|ok"`
Expected: PASS cả 2.

- [ ] **Step 5: Commit**

```bash
git add internal/intent/predictor.go internal/intent/predictor_test.go
git commit -m "feat(intent): Predictor — cosine + recency + frequency (toán thuần)"
```

---

## Task 4: API GET /api/predict

**Files:**
- Modify: `internal/webui/webui.go`

- [ ] **Step 1: Thêm handler + route**

Trong `internal/webui/webui.go`, thêm handler. Engine cần expose FileEntries + embed query — kiểm: engine có method trả `store.FileEntries` không. Nếu chưa, thêm method `FileEntries()` vào engine (trả `e.store.FileEntries()`) và `EmbedQuery(q string) []float32` (dùng e.embedder). Grep `func (e *Engine)` để xem đã có chưa.

Thêm vào engine.go (nếu chưa có):

```go
// FileEntries trả danh sách file đã index (cho predictor).
func (e *Engine) FileEntries() []store.FileEntry {
	return e.store.FileEntries()
}

// EmbedQuery embed 1 query thành vector (cho interest vector). Lỗi → nil.
func (e *Engine) EmbedQuery(q string) []float32 {
	vecs, err := e.embedder.Embed([]string{q})
	if err != nil || len(vecs) == 0 {
		return nil
	}
	return vecs[0]
}
```

Handler trong webui.go:

```go
// handlePredict trả top-5 file dự đoán cho ô search trống (khoảnh khắc "wow").
func handlePredict(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	engineMutex.RLock()
	eng := globalEngine
	engineMutex.RUnlock()
	if eng == nil || behaviorLog == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]intent.Prediction{})
		return
	}

	events, _ := behaviorLog.Load()
	now := time.Now()
	prof := intent.BuildProfileWithEmbed(events, now, eng.EmbedQuery)

	entries := eng.FileEntries()
	cands := make([]intent.FileCandidate, 0, len(entries))
	for _, fe := range entries {
		cands = append(cands, intent.FileCandidate{Path: fe.Path, Vector: fe.Vector, ModTime: fe.ModTime})
	}

	preds := intent.Predict(cands, prof, now, 5)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(preds)
}
```

Đăng ký route (cạnh `/api/event`):

```go
	mux.HandleFunc("/api/predict", handlePredict)
```

- [ ] **Step 2: Build + verify route**

Run: `export CGO_LDFLAGS="-L$TMPDIR/sfs-libs"; ln -sfn "/Users/tatlatat/Documents/level 3/libs" "$TMPDIR/sfs-libs"; go build ./... 2>&1 | grep -v "duplicate librar" | grep -iE "error|cannot" || echo BUILD_OK`
Expected: BUILD_OK.

- [ ] **Step 3: Verify end-to-end (cần index có sẵn)**

```bash
export CGO_LDFLAGS="-L$TMPDIR/sfs-libs"; ln -sfn "/Users/tatlatat/Documents/level 3/libs" "$TMPDIR/sfs-libs"
go build -o /tmp/sfs-p2 ./cmd/sfs-server 2>&1 | grep -v "duplicate librar" | grep -iE error || echo "build OK"
SFS_ROOT="$(pwd)" SFS_MODELS_DIR="$(pwd)" SFS_PORT=8765 /tmp/sfs-p2 > /tmp/p2.log 2>&1 &
sleep 5
python3 -c "import urllib.request,json;print(json.load(urllib.request.urlopen('http://localhost:8765/api/predict',timeout=20)))"
lsof -ti:8765 | xargs kill
```
Expected: JSON array (rỗng nếu index trống, hoặc top-5 {path,score,reason} nếu có index). KHÔNG lỗi 500.

- [ ] **Step 4: Commit**

```bash
git add internal/webui/webui.go internal/engine/engine.go
git commit -m "feat(webui): GET /api/predict trả top-5 file dự đoán"
```

---

## Task 5: UI hiện gợi ý khi ô search trống

**Files:**
- Modify: `internal/webui/assets/index.html`

- [ ] **Step 1: Gọi /api/predict khi ô search trống + render gợi ý**

Trong `<script>`, thêm hàm load + render gợi ý. Tìm `updateEmptyState(query)` (chỗ xử lý ô trống). Khi query rỗng, thay vì chỉ hiện "Nhập từ khóa...", gọi predict:

```javascript
        // Tải + hiện gợi ý chủ động khi ô search trống ("mở app là thấy file cần").
        async function loadSuggestions() {
            try {
                const r = await fetch('/api/predict');
                const preds = await r.json();
                renderSuggestions(preds);
            } catch (e) { /* im lặng nếu lỗi */ }
        }

        function renderSuggestions(preds) {
            const emptyState = document.getElementById('empty-state');
            if (!preds || preds.length === 0) {
                emptyState.innerHTML = '<span class="text-sm opacity-75">Nhập từ khóa để tìm tài liệu</span>';
                return;
            }
            const items = preds.map((p, i) => {
                const name = p.path.split(/[/\\]/).pop()
                    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
                const safePath = p.path
                    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#039;');
                const reason = (p.reason || '')
                    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
                return `<div data-suggest-path="${safePath}" data-suggest-rank="${i + 1}" class="flex items-center gap-2 p-2 rounded hover:bg-base-200 cursor-pointer">
                    <span class="font-medium truncate">${name}</span>
                    <span class="text-xs opacity-60">${reason}</span>
                </div>`;
            }).join('');
            emptyState.innerHTML = `<div class="text-xs font-semibold opacity-50 mb-1 px-2">GỢI Ý CHO BẠN</div>${items}`;
        }
```

Sửa `updateEmptyState`: khi `!query` (ô trống), gọi `loadSuggestions()` thay vì set text cứng. Tìm trong updateEmptyState nhánh `if (!query)` và đổi thành:

```javascript
            if (!query) {
                loadSuggestions();
                return;
            }
```

Và gọi `loadSuggestions()` 1 lần lúc trang load (cạnh `sendEvent({type:'app_open'})`):

```javascript
        loadSuggestions();
```

- [ ] **Step 2: Bắn suggestion_click khi user bấm 1 gợi ý**

Thêm event delegation (cạnh delegation 'open' đã có từ GĐ1):

```javascript
        // Click 1 gợi ý chủ động → ghi suggestion_click + mở (search theo tên).
        document.addEventListener('click', function (e) {
            const sug = e.target.closest('[data-suggest-path]');
            if (sug) {
                sendEvent({
                    type: 'suggestion_click',
                    path: sug.getAttribute('data-suggest-path'),
                    rank: parseInt(sug.getAttribute('data-suggest-rank') || '0', 10)
                });
                // mở chi tiết file: điền tên vào ô search để user thấy ngay
                const input = document.getElementById('search-input');
                if (input) {
                    input.value = sug.getAttribute('data-suggest-path').split(/[/\\]/).pop();
                    input.dispatchEvent(new Event('input'));
                }
            }
        });
```

- [ ] **Step 3: Verify thủ công**

```bash
export CGO_LDFLAGS="-L$TMPDIR/sfs-libs"; ln -sfn "/Users/tatlatat/Documents/level 3/libs" "$TMPDIR/sfs-libs"
go build -o /tmp/sfs-p2 ./cmd/sfs-server 2>&1 | grep -v "duplicate librar" | grep -iE error || echo "build OK"
```
(Mở browser http://localhost:8765 với index có sẵn → ô search trống phải hiện "GỢI Ý CHO BẠN" + danh sách file. Vì Claude "mù" GUI, người review xác nhận bằng mắt hoặc screenshot Playwright.)

- [ ] **Step 4: Commit**

```bash
git add internal/webui/assets/index.html
git commit -m "feat(ui): hiện gợi ý chủ động khi ô search trống (mở app thấy file cần)"
```

---

## Task 6: Verify toàn bộ Giai đoạn 2

- [ ] **Step 1: Full suite — không phá gì**

Run: `export CGO_LDFLAGS="-L$TMPDIR/sfs-libs"; ln -sfn "/Users/tatlatat/Documents/level 3/libs" "$TMPDIR/sfs-libs"; go test ./internal/... -short -timeout 400s 2>&1 | grep -v "duplicate librar" | grep -E "^(ok|FAIL)"`
Expected: mọi package `ok`, không `FAIL`.

- [ ] **Step 2: Vet sạch**

Run: `export CGO_LDFLAGS="-L$TMPDIR/sfs-libs"; ln -sfn "/Users/tatlatat/Documents/level 3/libs" "$TMPDIR/sfs-libs"; go vet ./internal/intent/ ./internal/store/ ./internal/webui/ 2>&1 | grep -v "duplicate librar" | grep "\.go:" || echo "VET OK"`
Expected: VET OK.

---

## Definition of Done (Giai đoạn 2)

- [ ] `store.FileEntries()` gom chunk→file (vector TB + mtime), test pass.
- [ ] `intent.BuildProfile/Predict` — công thức cosine+recency+frequency, test pass.
- [ ] `GET /api/predict` trả top-5 {path,score,reason}, không lỗi.
- [ ] UI hiện "GỢI Ý CHO BẠN" khi ô search trống; bắn suggestion_click khi bấm.
- [ ] Cold start: index có file nhưng chưa có hành vi → gợi ý file vừa sửa (recency).
- [ ] `go test ./internal/...` toàn xanh.

**Sau GĐ2:** mở app là thấy file cần (recency + ngữ nghĩa). Khoảnh khắc "wow" đầu
tiên. GĐ3 (time profile) + GĐ4 (feedback loop học trọng số) làm sau.
