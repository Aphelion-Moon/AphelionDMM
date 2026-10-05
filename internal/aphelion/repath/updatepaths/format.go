package updatepaths

import "strings"

func (r Rule) String() string {
	var b strings.Builder
	b.WriteString(r.Match.Path)
	if r.Match.Subtypes {
		b.WriteString(subtypesSuffix)
	}
	if len(r.Match.Filters) != 0 {
		entries := make([]string, len(r.Match.Filters))
		for n, filter := range r.Match.Filters {
			switch filter.Kind {
			case FilterNoEdits:
				entries[n] = "@UNSET"
			case FilterAny:
				entries[n] = filter.Name + " = @ANY"
			case FilterUnset:
				entries[n] = filter.Name + " = @UNSET"
			default:
				entries[n] = filter.Name + " = " + filter.Value
			}
		}
		b.WriteString("{" + strings.Join(entries, "; ") + "}")
	}
	b.WriteString(" : ")
	if r.Delete {
		b.WriteString("@DELETE")
		return b.String()
	}
	for n, output := range r.Outputs {
		if n != 0 {
			b.WriteString(", ")
		}
		b.WriteString(output.String())
	}
	return b.String()
}

func (o Output) String() string {
	var b strings.Builder
	switch o.Kind {
	case OutputKeepPath:
		b.WriteString("@OLD")
	case OutputSubtypes:
		b.WriteString(o.Path + subtypesSuffix)
	default:
		b.WriteString(o.Path)
	}
	if len(o.Props) == 0 {
		return b.String()
	}
	entries := make([]string, len(o.Props))
	for n, prop := range o.Props {
		switch prop.Kind {
		case PropAllOld:
			entries[n] = "@OLD"
		case PropSkip:
			entries[n] = prop.Name + " = @SKIP"
		case PropOld:
			entries[n] = prop.Name + " = @OLD"
		case PropOldOf:
			entries[n] = prop.Name + " = @OLD:" + prop.From
		default:
			entries[n] = prop.Name + " = " + prop.Value
		}
	}
	b.WriteString("{" + strings.Join(entries, "; ") + "}")
	return b.String()
}

// Format writes rules one per line in their given order.
func Format(rules []Rule) []byte {
	var b strings.Builder
	for _, rule := range rules {
		b.WriteString(rule.String())
		b.WriteByte('\n')
	}
	return []byte(b.String())
}
