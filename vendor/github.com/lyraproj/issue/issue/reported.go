package issue

import (
	"bytes"
	"regexp"
	"runtime"
	"sort"
	"strconv"
)

// A Reported instance contains information of an issue such as an
// error or a warning. It contains an Issue and arguments needed to
// format that issue. It also contains the location where the issue
// was reported.
type Reported interface {
	// Argument returns the argument for the given key or nil if no
	// such argument exists
	Argument(key string) interface{}

	// Keys returns the list of argument keys
	Keys() []string

	// Code returns the issue code
	Code() Code

	// Cause returns the cause of this Reported issue, if any.
	Cause() error

	// Error produces a string from the receives issue and arguments
	Error() string

	// Error produces a string from the receives issue and arguments
	// and writes it to the given buffer
	ErrorTo(*bytes.Buffer)

	// Location returns the location where the issue was reported
	Location() Location

	// OffsetByLocation returns a copy of the receiver where the location
	// is offset by the given location. This is useful when the original
	// source is embedded in a another file.
	OffsetByLocation(location Location) Reported

	// WithLocation returns a copy of the receiver where the location has
	// been overwritten by the argument
	WithLocation(location Location) Reported

	// Severity returns the severity
	Severity() Severity

	// Stack returns the formatted stack trace of the error or an empty string if
	// stack traces where not enabled using IncludeStackTrace()
	Stack() string

	// String is an alias for Error
	String() string
}

type reported struct {
	issueCode Code
	severity  Severity
	args      H
	location  Location
	stack     string
	cause     error
}

var includeStacktrace = false

// IncludeStacktrace can be set to true to get all Reported to include a stacktrace.
func IncludeStacktrace(flag bool) {
	includeStacktrace = flag
}

// ErrorWithoutStack creates a new instance of the Reported based on code, arguments, and location. It
// does not add a stack trace.
//
// This constructor is mainly intended for errors that are propagated from an external executable and a
// local stack trace would not make sense.
func ErrorWithStack(code Code, args H, location Location, cause error, stack string) Reported {
	return &reported{code, SeverityError, args, location, stack, cause}
}

// ErrorWithoutStack is deprecated and will be removed in future versions of the code
func ErrorWithoutStack(code Code, args H, location Location, cause error) Reported {
	return ErrorWithStack(code, args, location, cause, ``)
}

// NewNested creates a new instance of the Reported error with a given Code,  argument hash, and nested error.
// The  locOrSkip must either be nil, a Location, or an int denoting the number of frames to skip in a stacktrace,
// counting from the caller of NewNested.
func NewNested(code Code, args H, locOrSkip interface{}, cause error) Reported {
	return newReported(code, SeverityError, args, locOrSkip, cause)
}

// NewReported creates a new instance of the Reported with a given Code, Severity, and argument hash. The
// locOrSkip must either be nil, a Location, or an int denoting the number of frames to skip in a stacktrace,
// counting from the caller of NewReported.
func NewReported(code Code, severity Severity, args H, locOrSkip interface{}) Reported {
	return newReported(code, severity, args, locOrSkip, nil)
}

func newReported(code Code, severity Severity, args H, locOrSkip interface{}, cause error) Reported {

	var location Location
	skip := 0
	switch locOrSkip := locOrSkip.(type) {
	case int:
		skip = locOrSkip
	case Location:
		location = locOrSkip
	}

	skip += 3 // Always skip runtime.Callers and this function
	r := &reported{code, severity, args, location, ``, cause}

	var topFrame *runtime.Frame

	if includeStacktrace {
		// Ask runtime.Callers for up to 100 pcs, including runtime.Callers itself.
		pc := make([]uintptr, 100)
		n := runtime.Callers(skip, pc)
		if n > 0 {
			pc = pc[:n] // pass only valid pcs to runtime.CallersFrames
			frames := runtime.CallersFrames(pc)

			// Loop to get frames.
			// A fixed number of pcs can expand to an indefinite number of Frames.
			b := bytes.NewBufferString(``)
			for {
				if f, more := frames.Next(); more {
					if topFrame == nil {
						topFrame = &f
					}
					b.WriteString("\n at ")
					b.WriteString(f.File)
					b.WriteByte(':')
					b.WriteString(strconv.Itoa(f.Line))
					if f.Function != `` {
						b.WriteString(" (")
						b.WriteString(f.Function)
						b.WriteByte(')')
					}
				} else {
					break
				}
			}
			r.stack = b.String()
		}
	}

	if r.location == nil {
		if topFrame == nil {
			// Use first caller we can find with regards to given skip and use it
			// as the location
			for {
				// Start by decrementing to even out the different interpretations of skip between runtime.Caller
				// and runtime.Callers
				skip--
				if _, f, l, ok := runtime.Caller(skip); ok {
					r.location = NewLocation(f, l, 0)
					break
				}
			}
		} else {
			// Set location to first stack entry
			r.location = NewLocation(topFrame.File, topFrame.Line, 0)
		}
	}
	return r
}

func (ri *reported) Argument(key string) interface{} {
	return ri.args[key]
}

func (ri *reported) Keys() []string {
	keys := make([]string, 0, len(ri.args))
	for k := range ri.args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (ri *reported) OffsetByLocation(location Location) Reported {
	loc := ri.location
	if loc == nil {
		loc = location
	} else {
		loc = NewLocation(location.File(), location.Line()+loc.Line(), location.Pos())
	}
	return &reported{ri.issueCode, ri.severity, ri.args, loc, ri.stack, ri.cause}
}

func (ri *reported) WithLocation(location Location) Reported {
	return &reported{ri.issueCode, ri.severity, ri.args, location, ri.stack, ri.cause}
}

func (ri *reported) Error() (str string) {
	b := bytes.NewBufferString(``)
	ri.ErrorTo(b)
	return b.String()
}

var hasLocationPattern = regexp.MustCompile(`[^\w]file:\s+|[^\w]line:\s+`)

func (ri *reported) ErrorTo(b *bytes.Buffer) {
	ForCode(ri.issueCode).Format(b, ri.args)
	if ri.location != nil {
		if !hasLocationPattern.MatchString(b.String()) {
			b.WriteByte(' ')
			appendLocation(b, ri.location)
		}
	}
	b.WriteString(ri.stack)
	if ri.cause != nil {
		b.WriteString("\nCaused by: ")
		b.WriteString(ri.cause.Error())
	}
}

func (ri *reported) Cause() error {
	return ri.cause
}

func (ri *reported) Location() Location {
	return ri.location
}

func (ri *reported) Stack() string {
	return ri.stack
}

func (ri *reported) String() string {
	return ri.Error()
}

func (ri *reported) Code() Code {
	return ri.issueCode
}

func (ri *reported) Severity() Severity {
	return ri.severity
}
