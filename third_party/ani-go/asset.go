package ani

// CompanyAsset 是 ani-go 的一条命中: 一个公司 + 它的 ICP / 域名 / 投资关系。
// 一家公司可能有 0..N 条 ICP, 序列化时为每条 ICP 输出一个 CompanyAsset
// (扁平化), 与 Ina 的 Asset 单条粒度一致, 便于下游统一消费。
type CompanyAsset struct {
	Name     string  `json:"name"`     // 公司名
	PID      string  `json:"pid"`      // 数据源内部 ID (aqc sourceId / tyc id ...)
	ICP      string  `json:"icp"`      // ICP 主号, 如 "京ICP备12345号"
	Domain   string  `json:"domain"`   // ICP 对应的根域名
	Title    string  `json:"title"`    // 备案的站点名
	Parent   string  `json:"parent"`   // 投资父公司名 (根公司为 "")
	Percent  float64 `json:"percent"`  // 占股比例 (0.0 - 1.0)
	Depth    int     `json:"depth"`    // 在投资树中的层数, 根公司为 0
	Source   string  `json:"source"`   // "aqc_unauth" / "tyc_unauth" / ...
}
