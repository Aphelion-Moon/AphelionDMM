// Package varclip holds copied object variables in the variable editor so
// they can be pasted onto another instance or prefab.
package varclip

import "sort"

// Entry is one copied variable as DM source text.
type Entry struct {
	Name, Value string
}

// Clipboard is the editor-wide variable clipboard. It is local UI state; a
// paste still goes through the normal edit path and its validation.
type Clipboard struct {
	source  string
	entries []Entry
}

// Set replaces the clipboard with entries copied from source (a type path).
// Later entries with the same name replace earlier ones.
func (c *Clipboard) Set(source string, entries []Entry) {
	byName := make(map[string]int, len(entries))
	c.entries = c.entries[:0]
	for _, entry := range entries {
		if idx, ok := byName[entry.Name]; ok {
			c.entries[idx] = entry
			continue
		}
		byName[entry.Name] = len(c.entries)
		c.entries = append(c.entries, entry)
	}
	sort.Slice(c.entries, func(i, j int) bool { return c.entries[i].Name < c.entries[j].Name })
	c.source = source
}

func (c *Clipboard) Clear()           { c.source, c.entries = "", nil }
func (c *Clipboard) Empty() bool      { return len(c.entries) == 0 }
func (c *Clipboard) Source() string   { return c.source }
func (c *Clipboard) Entries() []Entry { return append([]Entry(nil), c.entries...) }

// Value is the copied value for name, if one was copied.
func (c *Clipboard) Value(name string) (string, bool) {
	for _, entry := range c.entries {
		if entry.Name == name {
			return entry.Value, true
		}
	}
	return "", false
}

// Modified lists the variables whose current value differs from the type's
// default, i.e. what makes this object different from a freshly placed one.
func Modified(names []string, current, initial func(string) string) []Entry {
	var entries []Entry
	for _, name := range names {
		if value := current(name); value != initial(name) {
			entries = append(entries, Entry{Name: name, Value: value})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

// Target describes the object a paste lands on.
type Target struct {
	// Defined reports whether the target type declares the variable.
	Defined func(string) bool
	// ReadOnly reports variables the editor refuses to change.
	ReadOnly func(string) bool
	// Current is the target's current value.
	Current func(string) string
}

// Plan splits copied entries into those that change the target and those it
// cannot take. Entries already equal to the target's value are dropped.
func Plan(entries []Entry, target Target) (apply []Entry, skipped []string) {
	for _, entry := range entries {
		if !target.Defined(entry.Name) || target.ReadOnly(entry.Name) {
			skipped = append(skipped, entry.Name)
			continue
		}
		if target.Current(entry.Name) == entry.Value {
			continue
		}
		apply = append(apply, entry)
	}
	return apply, skipped
}
