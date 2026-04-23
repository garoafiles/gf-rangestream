package rangestream

import (
	"fmt"
	"net/http"
)

// MaxRangeBytes returns a Validator that rejects range requests whose
// first range spans more than n bytes. Requests without a Range header
// pass through (no range, nothing to cap). For multi-range requests,
// only the first range is checked — pair with RejectMultiRange() when
// you want a hard cap on total response bytes.
func MaxRangeBytes(n int64) Validator {
	return func(r *http.Request, rng ParsedRange) error {
		if rng.Raw == "" {
			return nil
		}
		length := rng.End - rng.Start + 1
		if length > n {
			return fmt.Errorf("rangestream: range of %d bytes exceeds cap of %d", length, n)
		}
		return nil
	}
}

// RejectMultiRange returns a Validator that rejects requests with more
// than one byte-range in the Range header. Useful for CDN-cache-friendly
// deployments where single-range responses simplify partial-content
// semantics.
func RejectMultiRange() Validator {
	return func(r *http.Request, rng ParsedRange) error {
		if rng.IsMulti {
			return fmt.Errorf("rangestream: multi-range requests are not allowed")
		}
		return nil
	}
}
