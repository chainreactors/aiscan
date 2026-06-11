package neutron

import "github.com/chainreactors/sdk/pkg/types"

// Templates wraps neutron templates with helper APIs.
type Templates struct {
	Items map[string]*types.Template
}

// Templates returns template list.
func (t Templates) Templates() []*types.Template {
	if len(t.Items) == 0 {
		return nil
	}
	out := make([]*types.Template, 0, len(t.Items))
	for _, item := range t.Items {
		out = append(out, item)
	}
	return out
}

// Len returns item count.
func (t Templates) Len() int {
	return len(t.Items)
}

// Append adds a single template.
func (t Templates) Append(item *types.Template) Templates {
	if item == nil {
		return t
	}
	if t.Items == nil {
		t.Items = make(map[string]*types.Template)
	}
	key := templateKey(item)
	if key != "" {
		t.Items[key] = item
	}
	return t
}

// Merge appends templates into Templates.
func (t Templates) Merge(other []*types.Template) Templates {
	if len(other) == 0 {
		return t
	}
	if t.Items == nil {
		t.Items = make(map[string]*types.Template)
	}
	for _, item := range other {
		t = t.Append(item)
	}
	return t
}

// Filter returns a filtered copy of Templates using predicate.
func (t Templates) Filter(predicate func(*types.Template) bool) Templates {
	if predicate == nil || len(t.Items) == 0 {
		return t
	}
	filtered := Templates{
		Items: make(map[string]*types.Template),
	}
	for key, item := range t.Items {
		if predicate(item) {
			filtered.Items[key] = item
		}
	}
	return filtered
}

func templateKey(item *types.Template) string {
	if item == nil {
		return ""
	}
	if item.Info.Name != "" {
		return item.Info.Name
	}
	return item.Id
}
