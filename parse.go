package rangestream

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseRange parses an HTTP Range header value per RFC 7233 §2.1 for the
// "bytes=" unit. size is the total size of the underlying content.
//
// The returned ParsedRange exposes the first range's Start/End (inclusive
// byte offsets), whether there are multiple ranges (IsMulti), and the
// original header value (Raw). Parsing fails and returns an error when:
//
//   - header is empty or missing the "bytes=" prefix,
//   - any range segment is malformed,
//   - a start offset is beyond the end of the content.
//
// Parse is tolerant about over-large end offsets: an explicit end greater
// than the content size is clamped to size-1 (matching RFC 7233 §4.2's
// "satisfiable" rule).
func ParseRange(header string, size int64) (ParsedRange, error) {
	const prefix = "bytes="
	if !strings.HasPrefix(header, prefix) {
		return ParsedRange{}, fmt.Errorf("rangestream: missing %q prefix in %q", prefix, header)
	}
	spec := strings.TrimSpace(header[len(prefix):])
	if spec == "" {
		return ParsedRange{}, fmt.Errorf("rangestream: empty range spec")
	}
	parts := strings.Split(spec, ",")
	ranges := make([][2]int64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		r, err := parseOne(p, size)
		if err != nil {
			return ParsedRange{}, err
		}
		ranges = append(ranges, r)
	}
	if len(ranges) == 0 {
		return ParsedRange{}, fmt.Errorf("rangestream: no ranges parsed in %q", header)
	}
	return ParsedRange{
		Start:   ranges[0][0],
		End:     ranges[0][1],
		IsMulti: len(ranges) > 1,
		Raw:     header,
	}, nil
}

// parseOne parses a single byte-range-spec of the form "start-end",
// "start-", or "-last". Returns {start, end} inclusive.
func parseOne(spec string, size int64) ([2]int64, error) {
	dash := strings.IndexByte(spec, '-')
	if dash < 0 {
		return [2]int64{}, fmt.Errorf("rangestream: missing '-' in range spec %q", spec)
	}
	startStr := strings.TrimSpace(spec[:dash])
	endStr := strings.TrimSpace(spec[dash+1:])

	switch {
	case startStr == "" && endStr == "":
		return [2]int64{}, fmt.Errorf("rangestream: empty range spec %q", spec)

	case startStr == "":
		// Suffix form: "-N" means last N bytes.
		n, err := strconv.ParseInt(endStr, 10, 64)
		if err != nil || n <= 0 {
			return [2]int64{}, fmt.Errorf("rangestream: bad suffix length in %q", spec)
		}
		if n > size {
			n = size
		}
		return [2]int64{size - n, size - 1}, nil

	case endStr == "":
		s, err := strconv.ParseInt(startStr, 10, 64)
		if err != nil || s < 0 {
			return [2]int64{}, fmt.Errorf("rangestream: bad start in %q", spec)
		}
		if s >= size {
			return [2]int64{}, fmt.Errorf("rangestream: start %d past size %d", s, size)
		}
		return [2]int64{s, size - 1}, nil

	default:
		s, errS := strconv.ParseInt(startStr, 10, 64)
		e, errE := strconv.ParseInt(endStr, 10, 64)
		if errS != nil || errE != nil || s < 0 || e < s {
			return [2]int64{}, fmt.Errorf("rangestream: bad range pair %q", spec)
		}
		if s >= size {
			return [2]int64{}, fmt.Errorf("rangestream: start %d past size %d", s, size)
		}
		if e >= size {
			e = size - 1
		}
		return [2]int64{s, e}, nil
	}
}
