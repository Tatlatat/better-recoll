# Spec: Cấu trúc Toán học & Thuật toán học cho Intent Engine (Hệ C & D)

Tài liệu này định nghĩa chi tiết các công thức toán học tính điểm dự đoán (Hệ C - Predictor) và cơ chế tự học trọng số (Hệ D - Feedback Loop) cho "Bộ não thấu hiểu" của better-recoll.

Mục tiêu cốt lõi: 100% TOÁN THUẦN, tính toán cực nhanh (<100ms), không dùng thư viện ML (không PyTorch/TensorFlow), xử lý tốt dữ liệu thưa thớt của ứng dụng tìm kiếm, và dễ dàng implement bằng Go thuần.

---

## 1. CÔNG THỨC TÍNH ĐIỂM DỰ ĐOÁN (Predictor - Hệ C)

Khi người dùng mở ứng dụng (app_open), hệ thống sẽ tính điểm cho mọi file $f$ trong index dựa trên 4 yếu tố chính, được kết hợp theo dạng **tổng tuyến tính có trọng số**.

**Công thức tổng quát:**
$$ Score(f) = w_1 \cdot S_{sem}(f) + w_2 \cdot S_{rec}(f) + w_3 \cdot S_{freq}(f) + w_4 \cdot S_{time}(f) $$

*Lý do chọn tổng tuyến tính:* Khớp với triết lý đơn giản, cực nhanh (O(N)), dễ giải thích (biết ngay tại sao file được đẩy lên đầu), và quan trọng nhất là đạo hàm rất dễ để áp dụng các thuật toán học online (Hệ D). Tổng điểm luôn nằm trong khoảng [0, 1] do các $S_i(f)$ và $w_i$ đã được chuẩn hoá.

### 1.1. Khớp ngữ nghĩa ($S_{sem}$)
Đo lường mức độ liên quan giữa vector của file với "Interest Vector" hiện tại.
* **Toán học:** $S_{sem}(f) = \max(0, \text{cosine\_similarity}(V_{interest}, V_f))$
* **Vì sao:** Cosine similarity trả về [-1, 1]. Dùng hàm `max(0, x)` để cắt bỏ các file trái ngược ngữ nghĩa hoàn toàn (đưa về 0), giúp chuẩn hoá điểm thành [0, 1].
* **Go Implement:** Tính dot product của hai vector (vì BGE-M3 thường đã chuẩn hoá L2). Nếu `< 0` thì trả về `0.0`.

### 1.2. Mức độ gần đây ($S_{rec}$) - Tín hiệu VUA
Đo lường khoảng thời gian từ lần cuối file được tương tác hoặc sửa đổi.
* **Toán học:** $S_{rec}(f) = \exp\left(-\lambda_{rec} \cdot \text{age}(f)\right)$
    * $\text{age}(f)$ = (Thời gian hiện tại - $\max(\text{mtime}, \text{last\_open\_time})$) tính bằng giờ.
    * $\lambda_{rec} = \ln(2) / 24$ (tương đương **half-life = 24 giờ**).
* **Vì sao:** Exponential decay (Suy giảm hàm mũ) mô phỏng chính xác sự lãng quên trong trí nhớ ngắn hạn. Với dân văn phòng, file làm việc ngày hôm qua sẽ có mức độ quan trọng bằng 1/2 file làm hôm nay. Nằm gọn trong [0, 1].
* **Go Implement:** `math.Exp(-math.Ln2 * ageHours / 24.0)`

### 1.3. Tần suất tương tác ($S_{freq}$)
Đánh giá mức độ quan trọng lâu dài của một file (ví dụ: bảng chấm công, sổ tay).
* **Toán học:** $S_{freq}(f) = \frac{\text{count}(f)}{\text{count}(f) + k}$, với $k = 5$.
* **Vì sao:** Dùng BM25-style saturation thay vì Log. Hàm này tiệm cận 1. Nó ngăn chặn tình trạng một file được mở 200 lần đè bẹp một file được mở 20 lần (sự khác biệt giữa 1 và 2 lần là cực lớn, nhưng giữa 100 và 110 lần là không đáng kể). Chuẩn hoá [0, 1).
* **Go Implement:** `float64(count) / float64(count + 5.0)`

