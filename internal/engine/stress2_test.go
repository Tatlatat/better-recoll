package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Stress test nâng cao — Claude. Các ca khắc nghiệt hơn để chắc KHÔNG còn bug.

// Tên file + đường dẫn tiếng Việt có dấu, khoảng trắng, ký tự lạ.
func TestStressUnicodePaths(t *testing.T) {
	eng, dir := newTestEngine(t)
	names := []string{
		"Biên Bản Nghiệm Thu.txt",
		"báo giá (bản sao).txt",
		"hợp đồng - 2024.txt",
		"tài liệu  nhiều   khoảng trắng.txt",
	}
	for i, n := range names {
		os.WriteFile(filepath.Join(dir, n),
			[]byte(fmt.Sprintf("nội dung công trình số %d nghiệm thu vật tư", i)), 0644)
	}
	if err := eng.Index(dir); err != nil {
		t.Fatalf("Index tên file unicode crash: %v", err)
	}
	res, err := eng.Search("nghiệm thu", 10)
	if err != nil {
		t.Fatalf("Search lỗi: %v", err)
	}
	if len(res) == 0 {
		t.Error("không tìm được file có tên unicode")
	}
}

// File rất lớn (text nhiều MB) — không OOM/treo.
func TestStressLargeFile(t *testing.T) {
	eng, dir := newTestEngine(t)
	big := strings.Repeat("Đây là một đoạn văn bản tiếng Việt rất dài về công trình xây dựng và nghiệm thu vật tư. ", 5000) // ~450KB
	os.WriteFile(filepath.Join(dir, "big.txt"), []byte(big), 0644)
	os.WriteFile(filepath.Join(dir, "small.txt"), []byte("báo giá vật tư"), 0644)
	if err := eng.Index(dir); err != nil {
		t.Fatalf("Index file lớn crash: %v", err)
	}
	if _, err := eng.Search("vật tư", 5); err != nil {
		t.Fatalf("Search sau file lớn lỗi: %v", err)
	}
}

// Search ĐỒNG THỜI nhiều goroutine — không data race / panic.
func TestStressConcurrentSearch(t *testing.T) {
	eng, dir := newTestEngine(t)
	for i := 0; i < 10; i++ {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d.txt", i)),
			[]byte(fmt.Sprintf("hợp đồng thi công số %d xây dựng nhà xưởng", i)), 0644)
	}
	if err := eng.Index(dir); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			q := []string{"hợp đồng", "thi công", "xây dựng", "nhà xưởng"}[n%4]
			if _, err := eng.SearchRanked(q, 5); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent search lỗi: %v", err)
	}
}

// Nhiều file trùng nội dung hệt nhau (template) — dedupe không crash.
func TestStressIdenticalFiles(t *testing.T) {
	eng, dir := newTestEngine(t)
	same := "CÔNG TY XÂY DỰNG ABC\nBIÊN BẢN NGHIỆM THU\nHạng mục: móng"
	for i := 0; i < 15; i++ {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("doc%d.txt", i)), []byte(same), 0644)
	}
	if err := eng.Index(dir); err != nil {
		t.Fatalf("Index file trùng crash: %v", err)
	}
	if _, err := eng.Search("nghiệm thu", 10); err != nil {
		t.Fatalf("Search lỗi: %v", err)
	}
}
