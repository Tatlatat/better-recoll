package dedupe

import "testing"

// Oracle Claude. DedupeFinder tìm đoạn-khung-chung: đoạn xuất hiện ở NHIỀU
// file (template) bị đánh dấu boilerplate; đoạn riêng của từng file thì không.
func TestFindBoilerplate(t *testing.T) {
	// 3 "file", mỗi file = list đoạn. Đoạn header giống nhau ở cả 3 = khung chung.
	header := "CONG TY XAY DUNG ABC - DIA CHI 123 - DIEN THOAI 0900"
	docs := [][]string{
		{header, "bien ban nghiem thu hang muc mong"},
		{header, "bien ban nghiem thu hang muc than"},
		{header, "bao gia vat tu xi mang sat thep"},
	}
	d := New(2) // ngưỡng: đoạn xuất hiện ở >=2 file là boilerplate
	for _, doc := range docs {
		for _, seg := range doc {
			d.Add(seg)
		}
	}
	d.Build()

	if !d.IsBoilerplate(header) {
		t.Errorf("header (xuất hiện 3 file) phải là boilerplate")
	}
	if d.IsBoilerplate("bien ban nghiem thu hang muc mong") {
		t.Errorf("đoạn riêng (1 file) KHÔNG được là boilerplate")
	}
	if d.IsBoilerplate("bao gia vat tu xi mang sat thep") {
		t.Errorf("đoạn riêng KHÔNG được là boilerplate")
	}
}
