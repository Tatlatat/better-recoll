# Nghiên cứu thị trường: Intent Engine cho better-recoll

Tài liệu này phân tích thị trường dựa trên thiết kế "Bộ não thấu hiểu" (Intent Engine) cho better-recoll. Ý tưởng cốt lõi: một công cụ tìm kiếm file **100% Local, dùng toán thuần (không LLM), dự đoán và gợi ý file TRƯỚC KHI người dùng gõ (như thuật toán gợi ý của TikTok nhưng cho file văn phòng)**.

---

## 1. Bảng So Sánh Các Dự Án / Sản Phẩm Hiện Có

| Dự án/Sản phẩm | Local hay Cloud? | Đoán trước khi gõ? | Công nghệ / Embedding | Học hành vi / Feedback? | Mã nguồn mở? | Link (đại diện) |
| --- | --- | --- | --- | --- | --- | --- |
| **Recoll / DocFetcher** | 100% Local | KHÔNG (Bị động) | Keyword, TF-IDF (Text) | KHÔNG | CÓ | [recoll.org](https://www.lesbonscomptes.com/recoll/) |
| **Khoj** | Local / Cloud | KHÔNG (Chat/RAG) | LLM, Vector Embeddings | KHÔNG (Không có vòng lặp cập nhật trọng số) | CÓ | [khoj.dev](https://khoj.dev/) |
| **Reor** | 100% Local | KHÔNG (Chat/RAG) | Local LLM, Vector Embeddings | KHÔNG | CÓ | [reorproject.org](https://www.reorproject.org/) |
| **Orama** | N/A (Framework) | CÓ THỂ (Do dev tự xây) | Vector / Text search engine | KHÔNG (Chỉ là công cụ nền tảng) | CÓ | [askorama.ai](https://askorama.ai/) |
| **Mem / Heyday** | Cloud | CÓ (Surfacing context) | Knowledge Graph, Cloud LLMs | CÓ (Học context làm việc) | KHÔNG | [mem.ai](https://mem.ai/) |
| **Rewind.ai / Limitless**| Local (Lưu) / Cloud | KHÔNG (Chỉ recall) | Screen capture, OCR, LLM | KHÔNG (Chỉ ghi hình, không tự suy luận hành vi) | KHÔNG | [rewind.ai](https://www.rewind.ai/) |
| **Ý tưởng của chúng ta**| **100% Local** | **CÓ (Proactive)** | **BGE-M3, Cosine + Recency + Frequency**| **CÓ (Click/Ignore loop)**| **CÓ** | N/A |

---

## 2. Phân Tích Khoảng Trống (Gap Analysis)

Ý tưởng của bạn là sự kết hợp của 4 yếu tố: **Local + Toán thuần + Dự đoán trước khi gõ + Gọn nhẹ cho dân văn phòng**. Đã có ai làm ĐÚNG combo này chưa?
**Câu trả lời là: CHƯA CÓ.** Đây là một khoảng trống lớn ("Đại dương xanh") vì sự phát triển của công nghệ đang bị phân cực:

1. **Cực 1: Các công cụ search truyền thống (Recoll, Spotlight, Everything):** Rất nhẹ, 100% local, nhưng quá "ngu ngốc" theo nghĩa bị động. Bạn không gõ chính xác keyword, nó không tìm ra. Chúng không hề biết bạn là ai, thói quen của bạn là gì.
2. **Cực 2: Trào lưu AI "Second Brain" (Khoj, Reor, Mem):** Khi thị trường muốn làm cho search thông minh hơn, tất cả đều nhảy thẳng vào việc **tích hợp LLM (Mô hình ngôn ngữ lớn) để làm Chatbot (RAG)**. Việc chạy LLM ở local cực kỳ tốn RAM và GPU, không phù hợp cho "dân văn phòng" xài laptop thường. Nếu đẩy lên Cloud (Mem, ChatGPT) thì lại vi phạm nguyên tắc bảo mật thông tin nội bộ của công ty.

**Điểm khác biệt tuyệt đối của ý tưởng:**
Bạn đang đem nguyên lý của **Hệ thống Gợi ý (Recommendation System) kiểu TikTok (Vector Similarity + Thống kê tần suất/thời gian)** áp dụng vào tệp tin cá nhân ở môi trường Offline. Thay vì bắt máy tính "đọc hiểu" bằng LLM nặng nề, bạn dùng "Toán thuần" (Ghi nhận hành vi -> Vector hóa -> Tính khoảng cách Cosine -> Trọng số Recency/Frequency). Cấu trúc này vừa đủ thông minh để tạo khoảnh khắc "WOW" (mở app là thấy file cần tìm), vừa đủ nhẹ để chạy ngầm trên một chiếc laptop i5 RAM 8GB.

---

## 3. Bài Học Rút Ra Từ Các Hệ Thống Tương Tự

Từ việc nghiên cứu kiến trúc của Recommendation Systems phi LLM và các dự án mã nguồn mở, chúng ta có thể áp dụng các kỹ thuật sau:

- **Công thức Recency (Chống Cold-start & Tính mới):** Đa số các hệ thống dùng hàm suy giảm mũ (Exponential Decay) cho Recency. VD: Trọng số của một file $i$ sẽ bằng $e^{-\lambda \cdot \Delta t}$, với $\Delta t$ là thời gian từ lần cuối truy cập. Điều này giải quyết cold-start: user mới chưa có vector sở thích sẽ được gợi ý các file vừa sửa.
- **Tính toán Vector cực nhanh (Local):** Thay vì tự viết thuật toán duyệt mảng để tính Cosine Similarity, cộng đồng thường dùng **Faiss** (của Meta) hoặc các lightweight vector-db để tính toán khoảng cách vector trong vài mili-giây.
- **Tránh bão hòa Tần suất (Frequency Saturation):** Nếu dùng biến đếm số lần mở (Frequency) thuần túy, những file mở 1000 lần từ 3 năm trước sẽ đè bẹp file mới mở 5 lần tuần này. Do đó, cần áp dụng hàm kiểu BM25 bão hòa: `count / (count + k)` (như thiết kế spec đã đề cập) để khống chế sức mạnh của Frequency.
- **Hệ số Học (Feedback Loop):** Thuật toán Passive-Aggressive là một lựa chọn kinh điển để điều chỉnh các trọng số $(w_1, w_2, w_3, w_4)$ dựa trên click. Nếu người dùng click vào file có Recency cao nhưng Cosine thấp, tăng $w_{recency}$.

---

## 4. Rủi Ro Tiềm Ẩn

Có lý do vì sao một số dự án không đi theo hướng này hoặc gặp khó khăn:

1. **Tín hiệu thưa (Data Sparsity):** Khác với TikTok (nơi người dùng lướt và tạo tín hiệu mỗi 5 giây), người dùng desktop mở file ít hơn rất nhiều. Profile có thể sẽ "nghèo nàn" và cập nhật chậm, khiến dự đoán ban đầu chỉ loanh quanh ở "những file vừa sửa" (Recency) thay vì thực sự "hiểu" (Cosine).
2. **Nhiễu Ngữ cảnh (Context Switching):** Một người đang làm báo cáo tài chính buổi sáng, chiều nhảy sang code dự án A. Interest vector sẽ bị kéo giãn ra giữa hai mảng nội dung hoàn toàn khác nhau, dẫn đến gợi ý bị pha loãng (hệ thống có thể gợi ý file code xen lẫn báo cáo).
3. **Mô hình "Desktop" đang thoái trào:** Nhiều công cụ (như Heyday, Mem) chuyển hẳn lên web/cloud vì dữ liệu của con người ngày nay nằm trên Google Drive, Notion, Slack nhiều hơn là file Word/PDF dưới ổ cứng local. Sự giới hạn 100% local đồng nghĩa với việc bạn bỏ qua một lượng lớn "hành vi" trên trình duyệt.

**Kết luận:** Ý tưởng này không hề bị "đã chết" hay bị bỏ đi. Nó là một **ngách chưa ai chạm tới (untapped niche)** do sự bùng nổ của LLM làm lu mờ đi các kỹ thuật Recommendation System truyền thống cho local files. Nếu giải quyết tốt vấn đề "tín hiệu thưa" bằng các event ngầm (mtime), đây sẽ là một sản phẩm độc đáo.
