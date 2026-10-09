import { spawn } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { boundaryProvider } from './boundary-provider.mjs'

const cwd = fileURLToPath(new URL('..', import.meta.url))
const backendPort = process.env.CYBER_BOUNDARY_BACKEND_PORT || '38480'
const frontendPort = process.env.CYBER_BOUNDARY_PORT || '38479'
const providerPort = process.env.CYBER_BOUNDARY_PROVIDER_PORT || '38481'
const fixture = boundaryProvider()
await new Promise((resolve, reject) => {
  fixture.server.once('error', reject)
  fixture.server.listen(Number(providerPort), '127.0.0.1', resolve)
})
const children = []
let stopping = false
function launch(args, env) {
  const child = spawn(process.execPath, args, { cwd, stdio: 'inherit', windowsHide: true, env: { ...process.env, ...env } })
  children.push(child)
  child.on('error', error => { console.error(error); stop(1) })
  child.on('exit', code => { if (!stopping) stop(code || 1) })
  return child
}
function stop(code) {
  if (stopping) return
  stopping = true
  fixture.release()
  for (const child of children) child.kill('SIGTERM')
  fixture.server.close()
  setTimeout(() => process.exit(code), 1000).unref()
}
process.once('SIGINT', () => stop(130))
process.once('SIGTERM', () => stop(143))
launch(['e2e/start-server.mjs'], {
  CYBER_E2E_PORT: backendPort,
  CYBER_E2E_LLM_BASE_URL: `http://127.0.0.1:${providerPort}/v1`,
  CYBER_E2E_LLM_API_KEY: 'boundary-fixture-key',
  CYBER_E2E_LLM_MODEL: 'boundary-model-a',
  TYPESAFE_API_KEY: '',
})
for (let attempt = 0; ; attempt++) {
  if (stopping) break
  try {
    if ((await fetch(`http://127.0.0.1:${backendPort}/health`)).ok) {
      launch(['node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', frontendPort, '--strictPort'], { CYBER_BACKEND_URL: `http://127.0.0.1:${backendPort}` })
      console.log('Boundary fixture ready')
      break
    }
  } catch {}
  if (attempt >= 1200) { stop(1); throw new Error('Boundary backend did not start') }
  await new Promise(resolve => setTimeout(resolve, 200))
}
