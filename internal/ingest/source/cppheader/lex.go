package cppheader

import "strings"

// tok is one C++ token. Preprocessor lines and plain comments never become
// tokens; a documentation comment (/** */, /*! */, ///, //!) is attached to
// the token that follows it.
type tok struct {
	k    byte // 'i' identifier/keyword, 'p' punctuation, 's' string/char literal, 'n' number
	s    string
	doc  string
	line int
}

func (t tok) is(s string) bool { return t.k != 's' && t.s == s }

// lex tokenizes a header. Conditional compilation keeps the first branch of
// every #if/#ifdef/#ifndef (the branch a documentation build sees, e.g.
// "defined(_CTRE_DOCS_)" or "__cplusplus"), except "#if 0", whose #else
// branch is kept instead. Macros are not expanded.
func lex(src string) []tok {
	var out []tok
	type frame struct{ active, taken bool }
	var stack []frame
	active := func() bool { return len(stack) == 0 || stack[len(stack)-1].active }
	pendingDoc := ""
	line := 1
	atLineStart := true
	emit := func(k byte, s string) {
		if !active() {
			return
		}
		out = append(out, tok{k: k, s: s, doc: pendingDoc, line: line})
		pendingDoc = ""
	}
	n := len(src)
	for i := 0; i < n; {
		c := src[i]
		switch {
		case c == '\n':
			line++
			atLineStart = true
			i++
			continue
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			i++
			continue
		case c == '\\' && i+1 < n && (src[i+1] == '\n' || src[i+1] == '\r'):
			i++ // line continuation outside a directive
			continue
		case c == '#' && atLineStart:
			j := i
			var d strings.Builder
			for j < n && src[j] != '\n' {
				if src[j] == '\\' && j+1 < n && (src[j+1] == '\n' || (src[j+1] == '\r' && j+2 < n && src[j+2] == '\n')) {
					if src[j+1] == '\r' {
						j++
					}
					j += 2
					line++
					d.WriteByte(' ')
					continue
				}
				if src[j] == '/' && j+1 < n && src[j+1] == '*' { // block comment inside a directive
					e := strings.Index(src[j+2:], "*/")
					if e < 0 {
						j = n
						break
					}
					line += strings.Count(src[j:j+2+e], "\n")
					j += e + 4
					continue
				}
				if src[j] == '/' && j+1 < n && src[j+1] == '/' {
					for j < n && src[j] != '\n' {
						j++
					}
					break
				}
				d.WriteByte(src[j])
				j++
			}
			i = j
			dir := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(d.String()), "#"))
			word, rest, _ := strings.Cut(dir, " ")
			rest = strings.TrimSpace(rest)
			parent := active()
			switch word {
			case "if", "ifdef", "ifndef":
				cond := word != "if" || rest != "0"
				stack = append(stack, frame{active: parent && cond, taken: cond})
			case "elif", "elifdef", "elifndef", "else":
				if len(stack) > 0 {
					f := &stack[len(stack)-1]
					outer := len(stack) == 1 || stack[len(stack)-2].active
					f.active = outer && !f.taken
					f.taken = true
				}
			case "endif":
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
			}
			continue
		}
		atLineStart = false
		switch {
		case c == '/' && i+1 < n && src[i+1] == '/':
			j := i
			for j < n && src[j] != '\n' {
				j++
			}
			body := src[i:j]
			if (strings.HasPrefix(body, "///") && !strings.HasPrefix(body, "////")) || strings.HasPrefix(body, "//!") {
				if !strings.HasPrefix(body[3:], "<") && active() { // "///<" documents the previous member
					pendingDoc += strings.TrimSpace(body[3:]) + "\n"
				}
			}
			i = j
		case c == '/' && i+1 < n && src[i+1] == '*':
			e := strings.Index(src[i+2:], "*/")
			end := n
			if e >= 0 {
				end = i + 2 + e + 2
			}
			body := src[i:end]
			line += strings.Count(body, "\n")
			if (strings.HasPrefix(body, "/**") || strings.HasPrefix(body, "/*!")) && body != "/**/" && !strings.HasPrefix(body, "/**<") && !strings.HasPrefix(body, "/*!<") && active() {
				pendingDoc = cleanBlock(body)
			}
			i = end
		case c == '"' || c == '\'':
			j := skipQuoted(src, i)
			line += strings.Count(src[i:j], "\n")
			emit('s', src[i:j])
			i = j
		case isIdentStart(c):
			j := i + 1
			for j < n && isIdentChar(src[j]) {
				j++
			}
			word := src[i:j]
			if j < n && src[j] == '"' && strings.HasSuffix(word, "R") && (word == "R" || word == "u8R" || word == "uR" || word == "UR" || word == "LR") {
				k := skipRaw(src, j)
				line += strings.Count(src[i:k], "\n")
				emit('s', src[i:k])
				i = k
				continue
			}
			if j < n && (src[j] == '"' || src[j] == '\'') && (word == "u8" || word == "u" || word == "U" || word == "L") {
				k := skipQuoted(src, j)
				emit('s', src[i:k])
				i = k
				continue
			}
			emit('i', word)
			i = j
		case c >= '0' && c <= '9' || (c == '.' && i+1 < n && src[i+1] >= '0' && src[i+1] <= '9'):
			j := i + 1
			for j < n {
				d := src[j]
				if isIdentChar(d) || d == '.' || (d == '\'' && j+1 < n && isIdentChar(src[j+1])) {
					j++
					continue
				}
				if (d == '+' || d == '-') && (src[j-1] == 'e' || src[j-1] == 'E' || src[j-1] == 'p' || src[j-1] == 'P') {
					j++
					continue
				}
				break
			}
			emit('n', src[i:j])
			i = j
		default:
			switch {
			case strings.HasPrefix(src[i:], "::"):
				emit('p', "::")
				i += 2
			case strings.HasPrefix(src[i:], "->"):
				emit('p', "->")
				i += 2
			case strings.HasPrefix(src[i:], "..."):
				emit('p', "...")
				i += 3
			default:
				emit('p', src[i:i+1])
				i++
			}
		}
	}
	return out
}

func isIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func isIdentChar(c byte) bool { return isIdentStart(c) || c >= '0' && c <= '9' }

// skipQuoted returns the index just past the string or character literal
// starting at src[i].
func skipQuoted(src string, i int) int {
	q := src[i]
	j := i + 1
	for j < len(src) {
		switch src[j] {
		case '\\':
			j += 2
			continue
		case q:
			return j + 1
		case '\n':
			return j // unterminated: stop at the line end
		}
		j++
	}
	return len(src)
}

// skipRaw skips R"delim( ... )delim" starting at the opening quote.
func skipRaw(src string, q int) int {
	open := strings.IndexByte(src[q:], '(')
	if open < 0 {
		return len(src)
	}
	delim := src[q+1 : q+open]
	end := strings.Index(src[q+open:], ")"+delim+`"`)
	if end < 0 {
		return len(src)
	}
	return q + open + end + len(delim) + 2
}

// cleanBlock strips the comment markers and leading asterisks of a
// documentation block comment.
func cleanBlock(body string) string {
	body = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(body, "/**"), "/*!"), "*/")
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		l = strings.TrimSpace(l)
		l = strings.TrimLeft(l, "*")
		lines[i] = strings.TrimSpace(l)
	}
	return strings.Join(lines, "\n")
}
