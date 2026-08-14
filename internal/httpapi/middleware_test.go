package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWithRequestID_GeneratesID(t *testing.T) {
	s := &Server{}
	var capturedID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = GetRequestID(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := s.withRequestID(inner)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedID == "" {
		t.Error("request_id 不应为空")
	}
	if !strings.HasPrefix(capturedID, "req_") {
		t.Errorf("request_id 应以 req_ 开头，实际: %s", capturedID)
	}
	respID := rec.Header().Get("X-Request-ID")
	if respID != capturedID {
		t.Errorf("响应头 X-Request-ID (%s) 应与 context 中一致 (%s)", respID, capturedID)
	}
}

func TestWithRequestID_UsesExisting(t *testing.T) {
	s := &Server{}
	var capturedID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = GetRequestID(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := s.withRequestID(inner)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", "custom-123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedID != "custom-123" {
		t.Errorf("应使用请求头中的 request_id，实际: %s", capturedID)
	}
	if rec.Header().Get("X-Request-ID") != "custom-123" {
		t.Error("响应头应回传请求头中的 request_id")
	}
}

func TestWithAccessLog_CapturesStatus(t *testing.T) {
	s := &Server{}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	handler := s.withAccessLog(inner)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("期望 status 201，实际 %d", rec.Code)
	}
}

func TestMiddlewareChain_Integration(t *testing.T) {
	s := &Server{}
	var gotID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = GetRequestID(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := s.withRequestID(s.withAccessLog(inner))
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if gotID == "" {
		t.Error("中间件链中 request_id 丢失")
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("中间件链中 X-Request-ID 响应头丢失")
	}
}
