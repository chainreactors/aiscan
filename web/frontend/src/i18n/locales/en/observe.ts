export default {
  title: 'Observability', description: 'Session activity and assets from all sessions', open: 'View observations ({{count}})',
  all: 'All', search: 'Search observations', empty: 'No observations in this session',
  emptyHint: 'Tool results and captured data will appear here.', filteredEmpty: 'No matching observations', clearFilters: 'Clear filters', related: 'Related observations',
  session: 'Session activity', assets: 'Asset library', activity: 'Activity', events: 'Raw events', metadata: 'Metadata', back: 'Back to observations', copy: 'Copy event ID', copied: 'Copied', newItems: '{{count}} new items', jumpToLatest: 'Jump to latest',
  categories: { tool: 'Tools', traffic: 'Traffic', file: 'Files', record: 'Recordings', cstx: 'CSTX', command: 'Commands', process: 'Processes', other: 'Other' },
  started: 'Started', completed: 'Completed', failed: 'Failed', allowed: 'Allowed', denied: 'Denied', canceled: 'Canceled',
  file: { access: 'File access', read: 'Read', write: 'Write', edit: 'Edit', create: 'Create', delete: 'Delete', size: 'Size', transferred: 'Transferred', edits: 'Edits', directory: 'Working directory', tool: 'Tool', snapshot: 'Shell snapshot', control: 'Control plane', unknown: 'Unknown source' },
  cstx: { loading: 'Loading assets…', failed: 'Failed to parse assets', raw: 'Raw data', hosts: 'Hosts', noHosts: 'No host assets', ips: 'IPs', ports: 'Ports', apps: 'Apps', urls: 'URLs', frameworks: 'Frameworks', vulns: 'Vulnerabilities' },
}
