package types

import "time"

// ExportFilter is the SDK-wide filter used when exporting remote CyberHub data.
type ExportFilter struct {
	Names        []string
	Tags         []string
	Sources      []string
	Severities   []string
	Engines      []string // 按引擎类型过滤: "fingers", "fingerprinthub", "xray"
	POCType      string
	Statuses     []string
	ReviewStatus string
	Draft        bool

	CreatedAfter  *time.Time
	CreatedBefore *time.Time
	UpdatedAfter  *time.Time
	UpdatedBefore *time.Time

	Limit int
}

func NewExportFilter() *ExportFilter {
	return &ExportFilter{}
}

func (f *ExportFilter) WithTags(tags ...string) *ExportFilter {
	f.Tags = tags
	return f
}

func (f *ExportFilter) WithSources(sources ...string) *ExportFilter {
	f.Sources = sources
	return f
}

func (f *ExportFilter) WithCreatedAfter(t time.Time) *ExportFilter {
	f.CreatedAfter = &t
	return f
}

func (f *ExportFilter) WithCreatedBefore(t time.Time) *ExportFilter {
	f.CreatedBefore = &t
	return f
}

func (f *ExportFilter) WithUpdatedAfter(t time.Time) *ExportFilter {
	f.UpdatedAfter = &t
	return f
}

func (f *ExportFilter) WithUpdatedBefore(t time.Time) *ExportFilter {
	f.UpdatedBefore = &t
	return f
}

func (f *ExportFilter) WithLimit(limit int) *ExportFilter {
	f.Limit = limit
	return f
}

func (f *ExportFilter) WithNames(names ...string) *ExportFilter {
	f.Names = names
	return f
}

func (f *ExportFilter) WithSeverities(severities ...string) *ExportFilter {
	f.Severities = severities
	return f
}

func (f *ExportFilter) WithPOCType(pocType string) *ExportFilter {
	f.POCType = pocType
	return f
}

func (f *ExportFilter) WithStatuses(statuses ...string) *ExportFilter {
	f.Statuses = statuses
	return f
}

func (f *ExportFilter) WithReviewStatus(status string) *ExportFilter {
	f.ReviewStatus = status
	return f
}

func (f *ExportFilter) WithDraft(draft bool) *ExportFilter {
	f.Draft = draft
	return f
}

func (f *ExportFilter) WithEngines(engines ...string) *ExportFilter {
	f.Engines = engines
	return f
}
