import ReactMarkdown from 'react-markdown'

interface ReportViewProps {
  report: string
}

export default function ReportView({ report }: ReportViewProps) {
  if (!report) {
    return (
      <div className="text-muted-foreground text-center py-12 text-sm">
        No report available yet.
      </div>
    )
  }

  return (
    <div className="rounded-lg border border-border bg-card/50 p-6 overflow-auto">
      <div className="prose prose-invert prose-sm max-w-none
        prose-headings:text-cyber-400 prose-headings:font-semibold
        prose-h1:text-lg prose-h1:border-b prose-h1:border-border prose-h1:pb-2 prose-h1:border-l-2 prose-h1:border-l-cyber-500 prose-h1:pl-3
        prose-h2:text-base prose-h2:mt-6 prose-h2:border-l-2 prose-h2:border-l-cyber-500/50 prose-h2:pl-3
        prose-h3:text-sm
        prose-p:text-gray-300 prose-p:leading-relaxed
        prose-strong:text-gray-100
        prose-code:text-cyber-300 prose-code:bg-secondary prose-code:px-1.5 prose-code:py-0.5 prose-code:rounded prose-code:text-xs
        prose-pre:bg-secondary prose-pre:border prose-pre:border-border prose-pre:rounded-lg
        prose-table:text-xs
        prose-th:text-gray-300 prose-th:border-border prose-th:px-3 prose-th:py-2 prose-th:bg-secondary/50
        prose-td:text-gray-400 prose-td:border-border prose-td:px-3 prose-td:py-1.5
        prose-li:text-gray-300
        prose-a:text-cyber-400 prose-a:no-underline hover:prose-a:underline
        prose-del:text-red-400 prose-del:opacity-60
      ">
        <ReactMarkdown>{report}</ReactMarkdown>
      </div>
    </div>
  )
}
