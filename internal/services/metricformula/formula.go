// Package metricformula evaluates user-supplied metric formulas safely.
//
// A formula is an expression referencing sibling metrics by name using the
// ${metric_name} placeholder syntax. Example:
//
//	"${tasks_completed} / ${tasks_total} * 100"
//	"min(${hours_worked}, ${hours_planned})"
//
// Evaluation is performed by github.com/expr-lang/expr, a restricted
// expression engine — no I/O, no reflection, no user-defined functions
// beyond a small allowlist. Only arithmetic, comparison, and a handful of
// math helpers are exposed.
package metricformula

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/expr-lang/expr"
)

// placeholderRE matches ${metric_name}. Metric names are sanitized to [a-zA-Z0-9_]
// before substitution so the compiled expression is a valid identifier reference.
var placeholderRE = regexp.MustCompile(`\$\{([^}]+)\}`)

// identifierSanitizeRE replaces any char that isn't [A-Za-z0-9_] with _.
// Metric names can contain spaces, hyphens, etc.; we normalize for the expression.
var identifierSanitizeRE = regexp.MustCompile(`[^A-Za-z0-9_]+`)

// sanitizeIdent converts a metric name into a valid expr identifier.
//   "tasks completed" -> "tasks_completed"
//   "A/B Score"       -> "A_B_Score"
//   "42metric"        -> "_42metric"  (expr needs a leading letter/underscore)
func sanitizeIdent(name string) string {
	ident := identifierSanitizeRE.ReplaceAllString(name, "_")
	ident = strings.Trim(ident, "_")
	if ident == "" {
		return "_"
	}
	if ident[0] >= '0' && ident[0] <= '9' {
		ident = "_" + ident
	}
	return ident
}

// Evaluate computes a formula against sibling metric values.
//
//	formula: "${tasks_completed} / ${tasks_total} * 100"
//	siblings: map["tasks_completed"] = 42, map["tasks_total"] = 100
//
// Referenced siblings that are missing default to 0 — an unknown metric
// should not crash the pipeline but does log a warning at the service layer.
func Evaluate(formula string, siblings map[string]float64) (float64, error) {
	if strings.TrimSpace(formula) == "" {
		return 0, fmt.Errorf("formula is empty")
	}

	// Replace ${name} with sanitized identifiers and build the env.
	env := make(map[string]interface{}, len(siblings)+8)
	rewritten := placeholderRE.ReplaceAllStringFunc(formula, func(match string) string {
		raw := strings.TrimSpace(placeholderRE.FindStringSubmatch(match)[1])
		ident := sanitizeIdent(raw)
		// Lookup original name first; fallback to already-sanitized form.
		if v, ok := siblings[raw]; ok {
			env[ident] = v
		} else if v, ok := siblings[ident]; ok {
			env[ident] = v
		} else {
			env[ident] = 0.0
		}
		return ident
	})

	// Any sibling the formula didn't reference is still exposed in env so users
	// can write e.g. `a + b` without ${} too.
	for name, v := range siblings {
		ident := sanitizeIdent(name)
		if _, set := env[ident]; !set {
			env[ident] = v
		}
	}

	// Expose a small math helper surface.
	env["min"] = math.Min
	env["max"] = math.Max
	env["abs"] = math.Abs
	env["round"] = math.Round
	env["floor"] = math.Floor
	env["ceil"] = math.Ceil
	env["sqrt"] = math.Sqrt
	env["pow"] = math.Pow

	program, err := expr.Compile(rewritten, expr.Env(env), expr.AsFloat64())
	if err != nil {
		return 0, fmt.Errorf("compile formula: %w", err)
	}

	out, err := expr.Run(program, env)
	if err != nil {
		return 0, fmt.Errorf("run formula: %w", err)
	}

	val, ok := out.(float64)
	if !ok {
		return 0, fmt.Errorf("formula did not return a number: got %T", out)
	}
	if math.IsNaN(val) || math.IsInf(val, 0) {
		return 0, fmt.Errorf("formula produced non-finite result")
	}
	return val, nil
}
