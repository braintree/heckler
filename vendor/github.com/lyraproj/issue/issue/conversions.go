package issue

import (
	"bytes"
	"fmt"
	"math"
	"unicode"
)

// AnOrA returns the non capitalized article for the label of the given argument
func AnOrA(e interface{}) string {
	label := Label(e)
	return fmt.Sprintf(`%s %s`, Article(label), label)
}

// UcAnOrA returns the capitalized article for the label of the given argument
func UcAnOrA(e interface{}) string {
	label := Label(e)
	return fmt.Sprintf(`%s %s`, ArticleUc(label), label)
}

// CamelToSnakeCase converts a camel cased name like "NameIsBob" to
// its corresponding snake cased "name_is_bob"
func CamelToSnakeCase(name string) string {
	b := bytes.NewBufferString(``)
	leadIn := true
	mlUpper := false
	var p rune = -1
	for _, c := range name {
		if leadIn && c == '_' {
			b.WriteByte('_')
			continue
		}
		r := c
		if unicode.IsUpper(r) {
			mlUpper = unicode.IsUpper(p)
			if !(leadIn || p == '_' || mlUpper) {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		} else if mlUpper {
			mlUpper = false
			if !(leadIn || r == '_') {
				b.WriteByte('_')
			}
		}
		b.WriteRune(r)
		p = c
		leadIn = false
	}
	return b.String()
}

// FirstToLower ensures that the first character in a name like "NameIsBob" is lowercase. Leading
//// underscore characters are left as is.
func FirstToLower(name string) string {
	b := bytes.NewBufferString(``)

	firstChar := true
	for _, c := range name {
		if c == '_' {
			// Don't alter firstChar status
			b.WriteRune(c)
			continue
		}
		if firstChar {
			c = unicode.ToLower(c)
			firstChar = false
		}
		b.WriteRune(c)
	}
	return b.String()
}

// SnakeToCamelCase converts a snake cased name like "name_is_bob" to
// its corresponding camel cased "NameIsBob"
func SnakeToCamelCase(name string) string {
	return snakeToCamelCase(name, true)
}

// SnakeToCamelCaseDC converts a snake cased name like "name_is_bob" to
// its corresponding camel cased de-capitalized name "nameIsBob". Leading
// underscore characters are left as is.
func SnakeToCamelCaseDC(name string) string {
	return snakeToCamelCase(name, false)
}

func snakeToCamelCase(name string, nextUpper bool) string {
	b := bytes.NewBufferString(``)

	nonUnderscorePrefix := true
	for _, c := range name {
		if c == '_' {
			if nonUnderscorePrefix {
				b.WriteRune(c)
			} else {
				nextUpper = true
			}
			continue
		}
		if nextUpper {
			c = unicode.ToUpper(c)
			nextUpper = false
		}
		b.WriteRune(c)
		nonUnderscorePrefix = false
	}
	return b.String()
}

// Article returns the non capitalized article for the given string
func Article(s string) string {
	if s == `` {
		return `a`
	}
	switch s[0] {
	case 'A', 'E', 'I', 'O', 'U', 'Y', 'a', 'e', 'i', 'o', 'u', 'y':
		return `an`
	default:
		return `a`
	}
}

// ArticleUc returns the capitalized article for the given string
func ArticleUc(s string) string {
	if s == `` {
		return `A`
	}
	switch s[0] {
	case 'A', 'E', 'I', 'O', 'U', 'Y', 'a', 'e', 'i', 'o', 'u', 'y':
		return `An`
	default:
		return `A`
	}
}

// JoinErrors joins a set of errors into a string using newline separators. The argument
// can be a Result, a Reported, an error, a string, or a slice of Reported, error, or string,
func JoinErrors(e interface{}) string {
	b := bytes.NewBufferString(``)
	switch e := e.(type) {
	case Result:
		for _, err := range e.Issues() {
			b.WriteString("\n")
			err.ErrorTo(b)
		}
	case []Reported:
		for _, err := range e {
			b.WriteString("\n")
			err.ErrorTo(b)
		}
	case []error:
		for _, err := range e {
			b.WriteString("\n")
			b.WriteString(err.Error())
		}
	case []string:
		for _, err := range e {
			b.WriteString("\n")
			b.WriteString(err)
		}
	case Reported:
		e.ErrorTo(b)
	case error:
		b.WriteString(e.Error())
	case string:
		b.WriteString(e)
	}
	return b.String()
}

// Unindent determines the maximum indent that can be stripped by looking at leading whitespace on all lines. Lines that
// consists entirely of whitespace are not included in the computation.
// Strips first line if it's empty, then strips the computed indent from all lines and returns the result.
//
func Unindent(str string) string {
	minIndent := computeIndent(str)
	if minIndent == 0 {
		return str
	}
	r := bytes.NewBufferString(str)
	b := bytes.NewBufferString("")
	first := true
	for {
		line, err := r.ReadString('\n')
		if first {
			first = false
			if line == "\n" {
				continue
			}
		}
		if len(line) > minIndent {
			b.WriteString(line[minIndent:])
		} else if err == nil {
			b.WriteByte('\n')
		} else {
			break
		}
	}
	return b.String()
}

func computeIndent(str string) int {
	minIndent := math.MaxInt64
	r := bytes.NewBufferString(str)
	for minIndent > 0 {
		line, err := r.ReadString('\n')
		ll := len(line)

		for wsCount := 0; wsCount < minIndent && wsCount < ll; wsCount++ {
			c := line[wsCount]
			if c != ' ' && c != '\t' {
				if c != '\n' {
					minIndent = wsCount
				}
				break
			}
		}
		if err != nil {
			break
		}
	}
	if minIndent == math.MaxInt64 {
		minIndent = 0
	}
	return minIndent
}
