package ahocorasick

import "sort"

// KeywordIndex maps keyword strings to integer source IDs via an AC automaton.
// Build one with KeywordIndexBuilder.
type KeywordIndex struct {
	ac               *Automaton
	keywordToSources map[int][]int // AC pattern ID → source IDs
	fallbackSources  []int
	overlapping      bool
}

// MatchSources scans content and returns all source IDs whose keywords appear,
// unioned with fallback sources.
func (idx *KeywordIndex) MatchSources(content []byte) map[int]bool {
	if idx == nil {
		return nil
	}
	result := make(map[int]bool)
	if idx.ac != nil && len(content) > 0 {
		var matches []Match
		if idx.overlapping {
			matches = idx.ac.FindAllOverlapping(content)
		} else {
			matches = idx.ac.FindAll(content, -1)
		}
		for _, m := range matches {
			for _, sid := range idx.keywordToSources[m.PatternID] {
				result[sid] = true
			}
		}
	}
	for _, sid := range idx.fallbackSources {
		result[sid] = true
	}
	return result
}

// KeywordIndexBuilder collects keywords and builds a KeywordIndex.
type KeywordIndexBuilder struct {
	keywords    []string
	lookup      map[string]int // keyword → index in keywords slice
	kwToSources map[int][]int  // keyword index → source IDs
	fallbacks   map[int]bool
	overlapping bool
}

// NewKeywordIndexBuilder creates a new builder.
func NewKeywordIndexBuilder() *KeywordIndexBuilder {
	return &KeywordIndexBuilder{
		lookup:      make(map[string]int),
		kwToSources: make(map[int][]int),
		fallbacks:   make(map[int]bool),
	}
}

// SetOverlapping enables FindAllOverlapping instead of FindAll.
func (b *KeywordIndexBuilder) SetOverlapping(v bool) *KeywordIndexBuilder {
	b.overlapping = v
	return b
}

// AddKeyword registers a keyword→sourceID mapping. Duplicate keywords share
// a single AC pattern; duplicate (keyword, sourceID) pairs are ignored.
func (b *KeywordIndexBuilder) AddKeyword(keyword string, sourceID int) *KeywordIndexBuilder {
	kwIdx, ok := b.lookup[keyword]
	if !ok {
		kwIdx = len(b.keywords)
		b.lookup[keyword] = kwIdx
		b.keywords = append(b.keywords, keyword)
	}
	existing := b.kwToSources[kwIdx]
	for _, sid := range existing {
		if sid == sourceID {
			return b
		}
	}
	b.kwToSources[kwIdx] = append(existing, sourceID)
	return b
}

// AddFallback registers a source ID that always appears in results.
func (b *KeywordIndexBuilder) AddFallback(sourceID int) *KeywordIndexBuilder {
	b.fallbacks[sourceID] = true
	return b
}

// Build constructs the KeywordIndex. The builder should not be reused.
func (b *KeywordIndexBuilder) Build() *KeywordIndex {
	idx := &KeywordIndex{
		keywordToSources: b.kwToSources,
		overlapping:      b.overlapping,
	}
	if len(b.keywords) > 0 {
		ac, err := NewBuilder().AddStrings(b.keywords).Build()
		if err == nil {
			idx.ac = ac
		}
	}
	for sid := range b.fallbacks {
		idx.fallbackSources = append(idx.fallbackSources, sid)
	}
	sort.Ints(idx.fallbackSources)
	return idx
}

// DualKeywordIndex holds separate body and header KeywordIndex instances
// plus a shared fallback set.
type DualKeywordIndex struct {
	Body      *KeywordIndex
	Header    *KeywordIndex
	fallbacks []int
}

// MatchSources scans header and body content, returning all matching source IDs
// unioned with fallback sources.
func (d *DualKeywordIndex) MatchSources(header, body []byte) map[int]bool {
	result := make(map[int]bool)
	if d.Header != nil && d.Header.ac != nil && len(header) > 0 {
		for sid := range d.Header.MatchSources(header) {
			result[sid] = true
		}
	}
	if d.Body != nil && d.Body.ac != nil && len(body) > 0 {
		for sid := range d.Body.MatchSources(body) {
			result[sid] = true
		}
	}
	for _, sid := range d.fallbacks {
		result[sid] = true
	}
	return result
}

// DualKeywordIndexBuilder builds a DualKeywordIndex.
type DualKeywordIndexBuilder struct {
	body        *KeywordIndexBuilder
	header      *KeywordIndexBuilder
	fallbacks   map[int]bool
	overlapping bool
}

// NewDualKeywordIndexBuilder creates a new builder.
func NewDualKeywordIndexBuilder() *DualKeywordIndexBuilder {
	return &DualKeywordIndexBuilder{
		body:      NewKeywordIndexBuilder(),
		header:    NewKeywordIndexBuilder(),
		fallbacks: make(map[int]bool),
	}
}

// SetOverlapping enables FindAllOverlapping on both body and header.
func (b *DualKeywordIndexBuilder) SetOverlapping(v bool) *DualKeywordIndexBuilder {
	b.overlapping = v
	b.body.SetOverlapping(v)
	b.header.SetOverlapping(v)
	return b
}

// AddBodyKeyword adds a keyword→sourceID mapping to the body index.
func (b *DualKeywordIndexBuilder) AddBodyKeyword(keyword string, sourceID int) *DualKeywordIndexBuilder {
	b.body.AddKeyword(keyword, sourceID)
	return b
}

// AddHeaderKeyword adds a keyword→sourceID mapping to the header index.
func (b *DualKeywordIndexBuilder) AddHeaderKeyword(keyword string, sourceID int) *DualKeywordIndexBuilder {
	b.header.AddKeyword(keyword, sourceID)
	return b
}

// AddFallback registers a source ID that always appears in results.
func (b *DualKeywordIndexBuilder) AddFallback(sourceID int) *DualKeywordIndexBuilder {
	b.fallbacks[sourceID] = true
	return b
}

// Build constructs the DualKeywordIndex. The builder should not be reused.
func (b *DualKeywordIndexBuilder) Build() *DualKeywordIndex {
	d := &DualKeywordIndex{
		Body:   b.body.Build(),
		Header: b.header.Build(),
	}
	for sid := range b.fallbacks {
		d.fallbacks = append(d.fallbacks, sid)
	}
	sort.Ints(d.fallbacks)
	return d
}
