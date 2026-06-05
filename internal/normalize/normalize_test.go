package normalize

import "testing"

// Oracle Claude. Normalize: bỏ dấu tiếng Việt + lowercase. Đ/đ -> d.
func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"Biên Bản Nghiệm Thu":  "bien ban nghiem thu",
		"Báo Giá Vật Tư":       "bao gia vat tu",
		"Đường ĐỎ":             "duong do",
		"HỢP ĐỒNG xây DỰNG":    "hop dong xay dung",
		"Bản Vẽ Kết Cấu Móng":  "ban ve ket cau mong",
		"meeting MINUTES":      "meeting minutes",
		"Dự Toán Chi Phí":      "du toan chi phi",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}
