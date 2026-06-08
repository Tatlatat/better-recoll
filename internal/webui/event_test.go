package webui

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleEventAccepts(t *testing.T) {
	body := []byte(`{"type":"app_open"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/event", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handleEvent(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("muốn 200, được %d (%s)", w.Code, w.Body.String())
	}
}

func TestHandleEventRejectsGet(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/event", nil)
	w := httptest.NewRecorder()
	handleEvent(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET phải 405, được %d", w.Code)
	}
}
