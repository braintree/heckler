package issue

import (
	"fmt"
)

// this would be an enum in most other languages
const (
	SeverityIgnore      = Severity(1)
	SeverityDeprecation = Severity(2)
	SeverityWarning     = Severity(3)
	SeverityError       = Severity(4)
)

// Severity used in reported issues
type Severity int

// String returns the severity in lowercase string form
func (severity Severity) String() string {
	switch severity {
	case SeverityIgnore:
		return `ignore`
	case SeverityDeprecation:
		return `deprecation`
	case SeverityWarning:
		return `warning`
	case SeverityError:
		return `error`
	default:
		panic(fmt.Sprintf(`Illegal severity level: %d`, severity))
	}
}

// AssertValid checks that the given severity is one of the recognized severities
func (severity Severity) AssertValid() {
	switch severity {
	case SeverityIgnore, SeverityDeprecation, SeverityWarning, SeverityError:
		return
	default:
		panic(fmt.Sprintf(`Illegal severity level: %d`, severity))
	}
}
