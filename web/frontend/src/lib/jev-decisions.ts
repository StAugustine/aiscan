import { create } from '@bufbuild/protobuf'
import { ClaimDefinitionSchema, type Answer, type Question } from '../gen/types/jev_pb'

export function parseJEVJSON(value: string): unknown {
  try { return JSON.parse(value) } catch { return value }
}

export function claimDefinitions(output: unknown) {
  const list = Array.isArray(output) ? output : output && typeof output === 'object' && 'claims' in output ? output.claims : []
  if (!Array.isArray(list)) return []
  return list.flatMap(value => {
    if (typeof value === 'string' && value.trim()) return [create(ClaimDefinitionSchema, { text: value })]
    if (!value || typeof value !== 'object') return []
    if (typeof value.text === 'string' && value.text.trim()) return [create(ClaimDefinitionSchema, { text: value.text })]
    if (typeof value.when === 'string' && typeof value.question === 'string' && value.options
      && typeof value.options === 'object' && Object.values(value.options).every(option => typeof option === 'string')) {
      return [create(ClaimDefinitionSchema, { when: value.when, question: value.question, options: value.options })]
    }
    return []
  })
}

export function decisionText(value: unknown): string {
  const decoded = typeof value === 'string' ? parseJEVJSON(value) : value
  if (typeof decoded === 'string') return decoded
  if (decoded && typeof decoded === 'object' && !Array.isArray(decoded)) {
    const definition = decoded as Record<string, unknown>
    if (typeof definition.text === 'string' && definition.text) return definition.text
    if (typeof definition.when === 'string') return definition.when
    if (typeof definition.question === 'string') return definition.question
  }
  return decoded == null ? '' : JSON.stringify(decoded)
}

export function decisionOptions(question: Question, answer?: Answer) {
  const criteria = parseJEVJSON(question.criteriaJson)
  const options = criteria && typeof criteria === 'object' && !Array.isArray(criteria)
    ? criteria as Record<string, unknown> : {}
  const ids = [...new Set([...Object.keys(options), ...Object.keys(answer?.probabilities || {}), ...(answer?.choice ? [answer.choice] : [])])]
  return ids.map(id => {
    const probability = answer?.probabilities[id]
    return { id, description: decisionText(options[id] ?? answer?.legend[id]), selected: answer?.choice === id,
      probability: probability !== undefined && Number.isFinite(probability) && probability >= 0 && probability <= 1 ? probability : undefined }
  }).sort((a, b) => (b.probability ?? -1) - (a.probability ?? -1) || a.id.localeCompare(b.id))
}

// Map keys are not a reasoning sequence. This only stabilizes the parallel layout.
export function decisionQuestions(questions: Record<string, Question>) {
  const order = ['entry', 'generation']
  return Object.entries(questions).sort(([a], [b]) => {
    const priority = (id: string) => order.includes(id) ? order.indexOf(id) : order.length
    return priority(a) - priority(b) || a.localeCompare(b, undefined, { numeric: true })
  })
}
