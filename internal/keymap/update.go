package keymap

import (
	"regexp"
	"slices"
	"strings"
)

// A keybinds.toml written from unagit's template is the user's and
// unagit's at once, and Update keeps it that way across releases by one
// rule: a binding left commented out is unagit's, a binding uncommented is
// the user's. So Update rewrites the commented ones to the keys of today,
// adds the actions a release brought, drops the ones it took away, and
// puts today's descriptions on - and leaves every uncommented line, every
// note of the user's and every section of their own as they were.
//
// The file is paragraphs between blank lines: the header, a section's
// description with its [name], and each action's description with its
// binding. A description is "# Name: what it does", wrapped with "#   ",
// which is how Update tells it from a note of the user's.

// Describe is a description as the file writes it: "# " and the text,
// wrapped to the width of a terminal, its later lines indented.
func Describe(text string) []string {
	const width = 78
	var lines []string
	line := "#"
	for _, word := range strings.Fields(text) {
		if len(line) > 4 && len(line)+1+len(word) > width {
			lines = append(lines, line)
			line = "#  "
		}
		line += " " + word
	}
	return append(lines, line)
}

// Changes is what Update did, or would do.
type Changes struct {
	Added, Changed, Removed int
	// Rewritten is whether anything at all differs, descriptions included.
	Rewritten bool
}

var (
	commentedBinding = regexp.MustCompile(`^#\s*([A-Za-z0-9_]+)\s*=\s*(\[.*\]|".*")\s*$`)
	sectionLine      = regexp.MustCompile(`^\[\s*([A-Za-z0-9_]+)\s*\]\s*(#.*)?$`)
	bindingLine      = regexp.MustCompile(`^([A-Za-z0-9_]+)\s*=`)
)

// paragraph is a run of lines of the file, and what it is.
type paragraph struct {
	section   string   // the section it is in, or names
	id        string   // the action it binds
	about     []string // the comment lines above the section or binding line
	line      string   // the [section] or the binding line
	commented bool     // a binding left commented out
	header    bool     // a section's [name]
	other     []string // anything else: a note, a line Update does not know
}

func (p paragraph) lines() []string {
	if p.line == "" {
		return p.other
	}
	return append(slices.Clone(p.about), p.line)
}

// paragraphs reads a file into its paragraphs. A comment block ends at a
// blank line, or at the section or binding line it describes.
func paragraphs(text string) []paragraph {
	var out []paragraph
	var pending []string
	section := ""
	flush := func() {
		if len(pending) > 0 {
			out = append(out, paragraph{section: section, other: pending})
			pending = nil
		}
	}
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimRight(raw, " \t")
		switch m := sectionLine.FindStringSubmatch(line); {
		case line == "":
			flush()
		case m != nil:
			section = m[1]
			out = append(out, paragraph{section: section, about: pending, line: line, header: true})
			pending = nil
		case commentedBinding.MatchString(line):
			id := commentedBinding.FindStringSubmatch(line)[1]
			out = append(out, paragraph{section: section, id: id, about: pending, line: line, commented: true})
			pending = nil
		case bindingLine.MatchString(line):
			id := bindingLine.FindStringSubmatch(line)[1]
			out = append(out, paragraph{section: section, id: id, about: pending, line: line})
			pending = nil
		case strings.HasPrefix(line, "#"):
			pending = append(pending, line)
		default:
			// A line of TOML Update does not know - the rest of a list
			// written over several lines - stays with what it follows.
			if len(pending) == 0 && len(out) > 0 && out[len(out)-1].line != "" {
				last := &out[len(out)-1]
				last.line += "\n" + line
				continue
			}
			pending = append(pending, line)
		}
	}
	flush()
	return out
}

// described reports whether a comment block above a binding starts with
// the description template gives it, by its "# Name:", and how many of its
// lines that description takes; the rest are the user's.
func described(block, desc []string) (int, bool) {
	if len(block) == 0 || len(desc) == 0 {
		return 0, false
	}
	head := desc[0]
	if i := strings.Index(head, ": "); i > 0 {
		head = head[:i+1]
	}
	if !strings.HasPrefix(block[0], head) {
		return 0, false
	}
	n := 1
	for n < len(block) && strings.HasPrefix(block[n], "#   ") {
		n++
	}
	return n, true
}

