package rangestream

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- ServeRange -------------------------------------------------------------

func TestServeRangeNoRangeServesFullBody(t *testing.T) {
	body := []byte("hello, gf-rangestream")
	w, r := newServeReq(t, "", body)

	var served int64
	ServeRange(w, r, Content{
		DisplayName: "hello.txt",
		ModTime:     time.Unix(0, 0),
		Reader:      bytes.NewReader(body),
	}, Options{
		OnBytesServed: func(n int64) { served = n },
	})

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	if string(got) != string(body) {
		t.Errorf("body mismatch: got %q", got)
	}
	if served != int64(len(body)) {
		t.Errorf("OnBytesServed = %d, want %d", served, len(body))
	}
}

func TestServeRangeSingleRange(t *testing.T) {
	body := []byte("0123456789abcdef")
	w, r := newServeReq(t, "bytes=2-5", body)

	ServeRange(w, r, Content{
		DisplayName: "x.bin",
		ModTime:     time.Unix(0, 0),
		Reader:      bytes.NewReader(body),
	}, Options{})

	resp := w.Result()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	if string(got) != "2345" {
		t.Errorf("body = %q, want %q", got, "2345")
	}
}

func TestServeRangeSuffixRange(t *testing.T) {
	body := []byte("0123456789")
	w, r := newServeReq(t, "bytes=-3", body)

	ServeRange(w, r, Content{
		DisplayName: "x.bin",
		ModTime:     time.Unix(0, 0),
		Reader:      bytes.NewReader(body),
	}, Options{})

	resp := w.Result()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	if string(got) != "789" {
		t.Errorf("body = %q, want %q", got, "789")
	}
}

func TestServeRangeContentTypeFromOptions(t *testing.T) {
	body := []byte("hi")
	w, r := newServeReq(t, "", body)

	ServeRange(w, r, Content{Reader: bytes.NewReader(body)}, Options{
		ContentType: "application/x-made-up",
	})

	if got := w.Result().Header.Get("Content-Type"); got != "application/x-made-up" {
		t.Errorf("Content-Type = %q, want application/x-made-up", got)
	}
}

func TestServeRangeMaxRangeBytesExceeded(t *testing.T) {
	body := make([]byte, 1024)
	w, r := newServeReq(t, "bytes=0-511", body)

	ServeRange(w, r, Content{Reader: bytes.NewReader(body)}, Options{
		Validators: []Validator{MaxRangeBytes(100)},
	})

	if w.Result().StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Errorf("status = %d, want 416", w.Result().StatusCode)
	}
}

func TestServeRangeMaxRangeBytesWithinCap(t *testing.T) {
	body := make([]byte, 1024)
	w, r := newServeReq(t, "bytes=0-99", body)

	ServeRange(w, r, Content{
		DisplayName: "x.bin",
		ModTime:     time.Unix(0, 0),
		Reader:      bytes.NewReader(body),
	}, Options{
		Validators: []Validator{MaxRangeBytes(200)},
	})

	if w.Result().StatusCode != http.StatusPartialContent {
		t.Errorf("status = %d, want 206", w.Result().StatusCode)
	}
}

func TestServeRangeRejectMultiRange(t *testing.T) {
	body := []byte("0123456789abcdef")
	w, r := newServeReq(t, "bytes=0-3,8-11", body)

	ServeRange(w, r, Content{Reader: bytes.NewReader(body)}, Options{
		Validators: []Validator{RejectMultiRange()},
	})

	if w.Result().StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Errorf("status = %d, want 416", w.Result().StatusCode)
	}
}

func TestServeRangeStatusForOverride(t *testing.T) {
	body := make([]byte, 100)
	w, r := newServeReq(t, "bytes=0-99", body)

	ServeRange(w, r, Content{Reader: bytes.NewReader(body)}, Options{
		Validators: []Validator{MaxRangeBytes(10)},
		StatusFor:  func(err error) int { return http.StatusRequestEntityTooLarge },
	})

	if w.Result().StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", w.Result().StatusCode)
	}
}

func TestServeRangeBadRangeHeader(t *testing.T) {
	body := make([]byte, 100)
	w, r := newServeReq(t, "bytes=abc-def", body)

	ServeRange(w, r, Content{Reader: bytes.NewReader(body)}, Options{
		Validators: []Validator{RejectMultiRange()},
	})

	if w.Result().StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Errorf("status = %d, want 416", w.Result().StatusCode)
	}
}

