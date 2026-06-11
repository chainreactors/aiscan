package association

import (
	"github.com/chainreactors/fingers/alias"
	"github.com/chainreactors/fingers/common"
	fingersEngine "github.com/chainreactors/fingers/fingers"
	"github.com/chainreactors/neutron/templates"
	"github.com/chainreactors/parsers"
	"github.com/chainreactors/sdk/pkg/types"
)

// Query is the unified association query input.
type Query struct {
	Fingers    []string
	Aliases    []string
	Templates  []string
	Tags       []string
	Services   []string
	CPEs       []string
	CVEs       []string
	Attributes map[string][]string
}

func NewQuery() *Query {
	return &Query{}
}

func (q *Query) WithFingers(names ...string) *Query {
	q.Fingers = append(q.Fingers, names...)
	return q
}

func (q *Query) WithAliases(names ...string) *Query {
	q.Aliases = append(q.Aliases, names...)
	return q
}

func (q *Query) WithTemplates(ids ...string) *Query {
	q.Templates = append(q.Templates, ids...)
	return q
}

func (q *Query) WithTags(tags ...string) *Query {
	q.Tags = append(q.Tags, tags...)
	return q
}

func (q *Query) WithServices(services ...string) *Query {
	q.Services = append(q.Services, services...)
	return q
}

func (q *Query) WithCPEs(cpes ...string) *Query {
	q.CPEs = append(q.CPEs, cpes...)
	return q
}

func (q *Query) WithCVEs(cves ...string) *Query {
	q.CVEs = append(q.CVEs, cves...)
	return q
}

func (q *Query) WithAttr(kind string, values ...string) *Query {
	if q.Attributes == nil {
		q.Attributes = make(map[string][]string)
	}
	q.Attributes[kind] = append(q.Attributes[kind], values...)
	return q
}

func (q *Query) WithFrameworks(fws common.Frameworks) *Query {
	for _, fw := range fws {
		if fw != nil && fw.Name != "" {
			q.Fingers = append(q.Fingers, fw.Name)
		}
	}
	return q
}

func (q *Query) WithVulns(vulns common.Vulns) *Query {
	for name, v := range vulns {
		q.CVEs = append(q.CVEs, name)
		if v != nil && v.Framework != nil {
			q.Fingers = append(q.Fingers, v.Framework.Name)
		}
	}
	return q
}

func (q *Query) Merge(other *Query) *Query {
	if other == nil {
		return q
	}
	q.Fingers = append(q.Fingers, other.Fingers...)
	q.Aliases = append(q.Aliases, other.Aliases...)
	q.Templates = append(q.Templates, other.Templates...)
	q.Tags = append(q.Tags, other.Tags...)
	q.Services = append(q.Services, other.Services...)
	q.CPEs = append(q.CPEs, other.CPEs...)
	q.CVEs = append(q.CVEs, other.CVEs...)
	if len(other.Attributes) > 0 {
		if q.Attributes == nil {
			q.Attributes = make(map[string][]string)
		}
		for key, values := range other.Attributes {
			q.Attributes[key] = append(q.Attributes[key], values...)
		}
	}
	return q
}

func (q *Query) terms() []term {
	if q == nil {
		return nil
	}
	var terms []term
	appendTerms := func(kind string, values []string) {
		for _, value := range values {
			t := newTerm(kind, value)
			if t.valid() {
				terms = append(terms, t)
			}
		}
	}
	appendTerms("finger", q.Fingers)
	appendTerms("alias", q.Aliases)
	appendTerms("template", q.Templates)
	appendTerms("tag", q.Tags)
	appendTerms("service", q.Services)
	appendTerms("cpe", q.CPEs)
	appendTerms("cve", q.CVEs)
	for kind, values := range q.Attributes {
		appendTerms(kind, values)
	}
	return terms
}

// QueryFromResult extracts association query terms from an SDK result.
func QueryFromResult(r types.Result) *Query {
	q := NewQuery()
	if r == nil || !r.Success() {
		return q
	}
	switch data := r.Data().(type) {
	case *parsers.GOGOResult:
		q.WithFrameworks(data.Frameworks).WithVulns(data.Vulns)
	case *parsers.SprayResult:
		q.WithFrameworks(data.Frameworks)
	case *parsers.ZombieResult:
		if data.Service != "" {
			q.WithServices(data.Service)
		}
	}
	return q
}

// QueryResult contains the associated raw entities.
type QueryResult struct {
	Fingers   fingersEngine.Fingers
	Aliases   []*alias.Alias
	Templates []*templates.Template
}

