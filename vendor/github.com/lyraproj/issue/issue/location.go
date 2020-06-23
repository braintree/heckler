package issue

import (
	"bytes"
	"regexp"
	"strconv"
)

type Location interface {
	File() string

	Line() int

	// Position on line
	Pos() int
}

type Located interface {
	Location() Location
}

type location struct {
	file string
	line int
	pos  int
}

func NewLocation(file string, line, pos int) Location {
	return &location{file, line, pos}
}

var locationPattern = regexp.MustCompile(`^\((?:file:\s*(.+?),?,?)?(?:(?:\s*line:\s*([0-9]+))(?:,\s*column:\s*([0-9]+))?)?\)$`)

func ParseLocation(loc string) Location {
	if parts := locationPattern.FindStringSubmatch(loc); parts != nil {
		file := parts[1]
		line := 0
		if ls := parts[2]; ls != `` {
			line, _ = strconv.Atoi(ls)
		}
		pos := 0
		if ps := parts[3]; ps != `` {
			pos, _ = strconv.Atoi(ps)
		}
		return &location{file, line, pos}
	}
	return &location{loc, 0, 0}
}

func (l *location) File() string {
	return l.file
}

func (l *location) Line() int {
	return l.line
}

func (l *location) Pos() int {
	return l.pos
}

func LocationString(location Location) string {
	b := bytes.NewBufferString(``)
	appendLocation(b, location)
	return b.String()
}

func appendLocation(b *bytes.Buffer, location Location) {
	if location == nil {
		return
	}
	file := location.File()
	line := location.Line()
	if file == `` && line <= 0 {
		return
	}

	pos := location.Pos()
	b.WriteByte('(')
	if file != `` {
		b.WriteString(`file: `)
		b.WriteString(file)
		if line > 0 {
			b.WriteString(`, `)
		}
	}
	if line > 0 {
		b.WriteString(`line: `)
		b.WriteString(strconv.Itoa(line))
		if pos > 0 {
			b.WriteString(`, column: `)
			b.WriteString(strconv.Itoa(pos))
		}
	}
	b.WriteByte(')')
}
