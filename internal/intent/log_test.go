package intent

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAppendAndLoad(t *testing.T) {
	dir := t.TempDir()
	l := NewLog(dir)

	e1 := Event{Time: time.Now(), Type: EventAppOpen}
	e2 := Event{Time: time.Now(), Type: EventSearch, Query: "vinamilk"}
	if err := l.Append(e1); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(e2); err != nil {
		t.Fatal(err)
	}

	got, err := l.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("muốn 2 event, được %d", len(got))
	}
	if got[1].Type != EventSearch || got[1].Query != "vinamilk" {
		t.Fatalf("event 2 sai: %+v", got[1])
	}
	// đúng file path
	if _, err := filepath.Abs(l.path); err != nil {
		t.Fatal(err)
	}
}

func TestLoadEmptyReturnsNil(t *testing.T) {
	l := NewLog(t.TempDir())
	got, err := l.Load()
	if err != nil {
		t.Fatalf("load file chưa tồn tại phải không lỗi, được: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("muốn rỗng, được %d", len(got))
	}
}

func TestRotateKeepsRecent(t *testing.T) {
	dir := t.TempDir()
	l := NewLog(dir)
	l.maxEvents = 10 // ngưỡng nhỏ để test

	for i := 0; i < 25; i++ {
		if err := l.Append(Event{Time: time.Now(), Type: EventSearch, Query: "q"}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := l.Load()
	if err != nil {
		t.Fatal(err)
	}
	// sau rotate phải còn <= maxEvents (giữ gần đây)
	if len(got) > 10 {
		t.Fatalf("rotate hỏng: còn %d event, phải <= 10", len(got))
	}
	if len(got) == 0 {
		t.Fatal("rotate xoá sạch — phải giữ event gần đây")
	}
}
