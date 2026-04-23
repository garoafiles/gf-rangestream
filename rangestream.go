package rangestream

import (
	"io"
	"net/http"
	"time"
)

// Content is the minimum surface a caller must provide. An io.ReadSeeker
// with a known ModTime is enough to drive HTTP range semantics via
// http.ServeContent. DisplayName is used for content-type sniffing when
// Options.ContentType is empty and the caller hasn't pre-set Content-Type
// on w; callers that pre-set Content-Type may leave DisplayName empty.
type Content struct {
	DisplayName string
	ModTime     time.Time
	Reader      io.ReadSeeker
}

// Validator runs after the Range header (if any) has been parsed, before
// any bytes go out. Return a non-nil error to abort the request; the error
// is mapped to an HTTP status via Options.StatusFor (default 416 Range Not
// Satisfiable). A validator may inspect the request freely — the intended
// use is for policy checks that depend on parsed range (e.g. "reject ranges
// larger than 64 MiB").
//
// If the request has no Range header, rng is the zero value and rng.Raw is
// "". Validators that care about range presence should check rng.Raw != "".
type Validator func(r *http.Request, rng ParsedRange) error

// ParsedRange is a minimal snapshot of the request's Range header. For a
// single-range request, Start and End are inclusive byte offsets. For
// multi-range, IsMulti is true and Start/End refer to the first range;
// callers that need full multi-range visibility can inspect Raw.
type ParsedRange struct {
	Start   int64
	End     int64
	IsMulti bool
	Raw     string // the raw "bytes=..." header value; empty if no Range header
}

// ServeFunc is the signature of a low-level serve backend. The default
// implementation wraps http.ServeContent; alternatives include sendfile(2)
// adapters, io_uring backends, or writers that emit X-Accel-Redirect for
// nginx-fronted deployments. A ServeFunc is responsible for writing the
// status line, headers, and body in accordance with the request's range
// (which it may re-parse from r.Header if needed — Serve is not given the
// parsed range directly, to keep the signature stable).
type ServeFunc func(w http.ResponseWriter, r *http.Request, c Content) (bytesWritten int64, err error)

// Options configures a ServeRange call. A zero Options behaves like calling
// http.ServeContent directly.
type Options struct {
	// ContentType, if non-empty, is set on the response before any bytes
	// ship. When empty, http.ServeContent's name-based sniff (or any
	// Content-Type the caller already pre-set on w.Header()) applies.
	ContentType string

	// Validators run in order, first-non-nil-error-wins. Each is called
	// with the parsed range; returning nil allows the request through.
	// When no Range header is present, validators are still called with
	// a zero-value ParsedRange so callers can implement "require range"
	// policy if they want.
	Validators []Validator

	// Serve is the low-level handoff. Nil means DefaultServe, which wraps
	// http.ServeContent.
	Serve ServeFunc

	// OnBytesServed, if set, is called after the response completes with
	// the number of bytes actually written to w's body. Useful for
	// accounting / fair-use metering. Best-effort; called with 0 on
	// validator-rejection paths.
	OnBytesServed func(n int64)

	// StatusFor maps a validator error to an HTTP status. Default is 416
	// Range Not Satisfiable. Return 0 to let the default apply.
	StatusFor func(err error) int
}

// ServeRange streams c over HTTP, honoring Range requests. Behavior:
//
//  1. If opts.ContentType is non-empty, set Content-Type.
//  2. If there are validators and a Range header, parse it. Parse failure
//     responds 416 and returns.
//  3. Run each opts.Validators in order. First non-nil error aborts with
//     the mapped status and does NOT call Serve.
//  4. Invoke opts.Serve (or DefaultServe) to write the response.
//  5. Fire opts.OnBytesServed with the byte count.
//
// Callers must have already authenticated / authorized the request and
// set any Content-Disposition or caching headers they want. ServeRange
// sets Content-Type (from opts.ContentType) and writes Content-Length /
// Content-Range as part of the serve step.
func ServeRange(w http.ResponseWriter, r *http.Request, c Content, opts Options) {
	if opts.ContentType != "" {
		w.Header().Set("Content-Type", opts.ContentType)
	}

	if len(opts.Validators) > 0 {
		rng, ok := parseForValidators(w, r, c.Reader)
		if !ok {
			return
		}
		for _, v := range opts.Validators {
			if err := v(r, rng); err != nil {
				status := http.StatusRequestedRangeNotSatisfiable
				if opts.StatusFor != nil {
					if s := opts.StatusFor(err); s != 0 {
						status = s
					}
				}
				http.Error(w, err.Error(), status)
				return
			}
		}
	}

	serve := opts.Serve
	if serve == nil {
		serve = DefaultServe
	}
	n, _ := serve(w, r, c)
	if opts.OnBytesServed != nil {
		opts.OnBytesServed(n)
	}
}

// parseForValidators returns a ParsedRange for the request. If the request
// has no Range header, it returns a zero ParsedRange and ok=true. If the
// header is present but malformed, it writes 416 to w and returns ok=false.
func parseForValidators(w http.ResponseWriter, r *http.Request, rs io.ReadSeeker) (ParsedRange, bool) {
	raw := r.Header.Get("Range")
	if raw == "" {
		return ParsedRange{}, true
	}
	size, err := rs.Seek(0, io.SeekEnd)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return ParsedRange{}, false
	}
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return ParsedRange{}, false
	}
	rng, perr := ParseRange(raw, size)
	if perr != nil {
		http.Error(w, perr.Error(), http.StatusRequestedRangeNotSatisfiable)
		return ParsedRange{}, false
	}
	return rng, true
}

// DefaultServe wraps http.ServeContent and counts bytes written to the
// response body. Exposed so callers who want to compose validators with
// the stdlib behavior (the common case) can do so without re-implementing
// the counter.
func DefaultServe(w http.ResponseWriter, r *http.Request, c Content) (int64, error) {
	cw := &countingResponseWriter{ResponseWriter: w}
	http.ServeContent(cw, r, c.DisplayName, c.ModTime, c.Reader)
	return cw.n, nil
}

// countingResponseWriter tracks total bytes written to the response body.
// It does not implement http.Flusher/http.Hijacker intentionally:
// http.ServeContent does not need them, and preserving them would require
// runtime type-assertion gymnastics that add surface without benefit for
// this use case.
type countingResponseWriter struct {
	http.ResponseWriter
	n int64
}

func (c *countingResponseWriter) Write(b []byte) (int, error) {
	n, err := c.ResponseWriter.Write(b)
	c.n += int64(n)
	return n, err
}
