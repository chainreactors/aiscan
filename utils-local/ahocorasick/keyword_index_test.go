package ahocorasick

import "testing"

func TestKeywordIndex_Basic(t *testing.T) {
	idx := NewKeywordIndexBuilder().
		AddKeyword("nginx", 1).
		AddKeyword("apache", 2).
		AddKeyword("nginx", 3).
		AddFallback(99).
		Build()

	result := idx.MatchSources([]byte("powered by nginx"))
	if !result[1] {
		t.Error("expected source 1 (nginx)")
	}
	if !result[3] {
		t.Error("expected source 3 (nginx)")
	}
	if result[2] {
		t.Error("unexpected source 2 (apache)")
	}
	if !result[99] {
		t.Error("expected fallback source 99")
	}
}

func TestKeywordIndex_Dedup(t *testing.T) {
	idx := NewKeywordIndexBuilder().
		AddKeyword("foo", 1).
		AddKeyword("foo", 1).
		AddKeyword("foo", 2).
		Build()

	result := idx.MatchSources([]byte("foo"))
	if !result[1] || !result[2] {
		t.Errorf("expected sources 1 and 2, got %v", result)
	}
}

func TestKeywordIndex_Empty(t *testing.T) {
	idx := NewKeywordIndexBuilder().
		AddFallback(5).
		Build()

	result := idx.MatchSources([]byte("anything"))
	if !result[5] {
		t.Error("expected fallback 5")
	}
	if len(result) != 1 {
		t.Errorf("expected 1 result, got %d", len(result))
	}
}

func TestKeywordIndex_Overlapping(t *testing.T) {
	idx := NewKeywordIndexBuilder().
		SetOverlapping(true).
		AddKeyword("server: microsoft", 1).
		AddKeyword("server: microsoft-iis", 2).
		Build()

	result := idx.MatchSources([]byte("server: microsoft-iis/10.0"))
	if !result[1] {
		t.Error("expected source 1 (shorter overlap)")
	}
	if !result[2] {
		t.Error("expected source 2 (longer overlap)")
	}
}

func TestKeywordIndex_NonOverlapping(t *testing.T) {
	idx := NewKeywordIndexBuilder().
		AddKeyword("server: microsoft", 1).
		AddKeyword("server: microsoft-iis", 2).
		Build()

	result := idx.MatchSources([]byte("server: microsoft-iis/10.0"))
	// Non-overlapping: only one of them should match
	if len(result) == 0 {
		t.Error("expected at least one match")
	}
}

func TestDualKeywordIndex_Basic(t *testing.T) {
	idx := NewDualKeywordIndexBuilder().
		AddBodyKeyword("wordpress", 1).
		AddHeaderKeyword("nginx", 2).
		AddFallback(99).
		Build()

	result := idx.MatchSources(
		[]byte("server: nginx\n"),
		[]byte("<meta name='generator' content='wordpress'>"),
	)

	if !result[1] {
		t.Error("expected body source 1 (wordpress)")
	}
	if !result[2] {
		t.Error("expected header source 2 (nginx)")
	}
	if !result[99] {
		t.Error("expected fallback source 99")
	}
}

func TestDualKeywordIndex_BodyOnly(t *testing.T) {
	idx := NewDualKeywordIndexBuilder().
		AddBodyKeyword("test", 1).
		AddHeaderKeyword("nginx", 2).
		Build()

	result := idx.MatchSources(nil, []byte("this is a test"))
	if !result[1] {
		t.Error("expected body source 1")
	}
	if result[2] {
		t.Error("unexpected header source 2")
	}
}

func TestDualKeywordIndex_Overlapping(t *testing.T) {
	idx := NewDualKeywordIndexBuilder().
		SetOverlapping(true).
		AddHeaderKeyword("server: microsoft", 1).
		AddHeaderKeyword("server: microsoft-iis", 2).
		Build()

	result := idx.MatchSources(
		[]byte("server: microsoft-iis/10.0"),
		nil,
	)
	if !result[1] || !result[2] {
		t.Errorf("expected both overlapping sources, got %v", result)
	}
}

func TestDualKeywordIndex_FallbackOnly(t *testing.T) {
	idx := NewDualKeywordIndexBuilder().
		AddFallback(1).
		AddFallback(2).
		Build()

	result := idx.MatchSources(nil, nil)
	if !result[1] || !result[2] {
		t.Errorf("expected fallbacks 1 and 2, got %v", result)
	}
}
