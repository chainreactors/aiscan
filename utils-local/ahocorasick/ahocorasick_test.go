package ahocorasick

import "testing"

func TestBasicMatch(t *testing.T) {
	ac, err := NewBuilder().
		AddStrings([]string{"he", "she", "his", "hers"}).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	haystack := []byte("ushers")

	if !ac.IsMatch(haystack) {
		t.Error("expected IsMatch to be true")
	}

	m := ac.Find(haystack, 0)
	if m == nil {
		t.Fatal("expected a match")
	}
	t.Logf("First match: pattern=%d start=%d end=%d (%q)", m.PatternID, m.Start, m.End, haystack[m.Start:m.End])

	all := ac.FindAll(haystack, -1)
	t.Logf("All non-overlapping matches: %d", len(all))
	for _, m := range all {
		t.Logf("  pattern=%d [%d:%d] %q", m.PatternID, m.Start, m.End, haystack[m.Start:m.End])
	}

	overlapping := ac.FindAllOverlapping(haystack)
	t.Logf("All overlapping matches: %d", len(overlapping))
	for _, m := range overlapping {
		t.Logf("  pattern=%d [%d:%d] %q", m.PatternID, m.Start, m.End, haystack[m.Start:m.End])
	}
}

func TestNoMatch(t *testing.T) {
	ac, err := NewBuilder().
		AddStrings([]string{"foo", "bar", "baz"}).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	if ac.IsMatch([]byte("hello world")) {
		t.Error("expected no match")
	}

	if m := ac.Find([]byte("hello world"), 0); m != nil {
		t.Errorf("expected nil, got match at %d", m.Start)
	}
}

func TestManyPatterns(t *testing.T) {
	patterns := make([]string, 1000)
	for i := range patterns {
		patterns[i] = "pattern_" + itoa(i)
	}

	ac, err := NewBuilder().AddStrings(patterns).Build()
	if err != nil {
		t.Fatal(err)
	}

	// "unique_777" won't be a prefix of any other pattern
	haystack := []byte("this contains unique_777 somewhere")
	patterns2 := []string{"unique_777", "something_else"}
	ac2, err := NewBuilder().AddStrings(patterns2).Build()
	if err != nil {
		t.Fatal(err)
	}
	if !ac2.IsMatch(haystack) {
		t.Error("expected match for unique_777")
	}

	// With many patterns, verify IsMatch works
	haystack2 := []byte("this contains pattern_999 somewhere")
	if !ac.IsMatch(haystack2) {
		t.Error("expected match in many-pattern automaton")
	}

	m := ac.Find(haystack2, 0)
	if m == nil {
		t.Fatal("expected a match")
	}
	// LeftmostFirst: "pattern_9" (index 9) matches before "pattern_999" (index 999)
	matched := string(haystack2[m.Start:m.End])
	t.Logf("matched %q (pattern %d)", matched, m.PatternID)
}

func TestLeftmostLongest(t *testing.T) {
	ac, err := NewBuilder().
		SetMatchKind(LeftmostLongest).
		AddStrings([]string{"ab", "abc", "abcd"}).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	m := ac.Find([]byte("xabcdy"), 0)
	if m == nil {
		t.Fatal("expected a match")
	}
	if m.PatternID != 2 {
		t.Errorf("expected longest match (pattern 2 = 'abcd'), got pattern %d", m.PatternID)
	}
}

func TestCount(t *testing.T) {
	ac, err := NewBuilder().
		AddStrings([]string{"a"}).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	if got := ac.Count([]byte("banana")); got != 3 {
		t.Errorf("expected 3 matches, got %d", got)
	}
}

func BenchmarkIsMatch_Hit(b *testing.B) {
	patterns := []string{"error", "warning", "fatal", "panic", "critical"}
	ac, _ := NewBuilder().AddStrings(patterns).Build()
	haystack := make([]byte, 64*1024)
	copy(haystack[32*1024:], []byte("fatal error occurred"))

	b.SetBytes(int64(len(haystack)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ac.IsMatch(haystack)
	}
}

func BenchmarkIsMatch_Miss(b *testing.B) {
	patterns := []string{"error", "warning", "fatal", "panic", "critical"}
	ac, _ := NewBuilder().AddStrings(patterns).Build()
	haystack := make([]byte, 64*1024)
	for i := range haystack {
		haystack[i] = byte('a' + (i % 26))
	}

	b.SetBytes(int64(len(haystack)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ac.IsMatch(haystack)
	}
}

func BenchmarkFindAll_ManyPatterns(b *testing.B) {
	patterns := make([]string, 100)
	for i := range patterns {
		patterns[i] = "pat" + itoa(i)
	}
	ac, _ := NewBuilder().AddStrings(patterns).Build()
	haystack := []byte("prefix pat50 middle pat99 suffix pat0 end")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ac.FindAll(haystack, -1)
	}
}
