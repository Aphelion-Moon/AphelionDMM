package updatepaths

import (
	"errors"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	varName  = regexp.MustCompile(`^[A-Za-z0-9_\-$]+$`)
	typePath = regexp.MustCompile(`^(/[A-Za-z0-9_]+)+$`)
)

const subtypesSuffix = "/@SUBTYPES"

// Parse reads one script. Lines starting with '#' (the reference tool's comment
// form) or '//' are ignored. Every malformed line is reported; nothing is
// guessed, and malformed lines produce no rule.
func Parse(name string, src []byte) (Script, []ParseError) {
	script := Script{Name: name, Order: scriptOrder(name)}
	var errs []ParseError
	for n, line := range strings.Split(string(src), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		pos := Pos{File: name, Line: n + 1}
		rule, err := parseRule(line)
		if err != nil {
			errs = append(errs, ParseError{Pos: pos, Msg: err.Error()})
			continue
		}
		rule.Pos = pos
		script.Rules = append(script.Rules, rule)
	}
	return script, errs
}

// ParseRule reads a single rule line, as stored in remembered decisions.
func ParseRule(line string) (Rule, error) {
	return parseRule(strings.TrimSpace(line))
}

func scriptOrder(name string) int64 {
	base := filepath.Base(name)
	end := 0
	for end < len(base) && base[end] >= '0' && base[end] <= '9' {
		end++
	}
	if end == 0 {
		return -1
	}
	order, err := strconv.ParseInt(base[:end], 10, 64)
	if err != nil {
		return -1
	}
	return order
}

func parseRule(line string) (Rule, error) {
	parts, err := splitTop(line, ':', 2)
	if err != nil {
		return Rule{}, err
	}
	if len(parts) != 2 {
		return Rule{}, errors.New("missing ':' between the old and new path")
	}
	var rule Rule
	path, filters, err := parseSpec(parts[0])
	if err != nil {
		return Rule{}, err
	}
	if rule.Match, err = parseMatch(path, filters); err != nil {
		return Rule{}, err
	}
	if strings.TrimSpace(parts[1]) == "" {
		return Rule{}, errors.New("missing replacement after ':'")
	}
	outputs, err := splitTop(parts[1], ',', -1)
	if err != nil {
		return Rule{}, err
	}
	for _, text := range outputs {
		path, props, err := parseSpec(text)
		if err != nil {
			return Rule{}, err
		}
		if path == "@DELETE" {
			if props != nil {
				return Rule{}, errors.New("@DELETE takes no variables")
			}
			rule.Delete = true
			continue
		}
		output, err := parseOutput(path, props)
		if err != nil {
			return Rule{}, err
		}
		rule.Outputs = append(rule.Outputs, output)
	}
	if rule.Delete && len(rule.Outputs) != 0 {
		return Rule{}, errors.New("@DELETE cannot be combined with other outputs")
	}
	return rule, nil
}

// parseSpec splits "path{body}" and returns the body's top-level entries; a
// nil slice means no braces were present.
func parseSpec(text string) (string, []string, error) {
	text = strings.TrimSpace(text)
	open := indexTop(text, '{')
	if open < 0 {
		return text, nil, nil
	}
	close, err := matchingBrace(text, open)
	if err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(text[close+1:]) != "" {
		return "", nil, errors.New("unexpected text after '}'")
	}
	entries, err := splitTop(text[open+1:close], ';', -1)
	if err != nil {
		return "", nil, err
	}
	props := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry = strings.TrimSpace(entry); entry != "" {
			props = append(props, entry)
		}
	}
	return strings.TrimSpace(text[:open]), props, nil
}

func checkPath(path string) error {
	switch {
	case path == "":
		return errors.New("empty path")
	case strings.HasPrefix(path, "@"):
		return errors.New("unknown directive " + path)
	case !strings.HasPrefix(path, "/"):
		return errors.New("path " + strconv.Quote(path) + " must start with '/'")
	case !typePath.MatchString(path):
		return errors.New("invalid path " + strconv.Quote(path))
	}
	return nil
}

func parseMatch(path string, entries []string) (Pattern, error) {
	var pattern Pattern
	if strings.HasSuffix(path, subtypesSuffix) {
		path, pattern.Subtypes = strings.TrimSuffix(path, subtypesSuffix), true
	}
	if err := checkPath(path); err != nil {
		return Pattern{}, err
	}
	pattern.Path = path
	for _, entry := range entries {
		switch entry {
		case "@UNSET":
			pattern.Filters = append(pattern.Filters, Filter{Kind: FilterNoEdits})
			continue
		case "@OLD":
			return Pattern{}, errors.New("@OLD is not a filter")
		}
		name, value, err := splitAssignment(entry)
		if err != nil {
			return Pattern{}, err
		}
		filter := Filter{Name: name}
		switch {
		case value == "@ANY":
			filter.Kind = FilterAny
		case value == "@UNSET":
			filter.Kind = FilterUnset
		case value == "@SKIP" || strings.HasPrefix(value, "@OLD"):
			return Pattern{}, errors.New(strings.SplitN(value, ":", 2)[0] + " is not a filter value")
		case strings.HasPrefix(value, "@"):
			return Pattern{}, errors.New("unknown directive " + value)
		default:
			filter.Value = value
		}
		pattern.Filters = append(pattern.Filters, filter)
	}
	return pattern, nil
}

