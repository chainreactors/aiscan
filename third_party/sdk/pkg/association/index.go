package association

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/chainreactors/fingers/alias"
	fingersEngine "github.com/chainreactors/fingers/fingers"
	"github.com/chainreactors/fingers/resources"
	"github.com/chainreactors/neutron/templates"
	"github.com/chainreactors/sdk/pkg/types"
)

type entityKind uint8

const (
	entityFinger entityKind = iota + 1
	entityAlias
	entityTemplate
)

type entityRef struct {
	kind entityKind
	id   int
}

type term struct {
	kind  string
	value string
}

// IndexOptions controls optional indexes.
type IndexOptions struct {
	MetadataKeys []string
}

// IndexOption mutates IndexOptions.
type IndexOption func(*IndexOptions)

// WithMetadataKeys indexes the selected Alias.Metadata and Template.Info.Metadata keys.
func WithMetadataKeys(keys ...string) IndexOption {
	return func(options *IndexOptions) {
		options.MetadataKeys = append(options.MetadataKeys, keys...)
	}
}

// Index is a compact association query engine.
// It stores entities in slices, terms in one inverted index, and core relations
// as integer adjacency lists.
type Index struct {
	options      IndexOptions
	metadataKeys map[string]struct{}

	fingers   fingersEngine.Fingers
	aliases   []*alias.Alias
	templates []*templates.Template

	fingerByName map[string]int
	aliasByName  map[string]int
	templateByID map[string]int
	aliasLookup  map[string][]int

	termIndex map[term][]entityRef

	fingerAliases   [][]int
	aliasFingers    [][]int
	fingerTemplates [][]int
	templateFingers [][]int
	aliasTemplates  [][]int
	templateAliases [][]int

	mu sync.RWMutex
}

// NewIndex creates an empty index.
func NewIndex(options ...IndexOption) *Index {
	opts := IndexOptions{}
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}
	return NewIndexWithOptions(opts)
}

// NewIndexWithOptions creates an empty index using explicit options.
func NewIndexWithOptions(options IndexOptions) *Index {
	idx := &Index{}
	idx.setOptions(options)
	idx.clear()
	return idx
}

// BuildFromProvider loads data from a Provider and builds an index.
func BuildFromProvider(ctx context.Context, p types.Provider) (*Index, error) {
	return BuildFromProviderWithOptions(ctx, p, IndexOptions{})
}

// BuildFromProviderWithOptions loads data from a Provider and builds an index.
func BuildFromProviderWithOptions(ctx context.Context, p types.Provider, options IndexOptions) (*Index, error) {
	if p == nil {
		return nil, fmt.Errorf("provider is nil")
	}

	fingers, aliases, err := p.Fingers(ctx)
	if err != nil {
		return nil, fmt.Errorf("load fingers: %w", err)
	}

	tpls, err := p.POCs(ctx)
	if err != nil {
		return nil, fmt.Errorf("load pocs: %w", err)
	}

	idx := NewIndexWithOptions(options)
	idx.BuildWithFingers(fingers, aliases, tpls)
	return idx, nil
}

// Build constructs the index from aliases and templates.
func (idx *Index) Build(aliases []*alias.Alias, tpls []*templates.Template) {
	idx.BuildWithFingers(nil, aliases, tpls)
}

// BuildWithFingers constructs the index from fingerprints, aliases, and templates.
func (idx *Index) BuildWithFingers(fingers fingersEngine.Fingers, aliases []*alias.Alias, tpls []*templates.Template) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.clear()

	for _, f := range fingers {
		idx.addFinger(f)
	}
	for _, a := range aliases {
		idx.addAlias(a)
	}
	for _, t := range tpls {
		idx.addTemplate(t)
	}

	for aliasID := range idx.aliases {
		idx.linkAliasFingers(aliasID)
	}
	for templateID := range idx.templates {
		idx.linkTemplateFingers(templateID)
	}
	for aliasID := range idx.aliases {
		idx.linkAliasPOCs(aliasID)
	}
}

func (idx *Index) setOptions(options IndexOptions) {
	idx.options = IndexOptions{
		MetadataKeys: append([]string(nil), options.MetadataKeys...),
	}
	idx.metadataKeys = make(map[string]struct{}, len(options.MetadataKeys))
	for _, key := range options.MetadataKeys {
		key = nameKey(key)
		if key != "" {
			idx.metadataKeys[key] = struct{}{}
		}
	}
}

func (idx *Index) clear() {
	idx.fingers = nil
	idx.aliases = nil
	idx.templates = nil

	idx.fingerByName = make(map[string]int)
	idx.aliasByName = make(map[string]int)
	idx.templateByID = make(map[string]int)
	idx.aliasLookup = make(map[string][]int)

	idx.termIndex = make(map[term][]entityRef)

	idx.fingerAliases = nil
	idx.aliasFingers = nil
	idx.fingerTemplates = nil
	idx.templateFingers = nil
	idx.aliasTemplates = nil
	idx.templateAliases = nil
}

