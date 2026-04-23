# gf-rangestream

A thin HTTP Range streaming helper for Go. Wraps `http.ServeContent` with a
small hook surface: request validators, a pluggable serve backend, and
byte-count observability.

The library does **not** authenticate, authorize, or sign URLs — those are
the caller's responsibility. Use it when you want range-correct streaming
today and room to swap the serve backend or add per-request policy tomorrow
without rewriting the handler.

## Install

```sh
go get github.com/garoafiles/gf-rangestream
```

Requires Go 1.25 or later.

## Usage

Minimal (identical behavior to a direct `http.ServeContent` call):

```go
import "github.com/garoafiles/gf-rangestream"

func serve(w http.ResponseWriter, r *http.Request, f *os.File) {
    info, _ := f.Stat()
    rangestream.ServeRange(w, r, rangestream.Content{
        DisplayName: "report.pdf",
        ModTime:     info.ModTime(),
        Reader:      f,
    }, rangestream.Options{})
}
```

With validators + byte-count observability:

```go
rangestream.ServeRange(w, r, content, rangestream.Options{
    ContentType: "application/pdf",
    Validators: []rangestream.Validator{
        rangestream.MaxRangeBytes(64 << 20), // reject ranges > 64 MiB
        rangestream.RejectMultiRange(),
    },
    OnBytesServed: func(n int64) {
        metrics.RecordEgress(n)
    },
})
```

Callers that want a custom serve backend (e.g. `sendfile(2)`,
`X-Accel-Redirect`, or an `io_uring` adapter) supply a `ServeFunc`:

```go
opts := rangestream.Options{
    Serve: func(w http.ResponseWriter, r *http.Request, c rangestream.Content) (int64, error) {
        // write status, headers, body for the (pre-validated) request.
        return 0, nil
    },
}
rangestream.ServeRange(w, r, content, opts)
```

## Scope

**In.** `ServeRange`, `Options`, `Content`, `Validator`, `ServeFunc`,
`ParsedRange`. Built-in validators `MaxRangeBytes(n)` and
`RejectMultiRange()`. Exposed `ParseRange` for escape hatches.

**Out.** URL signing / verification (caller concern). Content-Disposition
crafting (response-shaping, easy to copy-paste). MIME sniffing (callers
pre-set `Content-Type` or rely on stdlib sniffing). Metering (use
`OnBytesServed` to build your own). Caching headers beyond what
`http.ServeContent` already handles via `ModTime`.

## Versioning

`v0.x.y` — API may shift. `v1.0.0` freezes `ServeRange`, `Options`,
`Content`, `Validator`, and the built-in validators.

## License

Apache-2.0. See [LICENSE](LICENSE).