func parseOutput(path string, entries []string) (Output, error) {
	var output Output
	switch {
	case path == "@OLD":
		output.Kind = OutputKeepPath
	case strings.HasSuffix(path, subtypesSuffix):
		// Without an @SUBTYPES match the suffix is empty, as in the reference
		// tool: the output is the base path.
		output.Kind, output.Path = OutputSubtypes, strings.TrimSuffix(path, subtypesSuffix)
	default:
		output.Path = path
	}
	if output.Kind != OutputKeepPath {
		if err := checkPath(output.Path); err != nil {
			return Output{}, err
		}
	}
	for _, entry := range entries {
		if entry == "@OLD" {
			output.Props = append(output.Props, Prop{Kind: PropAllOld})
			continue
		}
		name, value, err := splitAssignment(entry)
		if err != nil {
			return Output{}, err
		}
		prop := Prop{Name: name}
		switch {
		case value == "@SKIP":
			prop.Kind = PropSkip
		case value == "@OLD":
			prop.Kind = PropOld
		case strings.HasPrefix(value, "@OLD:"):
			prop.Kind, prop.From = PropOldOf, strings.TrimSpace(strings.TrimPrefix(value, "@OLD:"))
			if !varName.MatchString(prop.From) {
				return Output{}, errors.New("@OLD: requires a variable name")
			}
		case value == "@ANY" || value == "@UNSET":
			return Output{}, errors.New(value + " is only valid in a filter")
		case strings.HasPrefix(value, "@"):
			return Output{}, errors.New("unknown directive " + value)
		default:
			prop.Value = value
		}
		output.Props = append(output.Props, prop)
	}
	return output, nil
}

func splitAssignment(entry string) (string, string, error) {
	parts, err := splitTop(entry, '=', 2)
	if err != nil {
		return "", "", err
	}
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		return "", "", errors.New("expected name = value, got " + strconv.Quote(entry))
	}
	name := strings.TrimSpace(parts[0])
	if !varName.MatchString(name) {
		return "", "", errors.New("invalid variable name " + strconv.Quote(name))
	}
	return name, strings.TrimSpace(parts[1]), nil
}

// walkTop visits every byte outside string literals with the bracket depth
// before that byte. Returning false stops the walk.
func walkTop(s string, visit func(i int, depth int) bool) error {
	var stack []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"', '\'':
			end := i + 1
			for end < len(s) && s[end] != c {
				if s[end] == '\\' {
					end++
				}
				end++
			}
			if end >= len(s) {
				return errors.New("unterminated string")
			}
			i = end
			continue
		}
		if !visit(i, len(stack)) {
			return nil
		}
		switch c {
		case '(', '[', '{':
			stack = append(stack, c)
		case ')', ']', '}':
			want := map[byte]byte{')': '(', ']': '[', '}': '{'}[c]
			if len(stack) == 0 || stack[len(stack)-1] != want {
				return errors.New("unbalanced brackets")
			}
			stack = stack[:len(stack)-1]
		}
	}
	if len(stack) != 0 {
		return errors.New("unbalanced brackets")
	}
	return nil
}

func splitTop(s string, sep byte, limit int) ([]string, error) {
	var parts []string
	start := 0
	err := walkTop(s, func(i, depth int) bool {
		if depth == 0 && s[i] == sep && (limit < 0 || len(parts) < limit-1) {
			parts = append(parts, s[start:i])
			start = i + 1
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return append(parts, s[start:]), nil
}

func indexTop(s string, c byte) int {
	found := -1
	_ = walkTop(s, func(i, depth int) bool {
		if depth == 0 && s[i] == c {
			found = i
			return false
		}
		return true
	})
	return found
}

func matchingBrace(s string, open int) (int, error) {
	found := -1
	err := walkTop(s[open:], func(i, depth int) bool {
		if s[open+i] == '}' && depth == 1 {
			found = open + i
			return false
		}
		return true
	})
	if err != nil {
		return 0, err
	}
	if found < 0 {
		return 0, errors.New("unbalanced brackets")
	}
	return found, nil
}
