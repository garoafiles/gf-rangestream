// Package rangestream is a minimal HTTP Range streaming helper. ServeRange
// wraps http.ServeContent with a small Options hook surface: request
// validators that run after the Range header is parsed, a pluggable
// ServeFunc backend (default is stdlib), and byte-count observability.
//
// The library does not authenticate, authorize, or sign URLs — that is the
// caller's responsibility. By the time ServeRange is called, the caller
// must already have validated the request.
package rangestream
