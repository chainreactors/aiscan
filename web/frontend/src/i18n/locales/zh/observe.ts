export default {
  title: '可观测', description: '当前会话活动与跨会话资产库', open: '查看可观测记录（{{count}}）',
  all: '全部', search: '搜索观察记录', empty: '当前会话尚无观察记录',
  emptyHint: '工具调用结果与捕获的数据会显示在这里。', filteredEmpty: '没有匹配的记录', clearFilters: '清除筛选', related: '关联记录',
  session: '会话活动', assets: '资产库', activity: '活动', events: '原始事件', metadata: '元信息', back: '返回记录列表', copy: '复制事件 ID', copied: '已复制', newItems: '新增 {{count}} 条', jumpToLatest: '跳到最新',
  categories: { tool: '工具', traffic: '流量', file: '文件', record: '录制', cstx: 'CSTX', command: '命令', process: '进程', other: '其他' },
  started: '已开始', completed: '已完成', failed: '失败', allowed: '已允许', denied: '已拒绝', canceled: '已取消',
  file: { access: '文件访问', read: '读取', write: '写入', edit: '编辑', create: '创建', delete: '删除', size: '大小', transferred: '传输', edits: '修改次数', directory: '工作目录', tool: '工具', snapshot: 'Shell 快照', control: '控制端', unknown: '未知来源' },
  cstx: { loading: '正在加载资产…', failed: '资产解析失败', raw: '原始数据', hosts: '主机', noHosts: '暂无主机资产', ips: 'IP', ports: '端口', apps: '应用', urls: 'URL', frameworks: '指纹', vulns: '漏洞' },
}
