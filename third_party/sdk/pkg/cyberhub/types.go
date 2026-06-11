package cyberhub

import "github.com/chainreactors/sdk/pkg/types"

type ExportFilter = types.ExportFilter

// NewExportFilter 创建空的筛选器
func NewExportFilter() *ExportFilter {
	return types.NewExportFilter()
}