func TestServeRangeCustomServeFunc(t *testing.T) {
	body := []byte("x")
	w, r := newServeReq(t, "", body)

	called := false
	ServeRange(w, r, Content{Reader: bytes.NewReader(body)}, Options{
		Serve: func(w http.ResponseWriter, r *http.Request, c Content) (int64, error) {
			called = true
			w.WriteHeader(http.StatusTeapot)
			return 0, nil
		},
	})

	if !called {
		t.Error("custom ServeFunc was not called")
	}
	if w.Result().StatusCode != http.StatusTeapot {
		t.Errorf("status = %d, want 418", w.Result().StatusCode)
	}
}

// --- ParseRange -------------------------------------------------------------

func TestParseRange(t *testing.T) {
	size := int64(1000)
	cases := []struct {
		header  string
		start   int64
		end     int64
		isMulti bool
		wantErr bool
	}{
		{header: "bytes=0-99", start: 0, end: 99},
		{header: "bytes=0-0", start: 0, end: 0},
		{header: "bytes=100-", start: 100, end: 999},
		{header: "bytes=-50", start: 950, end: 999},
		{header: "bytes=0-99,200-299", start: 0, end: 99, isMulti: true},
		{header: "bytes=0-10000", start: 0, end: 999}, // end clamp
		{header: "bytes=-5000", start: 0, end: 999},   // suffix clamp
		{header: "", wantErr: true},
		{header: "bytes=", wantErr: true},
		{header: "bytes=abc-def", wantErr: true},
		{header: "items=0-10", wantErr: true},
		{header: "bytes=1000-1050", wantErr: true}, // start past end
		{header: "bytes=5-2", wantErr: true},       // end before start
	}
	for _, tc := range cases {
		got, err := ParseRange(tc.header, size)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseRange(%q) = %+v, want error", tc.header, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseRange(%q) unexpected error: %v", tc.header, err)
			continue
		}
		if got.Start != tc.start || got.End != tc.end || got.IsMulti != tc.isMulti {
			t.Errorf("ParseRange(%q) = %+v, want {Start:%d End:%d IsMulti:%v}",
				tc.header, got, tc.start, tc.end, tc.isMulti)
		}
		if got.Raw != tc.header {
			t.Errorf("ParseRange(%q) Raw = %q", tc.header, got.Raw)
		}
	}
}

// --- Validators (direct) ----------------------------------------------------

func TestMaxRangeBytesDirect(t *testing.T) {
	v := MaxRangeBytes(100)
	if err := v(nil, ParsedRange{Start: 0, End: 49, Raw: "bytes=0-49"}); err != nil {
		t.Errorf("range within cap rejected: %v", err)
	}
	if err := v(nil, ParsedRange{Start: 0, End: 200, Raw: "bytes=0-200"}); err == nil {
		t.Error("range above cap not rejected")
	}
	if err := v(nil, ParsedRange{}); err != nil {
		t.Errorf("no-range request rejected: %v", err)
	}
}

func TestRejectMultiRangeDirect(t *testing.T) {
	v := RejectMultiRange()
	if err := v(nil, ParsedRange{IsMulti: true, Raw: "bytes=0-9,20-29"}); err == nil {
		t.Error("multi-range not rejected")
	}
	if err := v(nil, ParsedRange{Raw: "bytes=0-9"}); err != nil {
		t.Errorf("single-range rejected: %v", err)
	}
}

// --- helpers ----------------------------------------------------------------

func newServeReq(t *testing.T, rangeHeader string, body []byte) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/", strings.NewReader(""))
	if rangeHeader != "" {
		r.Header.Set("Range", rangeHeader)
	}
	_ = body // keep signature symmetric for future payload-echo tests
	return httptest.NewRecorder(), r
}

// Example (compile-check documentation).
func ExampleServeRange() {
	handler := func(w http.ResponseWriter, r *http.Request) {
		body := bytes.NewReader([]byte("hello"))
		ServeRange(w, r, Content{
			DisplayName: "hello.txt",
			ModTime:     time.Now(),
			Reader:      body,
		}, Options{
			Validators:    []Validator{MaxRangeBytes(1 << 20), RejectMultiRange()},
			OnBytesServed: func(n int64) { fmt.Println(n) },
		})
	}
	_ = handler
}
