package keywords

import "testing"

// FuzzNewRegexFromKeywords checks that an arbitrary keyword expression is either
// compiled or rejected, and never panics. Keyword expressions are currently
// embedded data, but they are a likely user-facing override, and a bad
// expression must not take the process down.
func FuzzNewRegexFromKeywords(f *testing.F) {
	for _, seed := range []string{
		"", "foo", "foo*", "foo?", "foo+", "(audio)?books?",
		"(auto)?biograph(y|ies)", "foo\\(", "foo(", ")", "((", "\\",
		"#*?+|()", "web(dl|rip)?", "a b", "A", "日*",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, keyword string) {
		r, err := NewRegexFromKeywords(keyword)
		if err != nil {
			return
		}

		if r == nil {
			t.Fatalf("nil expression and nil error for keyword %q", keyword)
		}

		r.MatchString("something")
	})
}
