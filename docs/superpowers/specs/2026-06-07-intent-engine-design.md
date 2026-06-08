# Spec: "Bộ não thấu hiểu" — Intent Engine cho better-recoll

> Tài liệu thiết kế. Mục tiêu: biến app từ "tìm file" thành "thấu hiểu người dùng
> cần gì — cho họ thấy TRƯỚC KHI họ gõ". Toàn bộ 4 hệ con, 100% local, toán thuần.

Ngày: 2026-06-07. Trạng thái: thiết kế (chưa code).

---

## 0. TẦM NHÌN (một câu)

**Người dùng mở app → ô search còn trống → bên dưới đã có sẵn 3-5 file họ ĐỊNH
tìm.** Họ chưa kịp gõ, cái cần đã ở trước mắt. Không phải "tìm" — là "được thấu hiểu".

Giống TikTok: mở app là có ngay nội dung bạn muốn, chưa cần search. Nhưng tín hiệu
KHÁC TikTok: không phải "lướt/xem", mà là **công việc thật của bạn** — file bạn
đang làm, đang sửa, vào lúc nào.

---

## 1. NGUYÊN TẮC NỀN (bất biến)

1. **100% LOCAL.** Mọi hành vi + hồ sơ + dự đoán chạy trên máy. KHÔNG gửi đi đâu,
   KHÔNG gọi LLM/server ngoài. Đây vừa là triết lý (offline) vừa là điểm bán hàng
   (dân văn phòng sợ lộ dữ liệu). Hành vi là dữ liệu NHẠY NHẤT — phải bất khả xâm phạm.
2. **TOÁN THUẦN, KHÔNG LLM.** Dự đoán = cosine similarity (đã có BGE-M3) + thống
   kê hành vi (recency/frequency) + nhịp thời gian. Nhanh, nhẹ, chạy mọi máy.
   (TikTok thật cũng chủ yếu là cosine + thống kê, không phải LLM.)
3. **TÍN HIỆU TỪ CÔNG VIỆC, KHÔNG TỪ "LƯỚT".** App search dùng ít hơn TikTok nhiều
   lần. Bù lại: file đang làm/sửa = dấu vết công việc GIÀU hơn cả lướt. Khai thác cái đó.
4. **KHÔNG LÀM PHIỀN.** Gợi ý sai vẫn phải "vô hại" — chỉ hiện dưới ô search trống,
   không popup, không notification ép. User bỏ qua dễ dàng.

---

## 2. KIẾN TRÚC TỔNG (4 hệ con + dòng dữ liệu)

```
        ┌─────────────────────────────────────────────────────────┐
        │  TÍN HIỆU THÔ (đã có / dễ lấy)                            │
        │  • search queries (user gõ gì)                           │
        │  • file mở ra từ kết quả (click)                         │
        │  • file mtime (vừa sửa — từ filesystem)                  │
        │  • thời điểm mở app / search (giờ trong ngày, thứ)       │
        └───────────────────────────┬─────────────────────────────┘
                                     ↓
   ┌──────────────────┐   A. BEHAVIOR LOG (ghi hành vi, append-only)
   │ A. Behavior Log  │   events.jsonl trong .sfsindex/ — sự kiện có timestamp
   └────────┬─────────┘
            ↓
   ┌──────────────────┐   B. USER PROFILE (hồ sơ ngữ nghĩa)
   │ B. User Profile  │   • interest vector (1024-dim, trung bình có trọng số
   └────────┬─────────┘     các file/query gần đây — "bạn đang quan tâm gì")
            │               • file stats (mỗi file: lần mở, lần search trúng,
            │                 recency, mtime) — bảng điểm per-file
            │               • time profile (giờ nào hay mở file nào)
            ↓
   ┌──────────────────┐   C. PREDICTOR (dự đoán chủ động — khi MỞ app)
   │ C. Predictor     │   xếp hạng mọi file đã index theo "khả năng cần BÂY GIỜ":
   └────────┬─────────┘     score = w1·cosine(profile, file)
            │                     + w2·recency(file mtime + lần mở)
            │                     + w3·frequency(số lần đụng)
            │                     + w4·time_match(giờ này hay mở file này)
            │               → trả top-5 → hiện dưới ô search trống
            ↓
   ┌──────────────────┐   D. FEEDBACK LOOP (học từ phản ứng)
   │ D. Feedback Loop │   user bấm gợi ý = +1 (đúng); bỏ qua/search khác = tín
   └──────────────────┘     hiệu yếu. Điều chỉnh trọng số w1..w4 + per-file score.
                            → vòng sau đoán đúng hơn.
```

**Dòng chảy:** hành vi → ghi log (A) → cập nhật hồ sơ (B) → khi mở app, dự đoán
(C) → user phản ứng → học lại (D) → hồ sơ tốt hơn. Vòng lặp khép kín, hoàn toàn local.

