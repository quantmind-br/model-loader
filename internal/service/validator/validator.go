// Package validator checks profiles against a FlagSchema and fixed rules.
package validator

import "github.com/quantmind-br/llama-cpp-loader/internal/domain"

// Severity grades a FieldIssue.
type Severity int

const (
	SeverityWarning Severity = iota
	SeverityError
)

// FieldIssue is a single rule violation, scoped to one Profile field.
type FieldIssue struct {
	Field    string
	Message  string
	Severity Severity
}

// Report aggregates issues from all rules.
type Report struct {
	Errors   []FieldIssue
	Warnings []FieldIssue
}

// HasBlockingErrors returns true when at least one issue is SeverityError.
func (r Report) HasBlockingErrors() bool {
	return len(r.Errors) > 0
}

// Validator runs all configured rules on a Profile.
type Validator interface {
	Validate(p domain.Profile, schema domain.FlagSchema) Report
}

// New returns a default Validator with the standard rule set.
func New() Validator {
	return defaultValidator{}
}

type defaultValidator struct{}

func (v defaultValidator) Validate(p domain.Profile, schema domain.FlagSchema) Report {
	rep := Report{}
	rep = applyTypeRules(p, schema, rep)
	rep = applyCrossFieldRules(p, rep)
	rep = applyExistenceRules(p, rep)
	return rep
}

func appendIssue(rep Report, issue FieldIssue) Report {
	if issue.Severity == SeverityError {
		rep.Errors = append(rep.Errors, issue)
	} else {
		rep.Warnings = append(rep.Warnings, issue)
	}
	return rep
}
