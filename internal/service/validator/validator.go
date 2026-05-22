// Package validator checks profiles against a FlagSchema and fixed rules.
package validator

import (
	"log/slog"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/log"
)

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
	Validate(p domain.Profile, schema domain.FlagSchema, kind domain.BackendKind) Report
}

// New returns a default Validator with the standard rule set. logger may be
// nil; nil → log.Nop() (no-op handler). Production wires the *slog.Logger
// from internal/log; the profile_editor passes log.Nop() because its
// validator runs out of the spawn correlation path.
func New(logger *slog.Logger) Validator {
	if logger == nil {
		logger = log.Nop()
	}
	return defaultValidator{logger: logger}
}

type defaultValidator struct {
	logger *slog.Logger
}

func (v defaultValidator) Validate(p domain.Profile, schema domain.FlagSchema, kind domain.BackendKind) Report {
	rep := Report{}
	rep = applyTypeRules(p, schema, rep)
	rep = applyExtraArgsRules(p, schema, rep)
	rep = applyRequiredRules(p, schema, rep)
	rep = applyCrossFieldRules(p, schema, rep)
	rep = applyExistenceRules(p, kind, rep)
	if rep.HasBlockingErrors() {
		v.logger.Info("validation_failed",
			"profile_id", p.ID,
			"error_count", len(rep.Errors),
			"warning_count", len(rep.Warnings))
	}
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
