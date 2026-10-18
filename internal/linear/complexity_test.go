package linear

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

// Linear rejects queries above this complexity.
const maxComplexity = 10000

// score approximates Linear's complexity: objects cost 1, scalars 0.1, and
// connections multiply their children by the page size. It over-estimates
// (the old teams query scored 90k here and 55k on Linear), so it is a safe
// upper bound.
func score(ss ast.SelectionSet, vars map[string]float64) float64 {
	total := 0.0
	for _, sel := range ss {
		switch s := sel.(type) {
		case *ast.Field:
			if len(s.SelectionSet) == 0 {
				total += 0.1
				continue
			}
			mult := 1.0
			if a := s.Arguments.ForName("first"); a != nil {
				if a.Value.Kind == ast.Variable {
					mult = vars[a.Value.Raw]
				} else {
					fmt.Sscan(a.Value.Raw, &mult)
				}
			}
			total += mult * (1 + score(s.SelectionSet, vars))
		case *ast.FragmentSpread:
			total += score(s.Definition.SelectionSet, vars)
		case *ast.InlineFragment:
			total += score(s.SelectionSet, vars)
		}
	}
	return total
}

func TestQueryComplexity(t *testing.T) {
	schemaSrc, _ := os.ReadFile("schema.graphql")
	schema := gqlparser.MustLoadSchema(&ast.Source{Name: "s", Input: string(schemaSrc)})
	files, _ := filepath.Glob("queries/*.graphql")
	var all string
	for _, f := range files {
		b, _ := os.ReadFile(f)
		all += string(b) + "\n"
	}
	doc, errs := gqlparser.LoadQuery(schema, all)
	if errs != nil {
		t.Fatal(errs)
	}
	for _, op := range doc.Operations {
		if c := score(op.SelectionSet, map[string]float64{"first": pageSize}); c > maxComplexity {
			t.Errorf("%s: estimated complexity %.0f exceeds %d", op.Name, c, maxComplexity)
		}
	}
}