func (idx *Index) addFinger(f *fingersEngine.Finger) {
	if f == nil || f.Name == "" {
		return
	}

	id := len(idx.fingers)
	idx.fingers = append(idx.fingers, f)
	idx.fingerAliases = append(idx.fingerAliases, nil)
	idx.fingerTemplates = append(idx.fingerTemplates, nil)

	ref := entityRef{kind: entityFinger, id: id}
	idx.addFingerLookup(f.Name, id)
	idx.addTerm("finger", f.Name, ref)
	idx.addTerm("name", f.Name, ref)
	idx.addTerm("protocol", f.Protocol, ref)
	idx.addTerm("vendor", f.Attributes.Vendor, ref)
	idx.addTerm("product", f.Attributes.Product, ref)
	for _, tag := range f.Tags {
		idx.addTerm("tag", tag, ref)
	}
	for _, port := range f.DefaultPort {
		idx.addTerm("port", port, ref)
	}
}

func (idx *Index) addAlias(a *alias.Alias) {
	if a == nil || a.Name == "" {
		return
	}
	if len(a.AllTags()) == 0 {
		a.Compile()
	}

	id := len(idx.aliases)
	idx.aliases = append(idx.aliases, a)
	idx.aliasFingers = append(idx.aliasFingers, nil)
	idx.aliasTemplates = append(idx.aliasTemplates, nil)

	ref := entityRef{kind: entityAlias, id: id}
	idx.aliasByName[nameKey(a.Name)] = id
	idx.addAliasLookup(a.Name, id)
	idx.addTerm("alias", a.Name, ref)
	idx.addTerm("finger", a.Name, ref)
	idx.addTerm("name", a.Name, ref)
	idx.addTerm("vendor", a.Vendor, ref)
	idx.addTerm("product", a.Product, ref)
	if a.Vendor != "" && a.Product != "" {
		idx.addTerm("cpe", a.Vendor+"/"+a.Product, ref)
	}
	for _, tag := range a.Tags {
		idx.addTerm("tag", tag, ref)
	}
	for _, names := range a.AliasMap {
		for _, name := range names {
			idx.addAliasLookup(name, id)
			idx.addTerm("alias", name, ref)
			idx.addTerm("finger", name, ref)
		}
	}

	if a.Metadata != nil {
		if service, ok := a.Metadata["service"].(string); ok {
			idx.addTerm("service", service, ref)
		}
		idx.addWhitelistedMetadataTerms(a.Metadata, ref)
	}
}

func (idx *Index) addTemplate(t *templates.Template) {
	if t == nil || t.Id == "" {
		return
	}

	id := len(idx.templates)
	idx.templates = append(idx.templates, t)
	idx.templateFingers = append(idx.templateFingers, nil)
	idx.templateAliases = append(idx.templateAliases, nil)

	ref := entityRef{kind: entityTemplate, id: id}
	idx.templateByID[templateKey(t.Id)] = id
	idx.addTerm("template", t.Id, ref)
	idx.addTerm("id", t.Id, ref)
	idx.addTerm("severity", t.Info.Severity, ref)
	for _, fingerName := range t.Fingers {
		idx.addTerm("finger", fingerName, ref)
	}
	for _, tag := range t.GetTags() {
		idx.addTerm("tag", strings.TrimSpace(tag), ref)
	}
	if t.Info.Classification != nil {
		idx.addTerm("cve", t.Info.Classification.CVEID, ref)
		idx.addTerm("cwe", t.Info.Classification.CWEID, ref)
		idx.addTerm("cpe", t.Info.Classification.CPE, ref)
	}
	if t.Info.Metadata != nil {
		idx.addWhitelistedMetadataTerms(t.Info.Metadata, ref)
	}
}

func (idx *Index) linkAliasFingers(aliasID int) {
	a := idx.aliases[aliasID]
	idx.linkAliasFingerName(aliasID, a.Name)
	for _, names := range a.AliasMap {
		for _, name := range names {
			idx.linkAliasFingerName(aliasID, name)
		}
	}
}

func (idx *Index) linkAliasFingerName(aliasID int, name string) {
	if fingerID, ok := idx.fingerByName[nameKey(name)]; ok {
		idx.linkFingerAlias(fingerID, aliasID)
	}
	normalized := nameKey(resources.NormalizeString(name))
	if normalized != "" {
		if fingerID, ok := idx.fingerByName[normalized]; ok {
			idx.linkFingerAlias(fingerID, aliasID)
		}
	}
}

