package validator

import (
	"strconv"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func applyCrossFieldRules(p domain.Profile, schema domain.FlagSchema, rep Report) Report {
	for _, rule := range schema.Rules {
		if !condHolds(rule.When, p, schema) {
			continue
		}
		sev := SeverityWarning
		if rule.Severity == "error" {
			sev = SeverityError
		}
		switch rule.Then.Kind {
		case "message":
			rep = appendIssue(rep, FieldIssue{Field: rule.When.Flag, Message: rule.Then.Message, Severity: sev})
		case "limit":
			if !compare(argString(p, rule.Then.Flag, schema), rule.Then.Op, rule.Then.Value, schema, rule.Then.Flag) {
				rep = appendIssue(rep, FieldIssue{
					Field:    rule.Then.Flag,
					Message:  ruleMessage(rule),
					Severity: sev,
				})
			}
		case "require":
			got := argString(p, rule.Then.Flag, schema)
			if got == "" || (rule.Then.Value != "" && got != rule.Then.Value) {
				rep = appendIssue(rep, FieldIssue{
					Field:    rule.Then.Flag,
					Message:  ruleMessage(rule),
					Severity: sev,
				})
			}
		}
	}
	return rep
}

func ruleMessage(r domain.CrossFieldRule) string {
	if r.Then.Message != "" {
		return r.Then.Message
	}
	return "cross-field rule violated (when " + r.When.Flag + ")"
}

func condHolds(c domain.Cond, p domain.Profile, schema domain.FlagSchema) bool {
	return compare(argString(p, c.Flag, schema), c.Op, c.Value, schema, c.Flag)
}

// argString renders the profile arg for flag as a string for comparison.
// Bools render "on"/"off"; numbers via strconv; missing -> "".
func argString(p domain.Profile, flag string, schema domain.FlagSchema) string {
	v, ok := p.Args[flag]
	if !ok {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "on"
		}
		return "off"
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	default:
		return ""
	}
}

// compare evaluates "got op want". For le/ge it tries numeric comparison and
// falls back to string comparison when either side is non-numeric.
func compare(got, op, want string, schema domain.FlagSchema, flag string) bool {
	switch op {
	case "eq":
		return got == want
	case "ne":
		return got != want
	case "le", "ge":
		gf, gerr := strconv.ParseFloat(got, 64)
		wf, werr := strconv.ParseFloat(want, 64)
		if gerr == nil && werr == nil {
			if op == "le" {
				return gf <= wf
			}
			return gf >= wf
		}
		if op == "le" {
			return got <= want
		}
		return got >= want
	}
	return false
}
