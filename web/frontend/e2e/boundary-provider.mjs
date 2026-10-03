import { createServer } from 'node:http'

// Faults apply only to the latest operator input. Previous failures must not
// poison a conversation's later recovery requests.
export function boundaryProvider() {
  const calls = []
  const pending = new Map()
  const attempts = new Map()
  const json = (res, status, value) => {
    res.writeHead(status, { 'Content-Type': 'application/json' })
    res.end(JSON.stringify(value))
  }
  const server = createServer(async (req, res) => {
    try {
      const url = new URL(req.url, 'http://localhost')
      if (url.pathname === '/control/state') return json(res, 200, { calls, pending: [...pending.keys()] })
      if (url.pathname === '/control/release' && req.method === 'POST') {
        pending.get(url.searchParams.get('id'))?.()
        return json(res, 200, { ok: true })
      }
      if (url.pathname === '/v1/models') return json(res, 200, { data: [{ id: 'boundary-model-a' }, { id: 'boundary-model-b' }, { id: 'quota-model' }] })
      if (url.pathname !== '/v1/chat/completions') return json(res, 404, { error: 'not found' })
      const chunks = []
      for await (const chunk of req) chunks.push(chunk)
      const payload = JSON.parse(Buffer.concat(chunks).toString('utf8'))
      const messages = payload.messages || []
      const latest = [...messages].reverse().find(message => message.role === 'user')
      const input = typeof latest?.content === 'string' ? latest.content : JSON.stringify(latest?.content || '')
      const match = input.match(/BOUNDARY-E2E ([\w-]+) ([\w-]+)/)
      const [id, mode] = match ? [match[1], match[2]] : ['probe', 'ok']
      const attempt = (attempts.get(id) || 0) + 1
      if (payload.stream) attempts.set(id, attempt)
      const call = { id, mode, attempt, stream: !!payload.stream, model: payload.model, input, messages, closed: false, completed: false }
      calls.push(call)
      res.once('close', () => { call.closed = true; pending.get(id)?.(); pending.delete(id) })
      const fail = (status, message) => json(res, status, { error: { message } })
      if (payload.model === 'quota-model') return fail(402, 'boundary insufficient quota')
      if (!payload.stream) return json(res, 200, { choices: [{ message: { role: 'assistant', content: 'PONG' }, finish_reason: 'stop' }] })
      if (mode === 'quota') return fail(402, 'boundary insufficient quota')
      if (mode === 'unauthorized') return fail(401, 'boundary invalid API key')
      if (mode === 'bad-request') return fail(400, 'boundary invalid request')
      if (mode === 'retry' && attempt === 1) return fail(503, 'boundary service unavailable')
      if (mode === 'retry-hold') return fail(503, 'boundary service unavailable')
      if (mode === 'rate-limit' && attempt === 1) {
        res.setHeader('Retry-After', '1')
        return fail(429, 'boundary rate limit')
      }
      res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' })
      const frame = delta => res.write(`data: ${JSON.stringify({ choices: [{ index: 0, delta }] })}\n\n`)
      const end = reason => {
        if (res.destroyed) return
        call.completed = true
        res.end(`data: ${JSON.stringify({ choices: [{ index: 0, delta: {}, finish_reason: reason }], usage: { prompt_tokens: 20, completion_tokens: 5, total_tokens: 25 } })}\n\ndata: [DONE]\n\n`)
      }
      const userIndex = messages.findLastIndex(message => message.role === 'user')
      const toolsDone = messages.slice(userIndex + 1).some(message => message.role === 'tool')
      if (mode === 'tool-hold' && !toolsDone) {
        frame({ content: `INTERMEDIATE ${id}`, tool_calls: [{ index: 0, id: `read-${id}`, type: 'function', function: {
          name: 'read', arguments: JSON.stringify({ path: 'cyber://skills/cyber/okf/easm/gogo.md' }),
        } }] })
        return end('tool_calls')
      }
      if (mode === 'stream-break' && attempt === 1) {
        frame({ content: `STALE ${id}` })
        await new Promise(resolve => setTimeout(resolve, 100))
        return res.destroy()
      }
      if (mode === 'hold' || mode === 'tool-hold') {
        if (mode === 'hold') frame({ content: `ACTIVE ${id}` })
        await new Promise(resolve => pending.set(id, resolve))
        pending.delete(id)
        if (res.destroyed) return
      }
      const text = mode === 'markup'
        ? `SAFE ${id}\n<script>window.boundaryXSS = true</script>\n<img src=x onerror="window.boundaryXSS = true">\n[link](javascript:alert(1))\n中文 🧪 café\n` + 'long text '.repeat(1200)
        : `BOUNDARY-OK ${id} model:${payload.model}`
      if (mode === 'split-unicode') {
        const data = Buffer.from(`data: ${JSON.stringify({ choices: [{ index: 0, delta: { content: `BOUNDARY-OK ${id} 中文 🧪 café` } }] })}\n\n`)
        const start = data.indexOf(Buffer.from('🧪'))
        res.write(data.subarray(0, start + 1))
        res.write(data.subarray(start + 1, start + 2))
        res.write(data.subarray(start + 2))
      } else frame({ content: text })
      end('stop')
    } catch (error) {
      if (!res.headersSent) json(res, 500, { error: { message: String(error) } })
      else res.destroy()
    }
  })
  return { server, release: () => { for (const release of pending.values()) release(); pending.clear() } }
}
