# Prompt giao agy (3.1 pro) — nghiên cứu công thức điểm C + cơ chế học D

> Đây là PROMPT để dán vào agy CLI (chọn model 3.1 pro). Việc = NGHIÊN CỨU/THIẾT
> KẾ toán học, RA TÀI LIỆU, KHÔNG code. Output là 1 file markdown công thức đầy đủ.

---

## ════════ PROMPT (dán vào agy 3.1 pro) ════════

```
Bạn là chuyên gia recommendation systems. Nhiệm vụ: THIẾT KẾ công thức tính điểm
dự đoán + cơ chế tự học trọng số cho một "bộ não thấu hiểu người dùng" chạy 100%
LOCAL, TOÁN THUẦN (không LLM, không server, không thư viện ML nặng).

ĐỌC TRƯỚC: docs/superpowers/specs/2026-06-07-intent-engine-design.md (toàn bộ tầm
nhìn + 4 hệ con A/B/C/D + ràng buộc). Việc của bạn là hệ C (Predictor) + D
(Feedback Loop) — phần TOÁN.

BỐI CẢNH CỐT LÕI:
- App là semantic file search (BGE-M3, vector 1024-dim đã có sẵn cho mỗi file).
- Khoảnh khắc mục tiêu: user MỞ app → trước khi gõ, hiện top-5 file họ ĐỊNH tìm.
- Tín hiệu có: search queries, file mở (click), file mtime (vừa sửa), thời điểm
  (giờ/thứ), lịch sử bấm/bỏ qua gợi ý. TẤT CẢ ghi local trong events.jsonl.
- 100% LOCAL + TOÁN THUẦN: chỉ được dùng cosine similarity, thống kê, hàm decay,
  cập nhật online đơn giản. KHÔNG LLM, KHÔNG train neural network nặng, KHÔNG gọi mạng.
- Máy đích: laptop dân văn phòng (không GPU mạnh). Mỗi lần predict phải < 100ms.

NHIỆM VỤ — ra 1 file docs/agy-tasks/SCORING_DESIGN.md gồm:

1. CÔNG THỨC ĐIỂM đầy đủ cho mỗi file f khi user mở app:
   - 4 yếu tố tối thiểu: cosine(interest, f), recency(f), frequency(f),
     timeMatch(f, giờ hiện tại). Bạn ĐƯỢC THÊM yếu tố nếu có cơ sở (vd co-occurrence:
     file hay mở CÙNG file đang active).
   - Với MỖI yếu tố: hàm toán cụ thể (recency dùng exponential decay? half-life
     bao nhiêu? frequency dùng log hay BM25-style saturation? chuẩn hoá [0,1] thế nào?).
   - Cách KẾT HỢP: tổng có trọng số tuyến tính, hay có phi tuyến? Giải thích vì sao.
   - Chuẩn hoá điểm cuối để so sánh được giữa các file.

2. INTEREST VECTOR (hệ B, đầu vào của cosine): công thức trung bình-có-trọng-số
   các vector file/query gần đây. Hàm trọng số theo tuổi (decay) — half-life nào
   hợp lý cho "mối quan tâm hiện tại" của dân văn phòng? Có nên tách "phiên làm việc"
   (session) không?

3. COLD START: user mới chưa có hành vi → công thức nào? (gợi ý: recency thuần từ
   mtime). Khi nào chuyển dần sang công thức đầy đủ?

4. TRỌNG SỐ KHỞI TẠO w1..wN: đề xuất con số khởi tạo HỢP LÝ + LÝ DO (không phải bịa).
   Nêu rõ: đây là điểm KHỞI ĐẦU, sẽ được hệ D học lại.

5. CƠ CHẾ HỌC (hệ D) — phần QUAN TRỌNG NHẤT:
   - Tín hiệu: suggestion_click (đúng), suggestion_ignore + mở file khác (sai),
     file mở qua search mà KHÔNG được gợi ý (lẽ ra nên đoán được).
   - Thuật toán cập nhật w1..wN TOÁN THUẦN: đề xuất 1-2 cách (vd online logistic
     regression / perceptron-style update / multiplicative weights / bandit đơn
     giản). So sánh ưu nhược cho bối cảnh dữ liệu THƯA (app search dùng ít).
   - Chống overfit + chống "thiên lệch phản hồi" (chỉ học từ cái đã hiện ra —
     position bias). Cách xử lý đơn giản, không cần ML lib.
   - Learning rate / tốc độ thích nghi: nhanh hay chậm? vì sao?

6. ĐÁNH GIÁ: làm sao đo "đoán đúng" mà không cần A/B test online? (vd: offline
   replay trên events.jsonl — predict@k tại mỗi app_open, xem file user thật sự
   mở sau đó có trong top-k không). Định nghĩa metric cụ thể (recall@5, MRR...).

RÀNG BUỘC: mọi đề xuất phải IMPLEMENT ĐƯỢC bằng Go thuần + toán cơ bản, < 100ms
mỗi predict, dữ liệu THƯA (vài chục–vài trăm event). Ưu tiên ĐƠN GIẢN + GIẢI THÍCH
ĐƯỢC hơn là phức tạp. Mỗi công thức phải kèm: (a) định nghĩa toán, (b) vì sao chọn,
(c) tham số khởi tạo, (d) cách implement Go.

KHÔNG code. Chỉ ra docs/agy-tasks/SCORING_DESIGN.md. Bắt đầu bằng đọc spec rồi tóm
tắt hiểu biết trước khi thiết kế.
```

---

## Sau khi agy xong — Claude (tôi) sẽ làm gì

Tôi đọc `SCORING_DESIGN.md` agy ra, GÁC CỔNG bằng các tiêu chí:
- Mọi công thức có IMPLEMENT ĐƯỢC bằng Go thuần không? (không lén cần ML lib/LLM)
- Có chống cold-start + dữ liệu thưa + position bias không?
- Metric đánh giá có chạy offline được trên events.jsonl không?
- Có "lén" vi phạm 100%-local / toán-thuần không?

Nếu đạt → tích hợp vào spec C+D, đưa vào kế hoạch build. Nếu thiếu → đưa agy sửa.