---

## 3. CHI TIẾT TỪNG HỆ CON

### A. BEHAVIOR LOG (ghi hành vi) — nền móng, làm TRƯỚC

**Mục đích:** ghi lặng lẽ mọi sự kiện có ý nghĩa, append-only, để B/C/D dùng.

**Lưu ở:** `.sfsindex/events.jsonl` (mỗi dòng 1 JSON event). Append-only = không
sửa lịch sử, an toàn, dễ replay. Local, không bao giờ rời máy.

**Schema event:**
```json
{"t": "2026-06-07T09:15:03Z", "type": "search", "query": "vinamilk dairy"}
{"t": "2026-06-07T09:15:08Z", "type": "open", "path": "/.../Assignment-3-Vinamilk.docx", "fromQuery": "vinamilk dairy"}
{"t": "2026-06-07T09:20:00Z", "type": "app_open"}
{"t": "2026-06-07T09:21:00Z", "type": "suggestion_click", "path": "/.../report.pdf", "rank": 2}
{"t": "2026-06-07T09:22:00Z", "type": "suggestion_ignore", "shown": ["/a","/b","/c"]}
```
Loại event: `app_open`, `search`, `open` (click kết quả), `suggestion_click`,
`suggestion_ignore`. (file_modified lấy từ mtime lúc index, không cần watcher ở v1.)

**API mới:** `POST /api/event` (frontend gửi khi user mở file/bấm gợi ý). Server
append vào events.jsonl. Giới hạn kích thước: rotate khi > N events (giữ gần đây).

**Component:** `internal/intent/log.go` — `AppendEvent(e Event)`, `LoadEvents() []Event`.

---

### B. USER PROFILE (hồ sơ ngữ nghĩa) — "bạn đang quan tâm gì"

**Mục đích:** từ log → biểu diễn TOÁN HỌC điều người dùng quan tâm hiện tại.

**3 thành phần:**

1. **Interest vector (1024-dim):** trung bình CÓ TRỌNG SỐ vector của các file/query
   gần đây. Trọng số = recency (gần hơn nặng hơn, decay theo thời gian). File mở
   hôm nay nặng hơn file mở tuần trước. → "tâm điểm quan tâm" dưới dạng 1 vector.
   Tính: `interest = Σ wᵢ · vec(itemᵢ)` chuẩn hoá, wᵢ = exp(-λ·age).

2. **File stats (bảng điểm per-file):** mỗi file đã đụng tới:
   `{path, openCount, searchHitCount, lastTouched, mtime}`. Nguồn cho recency/frequency.

3. **Time profile:** histogram đơn giản — giờ-trong-ngày × file/chủ đề hay mở.
   "9h sáng hay mở assignment, 22h hay mở ghi chú cá nhân." Bắt nhịp làm việc.

**Cập nhật:** incremental sau mỗi event (rẻ). Lưu `.sfsindex/profile.gob`.

**Component:** `internal/intent/profile.go` — `Update(events)`, `InterestVector()`,
`FileStats()`, `TimeProfile()`.

---

### C. PREDICTOR (dự đoán chủ động) — KHOẢNH KHẮC "WOW"

**Mục đích:** khi user MỞ app (event `app_open`), xếp hạng mọi file đã index theo
"khả năng họ cần BÂY GIỜ", trả top-5 hiện dưới ô search trống.

**Công thức điểm (mỗi file f):**
```
score(f) = w1 · cosine(interestVector, vec(f))      // khớp ngữ nghĩa mối quan tâm
         + w2 · recency(f)                            // vừa sửa/mở gần đây
         + w3 · frequency(f)                          // hay đụng tới
         + w4 · timeMatch(f, gờ-hiện-tại)             // đúng nhịp giờ này
```
- `cosine`: dùng vector file đã có trong vindex (BGE-M3) — KHÔNG tốn thêm embed.
- `recency`: **exp decay half-life 24h** theo max(mtime, lastOpen). Tín hiệu VUA.
- `frequency`: **BM25-style saturation** `count/(count+5)` — chống file mở nhiều áp đảo.
- `timeMatch`: **Gaussian** quanh giờ-đỉnh hay mở file (σ=2h), khoảng cách giờ vòng tròn.
- **w1..w4 khởi tạo: 0.20 / 0.60 / 0.10 / 0.10** (recency nặng nhất → V1 không bao
  giờ tệ hơn "Recent Files" của OS). D tinh chỉnh per-user sau.

> **CÔNG THỨC TOÁN ĐẦY ĐỦ** (do agy 3.1 Pro nghiên cứu, đã gác cổng): hàm cụ thể
> của từng S_i, interest vector (half-life 2h = "session mềm"), cold-start tự suy
> biến, cơ chế học D (Passive-Aggressive + chống position-bias), đánh giá offline
> (HitRate@5 >60%, MRR@5 >0.4) — xem **`docs/agy-tasks/SCORING_DESIGN.md`**.

