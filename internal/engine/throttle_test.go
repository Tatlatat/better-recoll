package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Oracle Claude. IndexThrottled chạy index với cấu hình throttle:
// - Workers: số luồng embed (onboarding=nhiều, nền=1)
// - BatchSize: số file mỗi batch
// - PauseBetweenBatches: nghỉ giữa batch (nền nghỉ nhiều = mát)
// Test: throttle BẬT (pause lớn) phải CHẬM hơn rõ rệt so với không pause —
// chứng minh cơ chế nghỉ thật sự hoạt động (duty cycle).
func TestIndexThrottleSlowsDown(t *testing.T) {
	root, _ := filepath.Abs("../..")
	if _, err := os.Stat(filepath.Join(root, "models/onnx/bge-m3/model.onnx")); err != nil {
		t.Skip("no model")
	}

	// 6 file nhỏ
	mk := func() string {
		d := t.TempDir()
		for i := 0; i < 6; i++ {
			os.WriteFile(filepath.Join(d, "f"+string(rune('a'+i))+".txt"),
				[]byte("tài liệu mẫu công trình xây dựng nghiệm thu vật tư số "+string(rune('0'+i))), 0644)
		}
		return d
	}

	// Pha onboarding: không nghỉ
	d1 := mk()
	eng1, err := New(DefaultConfig(root, filepath.Join(d1, ".idx")))
	if err != nil {
		t.Fatal(err)
	}
	defer eng1.Close()
	fast := IndexOptions{Workers: 4, BatchSize: 2, PauseBetweenBatches: 0}
	s := time.Now()
	if err := eng1.IndexThrottled(d1, fast); err != nil {
		t.Fatalf("fast index: %v", err)
	}
	fastDur := time.Since(s)

	// Pha nền: nghỉ 300ms/batch
	d2 := mk()
	eng2, err := New(DefaultConfig(root, filepath.Join(d2, ".idx")))
	if err != nil {
		t.Fatal(err)
	}
	defer eng2.Close()
	slow := IndexOptions{Workers: 1, BatchSize: 2, PauseBetweenBatches: 300 * time.Millisecond}
	s = time.Now()
	if err := eng2.IndexThrottled(d2, slow); err != nil {
		t.Fatalf("slow index: %v", err)
	}
	slowDur := time.Since(s)

	t.Logf("fast=%v slow=%v", fastDur, slowDur)
	// 3 batch × 300ms nghỉ = +900ms ít nhất → slow phải chậm hơn fast rõ rệt
	if slowDur <= fastDur+500*time.Millisecond {
		t.Errorf("throttle không hiệu quả: slow=%v phải chậm hơn fast=%v ít nhất 500ms", slowDur, fastDur)
	}

	// Cả hai vẫn index đúng (search ra kết quả)
	res, _ := eng2.Search("nghiem thu", 5)
	if len(res) == 0 {
		t.Error("throttled index không tạo được kết quả search")
	}
}