// Lookup finds seed entities from Query terms, then adds directly associated entities.
func (idx *Index) Lookup(q *Query) *QueryResult {
	if q == nil {
		return &QueryResult{}
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	seeds := make([]entityRef, 0)
	seenSeed := make(map[entityRef]struct{})
	for _, t := range q.terms() {
		for _, variant := range termVariants(t) {
			for _, ref := range idx.termIndex[variant] {
				if _, ok := seenSeed[ref]; ok {
					continue
				}
				seenSeed[ref] = struct{}{}
				seeds = append(seeds, ref)
			}
		}
	}

	collector := newResultCollector(idx)
	for _, ref := range seeds {
		collector.addRef(ref)
	}
	for _, ref := range seeds {
		collector.addAssociated(ref)
	}
	return collector.result()
}

// Alias returns an alias by canonical name.
func (idx *Index) Alias(name string) *alias.Alias {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	if id, ok := idx.aliasByName[nameKey(name)]; ok {
		return idx.aliases[id]
	}
	return nil
}

// Finger returns a fingerprint by name.
func (idx *Index) Finger(name string) *fingersEngine.Finger {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	if id, ok := idx.fingerByName[nameKey(name)]; ok {
		return idx.fingers[id]
	}
	return nil
}

// Template returns a template by id.
func (idx *Index) Template(id string) *templates.Template {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	if id, ok := idx.templateByID[templateKey(id)]; ok {
		return idx.templates[id]
	}
	return nil
}

func (idx *Index) Aliases(names ...string) []*alias.Alias {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var result []*alias.Alias
	seen := make(map[int]struct{})
	for _, name := range names {
		id, ok := idx.aliasByName[nameKey(name)]
		if !ok {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, idx.aliases[id])
	}
	return result
}

func (idx *Index) Fingers(names ...string) fingersEngine.Fingers {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var result fingersEngine.Fingers
	seen := make(map[int]struct{})
	for _, name := range names {
		id, ok := idx.fingerByName[nameKey(name)]
		if !ok {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, idx.fingers[id])
	}
	return result
}

func (idx *Index) Templates(ids ...string) []*templates.Template {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var result []*templates.Template
	seen := make(map[int]struct{})
	for _, templateID := range ids {
		id, ok := idx.templateByID[templateKey(templateID)]
		if !ok {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, idx.templates[id])
	}
	return result
}

type resultCollector struct {
	idx *Index

	fingerIDs   []int
	aliasIDs    []int
	templateIDs []int

	seenFingers   map[int]struct{}
	seenAliases   map[int]struct{}
	seenTemplates map[int]struct{}
}

func newResultCollector(idx *Index) *resultCollector {
	return &resultCollector{
		idx:           idx,
		seenFingers:   make(map[int]struct{}),
		seenAliases:   make(map[int]struct{}),
		seenTemplates: make(map[int]struct{}),
	}
}

func (c *resultCollector) addRef(ref entityRef) {
	switch ref.kind {
	case entityFinger:
		c.addFinger(ref.id)
	case entityAlias:
		c.addAlias(ref.id)
	case entityTemplate:
		c.addTemplate(ref.id)
	}
}

func (c *resultCollector) addAssociated(ref entityRef) {
	switch ref.kind {
	case entityFinger:
		for _, aliasID := range c.idx.fingerAliases[ref.id] {
			c.addAlias(aliasID)
		}
		for _, templateID := range c.idx.fingerTemplates[ref.id] {
			c.addTemplate(templateID)
		}
	case entityAlias:
		for _, fingerID := range c.idx.aliasFingers[ref.id] {
			c.addFinger(fingerID)
		}
		for _, templateID := range c.idx.aliasTemplates[ref.id] {
			c.addTemplate(templateID)
		}
	case entityTemplate:
		for _, fingerID := range c.idx.templateFingers[ref.id] {
			c.addFinger(fingerID)
		}
		for _, aliasID := range c.idx.templateAliases[ref.id] {
			c.addAlias(aliasID)
		}
	}
}

func (c *resultCollector) addFinger(id int) {
	if id < 0 || id >= len(c.idx.fingers) {
		return
	}
	if _, ok := c.seenFingers[id]; ok {
		return
	}
	c.seenFingers[id] = struct{}{}
	c.fingerIDs = append(c.fingerIDs, id)
}

func (c *resultCollector) addAlias(id int) {
	if id < 0 || id >= len(c.idx.aliases) {
		return
	}
	if _, ok := c.seenAliases[id]; ok {
		return
	}
	c.seenAliases[id] = struct{}{}
	c.aliasIDs = append(c.aliasIDs, id)
}

func (c *resultCollector) addTemplate(id int) {
	if id < 0 || id >= len(c.idx.templates) {
		return
	}
	if _, ok := c.seenTemplates[id]; ok {
		return
	}
	c.seenTemplates[id] = struct{}{}
	c.templateIDs = append(c.templateIDs, id)
}

func (c *resultCollector) result() *QueryResult {
	result := &QueryResult{
		Fingers:   make(fingersEngine.Fingers, 0, len(c.fingerIDs)),
		Aliases:   make([]*alias.Alias, 0, len(c.aliasIDs)),
		Templates: make([]*templates.Template, 0, len(c.templateIDs)),
	}
	for _, id := range c.fingerIDs {
		result.Fingers = append(result.Fingers, c.idx.fingers[id])
	}
	for _, id := range c.aliasIDs {
		result.Aliases = append(result.Aliases, c.idx.aliases[id])
	}
	for _, id := range c.templateIDs {
		result.Templates = append(result.Templates, c.idx.templates[id])
	}
	return result
}
