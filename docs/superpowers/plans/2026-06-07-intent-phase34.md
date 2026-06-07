# Kế hoạch thực thi Giai đoạn 3 & 4 - Intent Engine

Tài liệu này liệt kê chi tiết các bước TDD (Test-Driven Development) nhằm thực thi GĐ3 (Nhịp thời gian) và GĐ4 (Feedback Loop/Tự học) cho hệ thống Intent Engine, tuân thủ nghiêm ngặt nguyên tắc 100% TOÁN THUẦN và 100% LOCAL.

## Giai đoạn 3: Time Profile (Nhịp thời gian)

**1. Mở rộng Profile (internal/intent/profile.go)**
- **Thiết kế**: Thêm cấu trúc `TimeProfile` vào `Profile`. Bản chất là đếm tần suất mở theo giờ cho từng file (ví dụ `map[string]map[int]int`).
- **Code**:
  - Cập nhật hàm `BuildProfile` và `BuildProfileWithEmbed` để khi gặp event `EventOpen` hoặc `EventSuggestionClick`, trích xuất `event.Time.Hour()` và tăng biến đếm tương ứng.
  - Viết test `TestProfile_TimeProfile`: tạo mảng `events` tập trung vào một khung giờ (vd: 9h sáng), chạy hàm và assert đúng số lượng count cho giờ đó. (TDD: Fail -> Code -> Pass).

**2. Tính toán điểm S_time trong Predictor (internal/intent/predictor.go)**
- **Thiết kế**: Thêm yếu tố `wTime` và tính toán hàm Gaussian.
- **Code**:
  - Thay đổi trọng số mặc định thành `wCos=0.20`, `wRec=0.60`, `wFreq=0.10`, `wTime=0.10`.
  - Trong `Predict()`, tính `S_time(f)` bằng cách:
    - Tìm `t_peak` (giờ có tần suất mở cao nhất từ `TimeProfile` của file).
    - Tính khỏang cách vòng tròn `delta = min(|now.Hour() - t_peak|, 24 - |now.Hour() - t_peak|)`.
    - `S_time = math.Exp(-(delta*delta)/(2 * sigma*sigma))` với `sigma = 2.0`.
  - Cập nhật `score` và hàm `reasonFor` (nếu S_time là mạnh nhất, báo "đúng nhịp làm việc giờ này").
  - Viết test `TestPredict_TimeMatch`: giả lập profile có time peak, gọi `Predict` vào đúng `t_peak` và kiểm tra xem điểm có tăng và xếp trên file khác không.

## Giai đoạn 4: Feedback Loop (Tự học trọng số)

**1. Quản lý Weights (internal/intent/weights.go)**
- **Thiết kế**: Trọng số không còn fix cứng mà load từ file lưu trữ (`weights.json` hoặc lưu chung vào `profile.gob`).
- **Code**:
  - Định nghĩa struct `Weights` `{Cos, Rec, Freq, Time float64}`.
  - Các hàm `DefaultWeights()`, `SaveWeights(dir, w)`, `LoadWeights(dir)`.
  - Sửa `Predict` để truyền/sử dụng cấu trúc `Weights` hiện tại thay vì hằng số.
  - Update các bài test cũ cho phù hợp.
  - Viết test lưu/đọc `Weights` đảm bảo tính nhất quán.

**2. Cơ chế Tự học Passive-Aggressive (internal/intent/learn.go)**
- **Thiết kế**: Viết hàm `Learn` áp dụng gradient đơn giản để điều chỉnh `Weights`.
- **Code**:
  - Hàm `Learn` tính toán sai số `E = margin - (Score(f+) - Score(f-))` dựa trên các feature gốc (`S_sem, S_rec, S_freq, S_time`).
  - Nếu `E > 0` thì cập nhật $w_i += \eta \cdot E \cdot (S_i(f^+) - S_i(f^-))$.
  - Ràng buộc: clamp $\ge 0.05$ và chuẩn hoá tổng $\sum w_i = 1$.
  - Cách lấy `f-` (Negative sample) chống position-bias: Từ event `suggestion_click` (có Rank), các file hiển thị trên nó (từ Rank-1 trở lên) sẽ là negatives. Do event hiện chưa lưu các file rank trên, hàm Replay sẽ tái tạo Top-K để trích xuất $f^-$ tại thời điểm đó.
  - Viết test `TestLearn_UpdateWeights`: Cố ý truyền dữ liệu để file $f^+$ có recency cực kỳ cao, file $f^-$ có cosine cao. Sau 1 vòng `Learn`, `wRec` phải TĂNG và `wCos` phải GIẢM. (TDD: Fail -> Code -> Pass).

**3. Offline Replay & Đánh giá (internal/intent/replay_test.go hoặc tool)**
- **Thiết kế**: Khép vòng lại bằng cách quét tuần tự qua `events.jsonl`, tái hiện quá trình Predict -> User action -> Learn.
- **Code**:
  - Hàm `RunReplay(events, files)`:
    - Lặp qua events. Cập nhật `Profile` online.
    - Khi gặp `app_open`, gọi `Predict(Top 5)`.
    - Tìm event `open` hoặc `suggestion_click` ngay sau đó.
    - Check file đó có trong Top 5 không -> Ghi nhận HitRate@5 và tính điểm MRR@5.
    - Sau khi biết user mở gì (và bỏ qua gì trên Top 5), gọi `Learn` điều chỉnh `Weights`.
  - In ra kết quả `% HitRate@5` và `MRR@5` sau khi Replay xong log.
  - Viết test `TestOfflineReplay` bằng bộ log mẫu nhỏ chứng minh logic tính metrics (MRR/HitRate) đúng chuẩn.

**Kết luận**: Kế hoạch chia rõ các mục nhỏ. Khi được duyệt sẽ tiến hành code test -> fail -> fix lần lượt theo các bước trên.
