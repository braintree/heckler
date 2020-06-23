package issue

import (
	"fmt"
)

// A Labeled is an object that has a label in the form of a string
type Labeled interface {
	// Returns a very brief description of this expression suitable to use in error messages
	Label() string
}

// A Named is an object that has a name in the form of a string
type Named interface {
	Name() string
}

// Label returns the Label for a Labeled argument, the Name for a Named argument, a string
// verbatim, or the resulting text from doing a Sprintf with "value of type %T" for other
// types of arguments.
func Label(e interface{}) string {
	if l, ok := e.(Labeled); ok {
		return l.Label()
	}
	if n, ok := e.(Named); ok {
		return n.Name()
	}
	if s, ok := e.(string); ok {
		return s
	}
	return fmt.Sprintf(`value of type %T`, e)
}