**Đầu ra:** top-5 file + lý do ngắn ("vừa sửa hôm qua", "liên quan dự án Vinamilk").
Hiện ở khối "GỢI Ý CHO BẠN" dưới ô search trống.

**API mới:** `GET /api/predict` → trả top-5 {path, score, reason}. Frontend gọi
khi load trang (ô search trống).

**Component:** `internal/intent/predictor.go` — `Predict(k int) []Prediction`.

---

### D. FEEDBACK LOOP (học từ phản ứng) — làm CUỐI

**Mục đích:** đoán sai → học → đoán đúng hơn. Khép vòng.

**Tín hiệu:**
- `suggestion_click` (user bấm gợi ý) = ĐÚNG mạnh → tăng điểm file đó + tăng trọng
  số yếu tố đã đẩy nó lên cao.
- `suggestion_ignore` rồi search/mở file khác = gợi ý SAI → giảm nhẹ.
- File được mở qua search (không qua gợi ý) = tín hiệu "lẽ ra nên đoán được nó".

**Cơ chế (toán thuần, không ML nặng):**
- Điều chỉnh w1..w4 bằng gradient đơn giản / quy tắc: nếu file được click có
  recency cao → tăng w2. Online, từng bước nhỏ.
- Per-file bonus: file hay được chọn từ gợi ý → cộng điểm thưởng nhỏ.

**Component:** `internal/intent/feedback.go` — `Learn(events)` cập nhật trọng số.

---

## 4. THAY ĐỔI HẠ TẦNG CẦN THIẾT

1. **Chunk/store thêm `mtime`:** hiện store chỉ có FilePath, không có thời gian
   sửa. Cần lưu mtime lúc index (để recency hoạt động). Sửa `store.Chunk` +
   `file_store.go`. (Thay đổi nhỏ, tương thích ngược.)
2. **Frontend gửi event:** index.html bắn `POST /api/event` khi: mở app, click
   kết quả, click/bỏ qua gợi ý. Khối UI mới "GỢI Ý CHO BẠN".
3. **Package mới `internal/intent/`:** log, profile, predictor, feedback — tách
   biệt, không đụng engine/search hiện có. Mỗi file một trách nhiệm rõ.

---

## 5. THỨ TỰ XÂY (4 giai đoạn, mỗi cái đo được)

| GĐ | Làm gì | Chứng minh được |
|----|--------|-----------------|
| **1** | A (behavior log) + store thêm mtime + frontend bắn event | events.jsonl ghi đúng hành vi |
| **2** | C-đơn-giản: predict = recency + frequency (chưa cần B đầy đủ) | mở app thấy file vừa sửa/hay mở — "wow" đầu tiên |
| **3** | B (interest vector + time profile) → C dùng cosine + timeMatch | gợi ý khớp NGỮ NGHĨA dự án, không chỉ recency |
| **4** | D (feedback loop) | gợi ý tốt dần theo thời gian dùng |

Mỗi GĐ là 1 spec→plan→build riêng. Spec này là TOÀN CẢNH; build từng GĐ.

---

## 6. RỦI RO / GIỚI HẠN (thành thật)

- **Cold start:** user mới, chưa có hành vi → gợi ý dựa recency thuần (file vừa sửa).
  Chấp nhận được — file mới sửa luôn là phỏng đoán hợp lý.
- **Tín hiệu thưa:** app search dùng ít → log mỏng. Bù bằng mtime filesystem (file
  sửa ngoài app vẫn bắt được qua re-index). v2 có thể thêm file-watcher.
- **Quyền riêng tư:** events.jsonl chứa lịch sử nhạy cảm → phải nằm trong .sfsindex
  (đã gitignore), có nút "xóa lịch sử" cho user. KHÔNG bao giờ ra mạng.
- **Đừng over-engineer:** bắt đầu w1..w4 cố định + công thức đơn giản. Knowledge
  graph đầy đủ (nối ý tưởng giữa file) là TẦM NHÌN XA, KHÔNG ở v1 — ghi nhận là
  hướng tương lai, không build vội.

---

## 7. KHÔNG LÀM (YAGNI — cắt khỏi v1)

- ❌ LLM local sinh gợi ý ngôn ngữ ("bạn đang viết X, nên làm Y") — để sau.
- ❌ Knowledge graph nối ý tưởng giữa các file — tầm nhìn xa, không v1.
- ❌ Sync đa thiết bị — 100% local trước.
- ❌ Gợi ý "khi gõ" / "khi đổi file" (tầng 2,3) — v1 chỉ "khi mở app" (tầng 1).
- ❌ File-watcher realtime — dùng mtime lúc index là đủ cho v1.
