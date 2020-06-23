package issue

import (
	"bytes"
	"fmt"
	"io"
	"unicode/utf8"
)

type H map[string]interface{}

type stringReader struct {
	i    int
	text string
}

func (r *stringReader) next() rune {
	if r.i >= len(r.text) {
		return 0
	}
	c := rune(r.text[r.i])
	if c < utf8.RuneSelf {
		r.i++
		return c
	}
	c, size := utf8.DecodeRuneInString(r.text[r.i:])
	if c == utf8.RuneError {
		panic(`Invalid unicode in string`)
	}
	r.i += size
	return c
}

// MapSprintf calls MapFprintf with a string Buffer and returns string that is output to that buffer
func MapSprintf(formatString string, args H) string {
	b := bytes.NewBufferString(``)
	_, err := MapFprintf(b, formatString, args)
	if err != nil {
		panic(err)
	}
	return b.String()
}

// MapFprintf is like fmt.Fprintf but it allows named arguments and it assumes a map as the arguments
// that follow the format string.
//
// The notation %{name} maps to the 'name' key of the map and uses the default format (%v)
// The notation %<name>2.2s maps to the 'name' key of the map and uses the %2.2s format.
func MapFprintf(writer io.Writer, formatString string, args H) (int, error) {
	posFormatString, argCount, expectedArgs := extractNamesAndLocations(formatString)
	posArgs := make([]interface{}, argCount)
	for k, v := range expectedArgs {
		var a interface{}
		if arg, ok := args[k]; ok {
			a = arg
		} else {
			a = fmt.Sprintf(`%%!{%s}(MISSING)`, k)
		}
		for _, pos := range v {
			posArgs[pos] = a
		}
	}
	return fmt.Fprintf(writer, posFormatString, posArgs...)
}

func extractNamesAndLocations(formatString string) (string, int, map[string][]int) {
	b := bytes.NewBufferString(``)
	rdr := stringReader{0, formatString}
	locations := make(map[string][]int, 8)
	c := rdr.next()
	location := 0
	for c != 0 {
		b.WriteRune(c)
		if c != '%' {
			c = rdr.next()
			continue
		}
		c = rdr.next()
		if c != '{' && c != '<' {
			if c != '%' {
				panic(fmt.Sprintf(`keyed formats cannot be combined with other %% formats at position %d in string '%s'`,
					rdr.i, formatString))
			}
			b.WriteRune(c)
			c = rdr.next()
			continue
		}
		ec := '}'
		bc := c
		if bc == '<' {
			ec = '>'
		}
		s := rdr.i
		c = rdr.next()
		for c != 0 && c != ec {
			c = rdr.next()
		}
		if c == 0 {
			panic(fmt.Sprintf(`unterminated %%%c at position %d in string '%s'`, bc, s-2, formatString))
		}
		e := rdr.i - 1
		if s == e {
			panic(fmt.Sprintf(`empty %%%c%c at position %d in string '%s'`, bc, ec, s-2, formatString))
		}
		key := formatString[s:e]
		if ps, ok := locations[key]; ok {
			locations[key] = append(ps, location)
		} else {
			locations[key] = []int{location}
		}
		location++
		if bc == '{' {
			// %{} constructs uses default format specifier whereas %<> uses whatever was specified
			b.WriteByte('v')
		}
		c = rdr.next()
	}
	return b.String(), location, locations
}
