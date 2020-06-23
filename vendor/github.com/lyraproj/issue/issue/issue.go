package issue

import (
	"bytes"
	"fmt"
)

var NoArgs = H{}

// A Code is a unique string representation of an issue. It should be all uppercase
// and words must be separated by underscores, not spaces.
// Since all issues live in the same global namespace, it's recommended that the
// code is prefixed with a package name.
//
// Example:
//
// const EVAL_UNKNOWN_FUNCTION = `EVAL_UNKNOWN_FUNCTION`
//
type Code string

// An ArgFormatter function, provided to the Issue constructors via the HF map, is
// responsible for formatting a named argument in the format string before the final
// formatting takes place.
//
// Typical formatters are AnOrA or UcAnOrA. Both will prefix the named argument
// with an article. The difference between the two is that UcAnOrA uses a capitalized
// article.
type ArgFormatter func(value interface{}) string

// A HF is used for passing ArgFormatters to an Issue constructor
type HF map[string]ArgFormatter

// An Issue is a formal description of a warning or an error.
type Issue interface {
	// ArgFormatters returns the argument formatters or nil if no such
	// formatters exists
	ArgFormatters() HF

	// Code returns the issue code
	Code() Code

	// Format uses the receivers format string and the given arguments to
	// write a string onto the given buffer
	Format(b *bytes.Buffer, arguments H)

	// IsDemotable returns false for soft issues and true for hard issues
	IsDemotable() bool

	// MessageFormat returns the format used when formatting the receiver
	MessageFormat() string
}

type issue struct {
	code          Code
	messageFormat string
	argFormats    HF
	demotable     bool
}

var issues = map[Code]*issue{}

// Hard creates a non-demotable Issue with the given code and messageFormat
func Hard(code Code, messageFormat string) Issue {
	return addIssue(code, messageFormat, false, nil)
}

// Hard2 creates a non-demotable Issue with the given code, messageFormat, and
// argFormatters map.
func Hard2(code Code, messageFormat string, argFormatters HF) Issue {
	return addIssue(code, messageFormat, false, argFormatters)
}

// Hard creates a demotable Issue with the given code and messageFormat
func Soft(code Code, messageFormat string) Issue {
	return addIssue(code, messageFormat, true, nil)
}

// Soft2 creates a demotable Issue with the given code, messageFormat, and
// argFormatters map.
func Soft2(code Code, messageFormat string, argFormats HF) Issue {
	return addIssue(code, messageFormat, true, argFormats)
}

func (issue *issue) ArgFormatters() HF {
	return issue.argFormats
}

func (issue *issue) Code() Code {
	return issue.code
}

func (issue *issue) IsDemotable() bool {
	return issue.demotable
}

func (issue *issue) MessageFormat() string {
	return issue.messageFormat
}

func (issue *issue) Format(b *bytes.Buffer, arguments H) {
	var args H
	af := issue.ArgFormatters()
	if af != nil {
		args = make(H, len(arguments))
		for k, v := range arguments {
			if a, ok := af[k]; ok {
				v = a(v)
			}
			args[k] = v
		}
	} else {
		args = arguments
	}
	_, _ = MapFprintf(b, issue.MessageFormat(), args)
}

// Returns the Issue for a Code. Will panic if the given code does not represent
// an existing issue
func ForCode(code Code) Issue {
	if dsc, ok := issues[code]; ok {
		return dsc
	}
	panic(fmt.Sprintf("internal error: no issue found for issue code '%s'", code))
}

// Returns the Issue for a Code together with a bool indicating if the issue was
// found or not
func ForCode2(code Code) (dsc Issue, ok bool) {
	dsc, ok = issues[code]
	return
}

func addIssue(code Code, messageFormat string, demotable bool, argFormats HF) Issue {
	dsc := &issue{code, messageFormat, argFormats, demotable}
	issues[code] = dsc
	return dsc
}

func withIssues(example func()) {
	savedIssues := issues
	defer func() {
		issues = savedIssues
	}()
	issues = map[Code]*issue{}
	example()
}