### 1.4. Nhịp thời gian ($S_{time}$)
Phát hiện thói quen mở file theo khung giờ trong ngày.
* **Toán học:** $S_{time}(f) = \exp\left(-\frac{\Delta t^2}{2\sigma^2}\right)$
    * Giả sử từ Time Profile tìm được "giờ đỉnh" $t_{peak}$ hay mở file này (ví dụ: 9.5 tương đương 9:30 sáng).
    * Khỏang cách giờ vòng tròn: $\Delta t = \min(|t_{now} - t_{peak}|, 24 - |t_{now} - t_{peak}|)$.
    * $\sigma = 2$ giờ.
* **Vì sao:** Hàm Gaussian phân bố trơn tru, tạo hiệu ứng "mờ" quanh khung giờ quen thuộc. Dung sai +- 2 tiếng phù hợp với độ trễ công việc thực tế.
* **Go Implement:** Tìm peak hour có tần suất cao nhất của file, tính khoảng cách cyclic `delta`, rồi `math.Exp(-(delta*delta)/(2 * 4.0))`.

---

## 2. INTEREST VECTOR (Hệ B)

Interest Vector $\vec{I}$ là biểu diễn ngữ nghĩa cho "Mối quan tâm hiện tại" của user, được tính mỗi khi mở app.

* **Toán học:** $\vec{I} = \frac{\sum_{i=1}^{N} w_i \cdot \vec{v}_i}{\left\| \sum_{i=1}^{N} w_i \cdot \vec{v}_i \right\|}$
* **Trọng số decay:** $w_i = \exp\left(-\lambda_{int} \cdot \text{age}_i\right)$
    * $\text{age}_i$ là tuổi của event (search query hoặc file open) tính bằng giờ.
    * $\lambda_{int} = \ln(2) / 2$ (tương đương **half-life = 2 giờ**).
* **Vì sao chọn Half-life 2 giờ:** Context switching (đổi dự án/phiên làm việc) diễn ra nhanh. Các hành động từ 4 tiếng trước chỉ còn 1/4 giá trị so với hiện tại, tự động tạo ranh giới "session mềm" mà không cần phải dùng thuật toán cắt session cứng nhắc.
* **Go Implement:** Để giữ tốc độ `< 100ms`, chỉ duyệt qua tối đa $N = 20$ event có chứa vector gần nhất, tính tổng có trọng số, rồi L2 normalize.

---

## 3. XỬ LÝ COLD START

Khi người dùng mới cài app, chưa có lịch sử hành vi (chưa có click, chưa có search):
* $V_{interest}$ trống rỗng $\rightarrow S_{sem} = 0$.
* Tần suất mở file $= 0 \rightarrow S_{freq} = 0$.
* Time Profile $= 0 \rightarrow S_{time} = 0$.
* Điểm dự đoán tự nhiên suy biến thành: $Score(f) = w_2 \cdot S_{rec}(f)$. Trong đó $S_{rec}$ chỉ được cấu thành từ `mtime` (thời điểm file bị sửa đổi).
* **Cơ chế chuyển giao:** Không cần bất kỳ lệnh `if/else` nào chặn trạng thái "New User". Công thức tự động nội suy mượt mà. File vừa sửa sẽ lập tức lên top (đúng với kỳ vọng). Ngay sau khi user mở 1 file/search 1 lần, các tham số khác sẽ lập tức > 0 và hòa quyện vào công thức. Để điểm dễ so sánh, ta có thể tự động rescale các trọng số khả dụng sao cho tổng = 1.

---

## 4. TRỌNG SỐ KHỞI TẠO ($w_1 ... w_4$)

Vì đây là một bài toán tìm kiếm cá nhân, tín hiệu thời gian thực tế thường có độ tin cậy cao hơn hẳn tín hiệu ngữ nghĩa ban đầu. Đề xuất:

* $w_1$ (Ngữ nghĩa): **0.20** - Cần thiết nhưng dễ bị nhiễu do BGE-M3 có thể map các từ khóa chung chung thành vector giống nhau.
* $w_2$ (Gần đây - Recency): **0.60** - Khởi đầu với giả định an toàn: "Phần lớn mọi người tìm file mình vừa đụng vào". Trọng số này đảm bảo sản phẩm V1 không bao giờ tệ hơn tính năng "Recent Files" của hệ điều hành.
* $w_3$ (Tần suất): **0.10** - Hỗ trợ các file ghim/thường dùng.
* $w_4$ (Khung giờ): **0.10** - Tie-breaker (yếu tố phân định) khi có nhiều file mở cùng ngày.