// Update brings a keybinds.toml up to the template of this release (the
// reference with every binding commented out), keeping whatever is the
// user's.
func Update(user, template string) (string, Changes) {
	tmpl := paragraphs(template)
	var header []string
	if len(tmpl) > 0 && tmpl[0].line == "" && tmpl[0].section == "" {
		header = tmpl[0].other
	}
	type known struct {
		p     paragraph
		order int
	}
	sectionOf := map[string]paragraph{}
	bindings := map[string]map[string]known{}
	var sectionOrder []string
	for _, p := range tmpl {
		switch {
		case p.header:
			sectionOf[p.section] = p
			sectionOrder = append(sectionOrder, p.section)
			bindings[p.section] = map[string]known{}
		case p.id != "":
			bindings[p.section][p.id] = known{p, len(bindings[p.section])}
		}
	}

	mine := paragraphs(user)
	// An action the user bound is theirs wherever its line is; a commented
	// copy of it is not needed beside it.
	bound := map[[2]string]bool{}
	for _, p := range mine {
		if p.id != "" && !p.commented {
			bound[[2]string{p.section, p.id}] = true
		}
	}

	var ch Changes
	var out []paragraph
	if len(header) > 0 {
		out = append(out, paragraph{other: header})
	}
	seen := map[[2]string]bool{}
	for i, p := range mine {
		k := [2]string{p.section, p.id}
		tb, isKnown := bindings[p.section][p.id]
		switch {
		case i == 0 && p.line == "" && len(header) > 0 && len(p.other) > 0 && p.other[0] == header[0]:
			// unagit's header, written above already as it is now
		case p.header:
			if t, ok := sectionOf[p.section]; ok {
				out = append(out, t)
				continue
			}
			out = append(out, p)
		case p.id != "" && p.commented:
			if _, ok := bindings[p.section]; !ok {
				out = append(out, p) // a section unagit does not have: the user's
				continue
			}
			if !isKnown {
				ch.Removed++
				continue
			}
			if bound[k] || seen[k] {
				continue
			}
			seen[k] = true
			if p.line != tb.p.line {
				ch.Changed++
			}
			out = append(out, tb.p)
		case p.id != "":
			seen[k] = true
			if isKnown {
				if n, ok := described(p.about, tb.p.about); ok {
					p.about = append(slices.Clone(tb.p.about), p.about[n:]...)
				} else {
					p.about = append(slices.Clone(tb.p.about), p.about...)
				}
			}
			out = append(out, p)
		default:
			out = append(out, p)
		}
	}

	// What this release added goes where the template has it: after the
	// action before it, or at the top of its section, or - a section the
	// file does not have - at the end, whole.
	for _, section := range sectionOrder {
		var missing []known
		for id, b := range bindings[section] {
			if !seen[[2]string{section, id}] {
				missing = append(missing, b)
			}
		}
		slices.SortFunc(missing, func(a, b known) int { return a.order - b.order })
		if len(missing) == 0 {
			continue
		}
		ch.Added += len(missing)
		at := slices.IndexFunc(out, func(p paragraph) bool { return p.header && p.section == section })
		if at < 0 {
			out = append(out, sectionOf[section])
			for _, b := range missing {
				out = append(out, b.p)
			}
			continue
		}
		for _, b := range missing {
			pos := at + 1
			for j := at + 1; j < len(out) && !out[j].header; j++ {
				if o, ok := bindings[section][out[j].id]; ok && o.order < b.order {
					pos = j + 1
				}
			}
			out = slices.Insert(out, pos, b.p)
		}
	}

	var lines []string
	for i, p := range out {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, p.lines()...)
	}
	text := strings.Join(lines, "\n") + "\n"
	ch.Rewritten = text != strings.ReplaceAll(user, "\r\n", "\n")
	return text, ch
}
