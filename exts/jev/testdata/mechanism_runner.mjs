// Run from the aiscan root after building .runlogs/jev-mechanism/experiments.exe.
// Usage: node exts/jev/testdata/mechanism_runner.mjs local|all
// `all` accepts {jev,llm} on stdin; credentials never enter files or argv.
import { createInterface } from 'node:readline'
import { spawn } from 'node:child_process'
import { mkdir, writeFile, readFile } from 'node:fs/promises'
import { resolve } from 'node:path'

if (!['local', 'all'].includes(process.argv[2])) throw new Error('Usage: node mechanism_runner.mjs local|all [test-regex]')
const live = process.argv[2] === 'all'
let credentials = {}
let model = ''
if (live) {
  if (process.stdin.isTTY) process.stdin.setRawMode(true)
  const input = createInterface({ input: process.stdin, terminal: false })
  const keep = setInterval(() => {}, 1000)
  console.log('Waiting for credential JSON on stdin; echo disabled.')
  credentials = await new Promise(done => input.once('line', line => done(JSON.parse(line))))
  input.close(); clearInterval(keep)
  if (!credentials.jev || !credentials.llm) throw new Error('jev and llm required')
  const response = await fetch('https://api.deepseek.com/v1/models', { headers: { Authorization: 'Bearer ' + credentials.llm }, signal: AbortSignal.timeout(30000) })
  if (!response.ok) throw new Error('model probe HTTP ' + response.status)
  const available = (await response.json()).data.map(v => v.id)
  model = ['deepseek-flash', 'deepseek-v4-flash', 'deepseek-chat'].find(v => available.includes(v))
  if (!model) throw new Error('supported model unavailable')
}
const stamp = new Date().toISOString().replaceAll(/[-:.]/g, '')
const root = resolve('.runlogs/jev-mechanism/run-' + stamp)
await mkdir(root, { recursive: true })
const safe = text => Object.values(credentials).reduce((s, key) => s.split(key).join('[REDACTED]'), text)
const selection = process.argv[3] || '^TestReflexMechanismExperiments$'
const child = spawn(resolve(process.env.JEV_MECHANISM_BINARY || '.runlogs/jev-mechanism/experiments.exe'), ['-test.run=' + selection, '-test.v', '-test.count=1', '-test.timeout=90m'], {
  cwd: resolve('exts/jev'), windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'],
  env: { ...process.env, JEV_MECHANISM_EXPERIMENT: '1', JEV_MECHANISM_BROWSER: '1',
    JEV_MECHANISM_LIVE: live ? '1' : '', JEV_MECHANISM_LEARNING: live ? '1' : '', JEV_MECHANISM_STRICT: '1',
    JEV_MECHANISM_REPORT_DIR: root, TYPESAFE_API_KEY: credentials.jev || '', CYBER_API_KEY: credentials.llm || '',
    CYBER_MODEL: model, CYBER_BASE_URL: 'https://api.deepseek.com/v1', CYBER_BROWSER_PATH: 'C:/Program Files/Google/Chrome/Application/chrome.exe' }
})
let log = ''
for (const stream of [child.stdout, child.stderr]) {
  stream.setEncoding('utf8')
  stream.on('data', chunk => { const text = safe(chunk); log += text; process.stdout.write(text) })
}
const code = await new Promise((done, reject) => { child.once('error', reject); child.once('exit', done) })
await writeFile(resolve(root, 'test.log'), log)
const report = JSON.parse(await readFile(resolve(root, 'summary.json'), 'utf8'))
const counts = {}
for (const row of report.rows) {
  const key = row.experiment + '/' + row.condition
  counts[key] ||= { passed: 0, total: 0 }
  counts[key].total++; if (row.accepted) counts[key].passed++
}
await writeFile(resolve(root, 'counts.json'), JSON.stringify({ counts, stages: report.stages, production_unchanged: report.production_unchanged }, null, 2))
console.log('acceptance exit=' + code + ' report=' + root)
process.exitCode = code
