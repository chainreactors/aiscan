package ina

// Asset 是一条 recon 命中。字段命名对齐 Python InaData.d, 便于上游迁移。
type Asset struct {
	IP     string `json:"ip"`
	Port   string `json:"port"`
	URL    string `json:"url"`
	Domain string `json:"domain"`
	Title  string `json:"title"`
	ICP    string `json:"icp"`

	// Optional Python InaData.d fields used by specific sources.
	ICO     string `json:"ico,omitempty"`
	Status  string `json:"status,omitempty"`
	Company string `json:"company,omitempty"`
	Frame   string `json:"frame,omitempty"`

	// Source is kept for Go callers and diagnostics. Python InaData.d output
	// does not include it, so it is intentionally omitted from JSON.
	Source string `json:"-"`
}