func (idx *Index) linkTemplateFingers(templateID int) {
	t := idx.templates[templateID]
	for _, fingerName := range t.Fingers {
		if fingerID, ok := idx.fingerByName[nameKey(fingerName)]; ok {
			idx.linkFingerTemplate(fingerID, templateID)
		}
		for _, aliasID := range idx.lookupAliasIDs(fingerName) {
			idx.linkAliasTemplate(aliasID, templateID)
		}
	}
}

func (idx *Index) linkAliasPOCs(aliasID int) {
	a := idx.aliases[aliasID]
	for _, pocID := range a.Pocs {
		if templateID, ok := idx.templateByID[templateKey(pocID)]; ok {
			idx.linkAliasTemplate(aliasID, templateID)
		}
	}
}

func (idx *Index) linkFingerAlias(fingerID, aliasID int) {
	idx.fingerAliases[fingerID] = appendUniqueInt(idx.fingerAliases[fingerID], aliasID)
	idx.aliasFingers[aliasID] = appendUniqueInt(idx.aliasFingers[aliasID], fingerID)
}

func (idx *Index) linkFingerTemplate(fingerID, templateID int) {
	idx.fingerTemplates[fingerID] = appendUniqueInt(idx.fingerTemplates[fingerID], templateID)
	idx.templateFingers[templateID] = appendUniqueInt(idx.templateFingers[templateID], fingerID)
}

func (idx *Index) linkAliasTemplate(aliasID, templateID int) {
	idx.aliasTemplates[aliasID] = appendUniqueInt(idx.aliasTemplates[aliasID], templateID)
	idx.templateAliases[templateID] = appendUniqueInt(idx.templateAliases[templateID], aliasID)
	for _, fingerID := range idx.aliasFingers[aliasID] {
		idx.linkFingerTemplate(fingerID, templateID)
	}
}

func (idx *Index) addFingerLookup(name string, id int) {
	if key := nameKey(name); key != "" {
		idx.fingerByName[key] = id
	}
	if normalized := nameKey(resources.NormalizeString(name)); normalized != "" {
		idx.fingerByName[normalized] = id
	}
}

func (idx *Index) addAliasLookup(name string, id int) {
	if key := nameKey(name); key != "" {
		idx.aliasLookup[key] = appendUniqueInt(idx.aliasLookup[key], id)
	}
	if normalized := nameKey(resources.NormalizeString(name)); normalized != "" {
		idx.aliasLookup[normalized] = appendUniqueInt(idx.aliasLookup[normalized], id)
	}
}

func (idx *Index) lookupAliasIDs(name string) []int {
	key := nameKey(name)
	result := append([]int(nil), idx.aliasLookup[key]...)
	normalized := nameKey(resources.NormalizeString(name))
	if normalized != "" && normalized != key {
		for _, id := range idx.aliasLookup[normalized] {
			result = appendUniqueInt(result, id)
		}
	}
	return result
}

func (idx *Index) addTerm(kind, value string, ref entityRef) {
	t := newTerm(kind, value)
	if !t.valid() {
		return
	}
	idx.termIndex[t] = appendUniqueRef(idx.termIndex[t], ref)

	normalized := newTerm(kind, resources.NormalizeString(value))
	if normalized.valid() && normalized != t {
		idx.termIndex[normalized] = appendUniqueRef(idx.termIndex[normalized], ref)
	}
}

func (idx *Index) addWhitelistedMetadataTerms(metadata map[string]interface{}, ref entityRef) {
	for key, raw := range metadata {
		key = nameKey(key)
		if _, ok := idx.metadataKeys[key]; !ok {
			continue
		}
		for _, value := range metadataValues(raw) {
			idx.addTerm(key, value, ref)
		}
	}
}

func metadataValues(raw interface{}) []string {
	switch v := raw.(type) {
	case string:
		return []string{v}
	case []string:
		return v
	case []interface{}:
		values := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				values = append(values, s)
			}
		}
		return values
	default:
		return nil
	}
}

func newTerm(kind, value string) term {
	return term{kind: nameKey(kind), value: nameKey(value)}
}

func termVariants(t term) []term {
	if !t.valid() {
		return nil
	}
	normalized := newTerm(t.kind, resources.NormalizeString(t.value))
	if normalized.valid() && normalized != t {
		return []term{t, normalized}
	}
	return []term{t}
}

func (t term) valid() bool {
	return t.kind != "" && t.value != ""
}

func nameKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func templateKey(s string) string {
	return nameKey(s)
}

func appendUniqueInt(slice []int, value int) []int {
	for _, existing := range slice {
		if existing == value {
			return slice
		}
	}
	return append(slice, value)
}

func appendUniqueRef(slice []entityRef, ref entityRef) []entityRef {
	for _, existing := range slice {
		if existing == ref {
			return slice
		}
	}
	return append(slice, ref)
}