*(Lưu ý: Mọi trọng số sẽ được điều chỉnh cho từng user cụ thể thông qua Hệ D).*

---

## 5. CƠ CHẾ TỰ HỌC (Feedback Loop - Hệ D)

Đây là lõi của sự thông minh. Ta sử dụng một dạng đơn giản của thuật toán **Online Gradient Descent (thuật toán Margin-based Perceptron / Passive-Aggressive)**, lý tưởng cho dữ liệu thưa và chống overfitting cực tốt.

### 5.1. Phân loại Tín hiệu
* **Positive Sample ($f^+$):** File user đã click (qua gợi ý hoặc qua việc search ngay sau đó).
* **Negative Sample ($f^-$):**
    * *Chống Position Bias:* Nếu user click vào Top 3, thì Top 1 và Top 2 chính là negative samples (vì user *đã thấy* chúng nằm trên nhưng vẫn quyết định bỏ qua).
    * Nếu user bỏ qua toàn bộ gợi ý và search một file mới, Top 3 file đã gợi ý là negative samples.

### 5.2. Công thức Cập nhật (Toán thuần)
Giả sử ta có feature vector $\vec{x}(f) = [S_{sem}, S_{rec}, S_{freq}, S_{time}]$ và trọng số $\vec{w}$.
Với mỗi cặp $(f^+, f^-)$, ta muốn: $Score(f^+) \ge Score(f^-) + margin$.

Nếu thuật toán đang sai hoặc độ tin cậy thấp (Tức là: $Score(f^+) - Score(f^-) < \text{margin}$):
1. **Tính sai số (Error):** $E = \text{margin} - (Score(f^+) - Score(f^-))$ (ví dụ margin = 0.05).
2. **Cập nhật trọng số:** 
   $$ w_i \leftarrow w_i + \eta \cdot E \cdot (S_i(f^+) - S_i(f^-)) $$
3. **Chuẩn hoá (Projection):**
   * Giới hạn $w_i \ge 0.05$ (đảm bảo không một yếu tố nào bị tắt hoàn toàn).
   * Chia mọi $w_i$ cho tổng $\sum w$ để đảm bảo $\sum w_i = 1$.

### 5.3. Tốc độ thích nghi (Learning Rate $\eta$)
* Chọn $\eta = 0.02$ (Cực kỳ nhỏ).
* **Vì sao:** Dữ liệu hành vi search là dữ liệu "thưa" (sparse) và đôi khi user hành động ngẫu nhiên/không có quy luật. Learning rate lớn sẽ khiến mô hình bị lật qua lật lại, làm user cảm thấy hệ thống mất ổn định. Ta muốn hệ thống thay đổi từ từ qua từng tuần làm việc chứ không giật cục theo từng cú click.

---

## 6. ĐÁNH GIÁ (Evaluation) - Không cần A/B Test

Để biết các công thức này hoạt động tốt hay không trên máy user mà không cần gửi dữ liệu về, ta chạy **Offline Replay** bằng Go tool nội bộ ngay trên file `events.jsonl`:

**Quy trình:**
1. Khởi tạo một Profile trống và các $w$ mặc định.
2. Quét file `events.jsonl` theo dòng thời gian.
3. Cứ mỗi khi bắt gặp `app_open`, ta "đóng băng" thời gian lại, chạy hàm `Predict()` để lấy Top 5.
4. Tìm về phía trước xem event `open` (file được mở) tiếp theo là file nào (giả sử là $f^*$).
5. So sánh $f^*$ với Top 5 đã dự đoán.
6. Feed event vào Hệ B và D để học.

**Metrics Cụ thể:**
* **HitRate@5 (Tương đương Recall@5):** Phần trăm số lần $f^*$ nằm trong danh sách Top 5 được dự đoán. Mục tiêu: > 60%. (Rất dễ hiểu: Có mặt hay không).
* **MRR@5 (Mean Reciprocal Rank):** $\sum \frac{1}{\text{rank}(f^*)} / N$. (Nếu nằm top 1 được 1 điểm, top 2 được 0.5 điểm, top 5 được 0.2 điểm, không có trong top 5 được 0 điểm). Đo lường việc file đúng có xếp *ở trên cùng* hay không. Mục tiêu: > 0.4.

Tất cả các metric này có thể in ra terminal mỗi khi user chạy `recoll-cli benchmark`, tạo ra sự minh bạch tuyệt đối về độ chính xác của AI local.
