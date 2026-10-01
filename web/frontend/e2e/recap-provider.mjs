// Local deterministic LLM for exercising the real Web -> agent -> recap path.
// Run with: node e2e/recap-provider.mjs (port 38181 by default).
import { createServer } from 'node:http'

const requests = []
const pending = new Map()
const json = (res, status, value) => {
  res.writeHead(status, { 'Content-Type': 'application/json' })
  res.end(JSON.stringify(value))
}
const wait = ms => new Promise(resolve => setTimeout(resolve, ms))

createServer(async (req, res) => {
  try {
    const url = new URL(req.url, 'http://localhost')
    if (url.pathname === '/test/requests') return json(res, 200, requests)
    if (url.pathname === '/test/release' && req.method === 'POST') {
      pending.get(url.searchParams.get('case'))?.()
      return json(res, 200, { ok: true })
    }
    if (url.pathname === '/v1/models') return json(res, 200, { data: [{ id: 'gpt-4.1' }, { id: 'gpt-4.1-nano' }] })
    if (url.pathname !== '/v1/chat/completions') return json(res, 404, { error: 'not found' })
    const chunks = []
    for await (const chunk of req) chunks.push(chunk)
    const payload = JSON.parse(Buffer.concat(chunks).toString('utf8'))
    const messages = payload.messages || []
    const isRecap = messages[0]?.content?.startsWith('Summarize the work record below') === true
    const latestUser = [...messages].reverse().find(message => message.role === 'user')?.content || ''
    const input = typeof latestUser === 'string' ? latestUser : JSON.stringify(latestUser)
    const scenario = input.match(/RECAP-E2E ([a-z-]+)/)?.[1] || 'basic'
    const record = { kind: isRecap ? 'recap' : 'task', scenario, model: payload.model,
      tools: payload.tools?.length || 0, stream: !!payload.stream, maxTokens: payload.max_tokens,
      text: messages.map(message => typeof message.content === 'string' ? message.content : JSON.stringify(message.content)).join('\n'),
      at: Date.now() }
    requests.push(record)
    if (!isRecap && input.includes('Reply with exactly one word: PONG')) {
      if (payload.stream) {
        res.writeHead(200, { 'Content-Type': 'text/event-stream' })
        res.write('data: {"choices":[{"index":0,"delta":{"content":"PONG"}}]}\n\n')
        res.end('data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}\n\ndata: [DONE]\n\n')
      } else json(res, 200, { choices: [{ message: { role: 'assistant', content: 'PONG' }, finish_reason: 'stop' }] })
      return
    }
    if (payload.tools?.some(tool => tool.function?.name === 'verdict')) {
      const secondRound = JSON.stringify(messages).includes('Task goal-second complete.')
      return json(res, 200, { choices: [{ message: { role: 'assistant', tool_calls: [{ id: 'recap-verdict', type: 'function', function: {
        name: 'verdict', arguments: JSON.stringify({ pass: secondRound, continue: !secondRound, inherit_context: true,
          reason: secondRound ? 'Both checks completed' : 'Check a second round', feedback: 'RECAP-E2E goal-second run the next local check' }),
      } }] }, finish_reason: 'tool_calls' }] })
    }
    if (isRecap) {
      if (scenario === 'fallback' && payload.model === 'gpt-4.1-nano') {
        return json(res, 404, { error: { code: 'model_not_found', message: 'small model unavailable in this test' } })
      }
      if (scenario === 'failure') return json(res, 500, { error: { message: 'recap service unavailable in this test' } })
      if (scenario === 'late') await new Promise(resolve => pending.set(scenario, resolve))
      const text = scenario === 'cancel' ? '已读取本地技能，任务随后被取消。' : `已完成 ${scenario} 的本地检查并验证结果。`
      return json(res, 200, { choices: [{ message: { role: 'assistant', content: text }, finish_reason: 'stop' }] })
    }
    const userIndex = messages.findLastIndex(message => message.role === 'user')
    const toolDone = messages.slice(userIndex + 1).some(message => message.role === 'tool')
    const tool = { id: `recap-read-${requests.length}`, type: 'function', function: {
      name: 'read', arguments: JSON.stringify({ path: 'cyber://skills/cyber/okf/easm/gogo.md' }),
    } }
    const content = toolDone ? `Task ${scenario} complete.` : scenario === 'long'
      ? 'LONG_RECORD_START ' + 'observed work '.repeat(5000) + ' LONG_RECORD_END'
      : `Checking ${scenario} with a local tool.`
    const message = { role: 'assistant', content, reasoning_content: `PRIVATE_RECAP_THINKING_${scenario}`,
      ...(!toolDone ? { tool_calls: [tool] } : {}) }
    if (payload.stream) {
      res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' })
      const frame = delta => res.write(`data: ${JSON.stringify({ choices: [{ index: 0, delta }] })}\n\n`)
      frame({ role: 'assistant', reasoning_content: message.reasoning_content })
      if (toolDone && scenario === 'cancel') {
        frame({ content: 'Waiting for the cancellation check.' })
        await wait(8000)
      } else {
        await wait(250)
        frame({ content, ...(!toolDone ? { tool_calls: [{ index: 0, ...tool }] } : {}) })
      }
      res.write(`data: ${JSON.stringify({ choices: [{ index: 0, delta: {}, finish_reason: toolDone ? 'stop' : 'tool_calls' }],
        usage: { prompt_tokens: 100, completion_tokens: 20, total_tokens: 120 } })}\n\n`)
      res.end('data: [DONE]\n\n')
    } else {
      json(res, 200, { choices: [{ message, finish_reason: toolDone ? 'stop' : 'tool_calls' }],
        usage: { prompt_tokens: 100, completion_tokens: 20, total_tokens: 120 } })
    }
  } catch (error) {
    if (!res.headersSent) json(res, 500, { error: { message: String(error) } })
    else res.end()
  }
}).listen(Number(process.env.RECAP_TEST_PROVIDER_PORT || 38181), '127.0.0.1', () => {
  console.log('Recap test provider ready')
})
