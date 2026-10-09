// Secrets enter only through stdin and the child environment, never a file.
// Build the test binary first; run this file from the repository root.
import { createInterface } from 'node:readline'
import { spawn } from 'node:child_process'
import { mkdir, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'

if (process.stdin.isTTY) process.stdin.setRawMode(true)
const keep = setInterval(() => {}, 1000)
const input = createInterface({ input: process.stdin, terminal: false })
console.log('Waiting for credential JSON on stdin; terminal echo disabled.')
const credentials = await new Promise(done => input.once('line', line => done(JSON.parse(line))))
input.close()
clearInterval(keep)
const redacted = text => [credentials.llm, credentials.jev].reduce((s, key) => s.split(key).join('[REDACTED]'), text)
const base = 'https://api.deepseek.com/v1'
const models = await fetch(base + '/models', { headers: { Authorization: 'Bearer ' + credentials.llm }, signal: AbortSignal.timeout(30000) })
if (!models.ok) throw new Error('model list HTTP ' + models.status)
const available = (await models.json()).data.map(v => v.id)
const model = ['deepseek-flash', 'deepseek-v4-flash', 'deepseek-chat'].find(v => available.includes(v))
if (!model) throw new Error('no supported model')
const stamp = new Date().toISOString().replaceAll(/[-:.]/g, '')
const root = resolve('.runlogs/jev-playwright-20261004/live-' + stamp)
await mkdir(root, { recursive: true })
const probe = await fetch('https://api.typesafe.ai/v1/systemone', {
  method: 'POST', headers: { Authorization: 'Bearer ' + credentials.jev, 'Content-Type': 'application/json' },
  body: JSON.stringify({ model: 'jev-1.13.0', state: { operation: 'browser verification' }, questions: { ready: { type: 'choice', instructions: 'Select ready for a browser verification operation.', criteria: { ready: 'Browser verification', other: 'Other' } } } }),
  signal: AbortSignal.timeout(30000)
})
let detail
try { detail = await probe.json() } catch { detail = { error: 'non-JSON response' } }
await writeFile(resolve(root, 'service-probe.json'), JSON.stringify({ deepseek_models_status: models.status, model, jev_status: probe.status, detail }, null, 2))
console.log('model=' + model + ' JEV HTTP=' + probe.status + ' report=' + root)
if (!probe.ok) throw new Error('JEV service probe failed; no acceptance tasks started')
const env = { ...process.env, CYBER_API_KEY: credentials.llm, TYPESAFE_API_KEY: credentials.jev, CYBER_BASE_URL: base, CYBER_MODEL: model,
  JEV_TAKEOVER_LIVE: '1', JEV_TAKEOVER_REPORT_DIR: root, JEV_TAKEOVER_CASES: process.argv[2] || 'expense,shadow,repeat,popup,frame,files,drag',
  JEV_TAKEOVER_WARM: process.argv[3] || '1', CYBER_BROWSER_PATH: 'C:/Program Files/Google/Chrome/Application/chrome.exe' }
const child = spawn(resolve('.runlogs/jev-playwright-20261004/jev-tests.exe'), ['-test.run=^TestLivePlaywrightTakeoverMatrix$', '-test.v', '-test.count=1', '-test.parallel=3', '-test.timeout=90m'], {
  cwd: resolve('exts/jev'), env, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe']
})
let log = ''
for (const stream of [child.stdout, child.stderr]) {
  stream.setEncoding('utf8')
  stream.on('data', chunk => { const safe = redacted(chunk); log += safe; process.stdout.write(safe) })
}
const code = await new Promise((done, reject) => { child.once('error', reject); child.once('exit', done) })
await writeFile(resolve(root, 'test.log'), log)
console.log('acceptance exit=' + code + ' report=' + root)
process.exitCode = code
