package maplint

import (
	"math"
	"strconv"
	"strings"
)

type constKind int

const (
	constNum constKind = iota
	constStr
	constPath
	constNull
	constFile
	constList
	constRaw
)

// constant is a map variable value parsed the way maplint's dmm.py
// DMMParser.parse_constant does it. raw is the original DM source text.
type constant struct {
	kind constKind
	num  float64
	s    string // string/path/file content (quotes stripped)
	raw  string
}

func isTypepathText(s string) bool {
	// common.py REGEX_TYPEPATH-like: ^/[/\w]+$
	if len(s) < 2 || s[0] != '/' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c == '/' || c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			continue
		}
		return false
	}
	return true
}

// parseConstant mirrors dmm.py DMMParser.parse_constant. Lists collapse to
// ["NYI: list"] upstream; here they keep their raw text for equality (see
// constEqual) but stringify like upstream. Unknown shapes become constRaw
// rather than an error.
func parseConstant(text string) constant {
	// safe_float
	if !strings.ContainsAny(text, "xX") {
		if f, err := strconv.ParseFloat(text, 64); err == nil || isRangeErr(err) {
			return constant{kind: constNum, num: f, raw: text}
		}
	}
	switch {
	case isTypepathText(text):
		return constant{kind: constPath, s: text, raw: text}
	case len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"':
		return constant{kind: constStr, s: text[1 : len(text)-1], raw: text}
	case text == "null":
		return constant{kind: constNull, s: "null", raw: text}
	case len(text) >= 2 && text[0] == '\'' && text[len(text)-1] == '\'':
		return constant{kind: constFile, s: text[1 : len(text)-1], raw: text}
	case strings.HasPrefix(text, "list(") && strings.HasSuffix(text, ")"):
		return constant{kind: constList, raw: text}
	}
	return constant{kind: constRaw, s: text, raw: text}
}

func isRangeErr(err error) bool {
	ne, ok := err.(*strconv.NumError)
	return ok && ne.Err == strconv.ErrRange
}

// pyStr mirrors Python's str() of the parsed constant.
func (c constant) pyStr() string {
	switch c.kind {
	case constNum:
		return pyFloatStr(c.num)
	case constList:
		return "['NYI: list']"
	default:
		return c.s
	}
}

// pyFloatStr mirrors Python's repr(float).
func pyFloatStr(f float64) string {
	switch {
	case math.IsNaN(f):
		return "nan"
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	}
	abs := math.Abs(f)
	if abs == 0 || (abs >= 1e-4 && abs < 1e16) {
		s := strconv.FormatFloat(f, 'f', -1, 64)
		if !strings.Contains(s, ".") {
			s += ".0"
		}
		return s
	}
	return strconv.FormatFloat(f, 'e', -1, 64)
}

// constEqual mirrors Python equality of parsed constants. Lists compare by raw
// text instead of upstream's constant placeholder, which would equate any two
// lists.
func constEqual(a, b constant) bool {
	if a.kind != b.kind {
		return false
	}
	switch a.kind {
	case constNum:
		return a.num == b.num
	case constList:
		return a.raw == b.raw
	default:
		return a.s == b.s
	}
}
