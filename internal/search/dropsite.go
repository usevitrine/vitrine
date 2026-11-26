package search

import (
	"slices"
	"strings"
)

// DropSite takes a site out of a search as typed: system:site=a,b loses the names given (the word goes when none is
// left), and an OR group left with nothing, or a dangling ~, goes with it. Everything else stays as written. (A site
// wiped from the library: no saved search keeps its name.)
func DropSite(text string, names []string) string {
	var out []string
	for _, tok := range strings.Fields(text) {
		low := strings.ToLower(tok)
		for _, prefix := range []string{"system:site!=", "system:site="} {
			if !strings.HasPrefix(low, prefix) {
				continue
			}
			var keep []string
			for _, v := range strings.Split(tok[len(prefix):], ",") {
				if v != "" && !slices.ContainsFunc(names, func(n string) bool { return strings.EqualFold(n, v) }) {
					keep = append(keep, v)
				}
			}
			if len(keep) > 0 {
				tok = tok[:len(prefix)] + strings.Join(keep, ",") // (the prefix as typed)
			} else {
				tok = ""
			}
			break
		}
		if tok != "" {
			out = append(out, tok)
		}
	}
	// (an OR group's leftovers: "( ~ a )", "( a ~ )", "( a ~ ~ b )", "( )")
	for changed := true; changed; {
		changed = false
		for i := 0; i < len(out); i++ {
			prev, next := "", ""
			if i > 0 {
				prev = out[i-1]
			}
			if i+1 < len(out) {
				next = out[i+1]
			}
			if (out[i] == "~" && (prev == "(" || prev == "~" || next == ")" || prev == "" || next == "")) || (out[i] == "(" && next == ")") {
				n := 1
				if out[i] == "(" {
					n = 2
				}
				out = slices.Delete(out, i, i+n)
				changed = true
				break
			}
		}
	}
	return strings.Join(out, " ")
}
