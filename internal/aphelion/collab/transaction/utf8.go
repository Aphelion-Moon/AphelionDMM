package transaction

import (
	"fmt"
	"io"
	"unicode/utf8"
)

// JSON replaces malformed UTF-8 silently. Transactions must reject it instead
// of changing raw map values. Only an incomplete rune crosses read boundaries.
type utf8Reader struct {
	source  io.Reader
	tail    [utf8.UTFMax]byte
	pending int
}

func (r *utf8Reader) Read(p []byte) (int, error) {
	n, err := r.source.Read(p)
	data := p[:n]
	if r.pending > 0 {
		for len(data) > 0 && !utf8.FullRune(r.tail[:r.pending]) {
			r.tail[r.pending] = data[0]
			r.pending++
			data = data[1:]
		}
		if utf8.FullRune(r.tail[:r.pending]) {
			if !utf8.Valid(r.tail[:r.pending]) {
				return 0, fmt.Errorf("transaction contains invalid UTF-8")
			}
			r.pending = 0
		}
	}
	if len(data) > 0 && !utf8.Valid(data) {
		start := len(data) - 1
		for start > 0 && !utf8.RuneStart(data[start]) && len(data)-start < utf8.UTFMax {
			start--
		}
		if !utf8.Valid(data[:start]) || utf8.FullRune(data[start:]) {
			return 0, fmt.Errorf("transaction contains invalid UTF-8")
		}
		r.pending = copy(r.tail[:], data[start:])
	}
	if err == io.EOF && r.pending != 0 {
		return 0, fmt.Errorf("transaction ends in incomplete UTF-8")
	}
	return n, err
}
