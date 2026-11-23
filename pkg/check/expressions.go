package check

import (
	"fmt"
	"reflect"
	"text/template"

	"github.com/intentd/jig/pkg/spec"
	"github.com/intentd/tools/assert"
	"github.com/intentd/tools/condition"
	"github.com/intentd/tools/resolver"
)

// expressions reports each template and CEL value that can't run, where
// jig evaluates them: every when:/or: condition and every external call,
// wherever they are in the test. A syntax error, a function that doesn't
// exist (base, KEL's when nil, and the jig's own) and a call with the wrong number or type
// of arguments fail here instead of at run time. Each distinct value is
// checked once.
func expressions(d *spec.Declaration, base template.FuncMap) []error {
	funcs := resolver.NewWithFunctions(base, d.Functions.Functions()...).Funcs()
	seen := map[string]bool{}
	var errs []error
	check := func(s string) {
		if seen[s] {
			return
		}
		seen[s] = true
		if err := resolver.Check(s, funcs, nil); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s, err))
		}
	}
	walkValues(reflect.ValueOf(d).Elem(), func(v reflect.Value) bool {
		switch a := v.Interface().(type) {
		case condition.Condition, spec.ExternalCall:
			eachString(v, check)
			return false
		case assert.Assertions:
			// match: under assert:, its expressions as the json: it makes.
			for _, j := range assert.MatchAssertions(a.Match) {
				if j.Expr != "" && !seen["expr:"+j.Expr] {
					seen["expr:"+j.Expr] = true
					if err := assert.CheckExpr(j.Expr); err != nil {
						errs = append(errs, fmt.Errorf("match %s: %w", j.Path, err))
					}
				}
			}
			return true
		case assert.TextAssertion:
			// expr: compiles with value and object, wherever an assertion is.
			if a.Expr != "" && !seen["expr:"+a.Expr] {
				seen["expr:"+a.Expr] = true
				if err := assert.CheckExpr(a.Expr); err != nil {
					errs = append(errs, fmt.Errorf("expr %s: %w", a.Expr, err))
				}
			}
			return false
		}
		return true
	})
	return errs
}

// walkValues calls visit on v and, while visit returns true, on everything
// v holds: struct fields, pointers, slices, maps and interfaces.
func walkValues(v reflect.Value, visit func(reflect.Value) bool) {
	if !v.IsValid() || !v.CanInterface() || !visit(v) {
		return
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			walkValues(v.Elem(), visit)
		}
	case reflect.Struct:
		for i := range v.NumField() {
			walkValues(v.Field(i), visit)
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			walkValues(v.Index(i), visit)
		}
	case reflect.Map:
		for it := v.MapRange(); it.Next(); {
			walkValues(it.Value(), visit)
		}
	}
}

// eachString calls fn with every string v holds.
func eachString(v reflect.Value, fn func(string)) {
	walkValues(v, func(v reflect.Value) bool {
		if v.Kind() == reflect.String {
			fn(v.String())
			return false
		}
		return true
	})
}
