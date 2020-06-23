package issue

type (
	Result interface {
		// Error returns true if errors where found or false if this result
		// contains only warnings.
		Error() bool

		// Issues returns all errors and warnings. An empty slice is returned
		// when no errors or warnings were generated
		Issues() []Reported
	}

	result struct {
		issues []Reported
	}
)

// NewResult creates a Result from a slice of Reported
func NewResult(issues []Reported) Result {
	return &result{issues}
}

func (pr *result) Error() bool {
	for _, i := range pr.issues {
		if i.Severity() == SeverityError {
			return true
		}
	}
	return false
}

func (pr *result) Issues() []Reported {
	return pr.issues
}
