import { create } from '@bufbuild/protobuf'
import { ClaimDefinitionSchema } from '../gen/types/jev_pb'
import { ClaimType, type Claim, type Evaluation } from '../gen/decision/claim_pb'
export function parseJEVJSON(value: string): unknown { try { return JSON.parse(value) } catch { return value } }
export function claimDefinitions(output: unknown) {
  const list = Array.isArray(output) ? output : output && typeof output === 'object' && 'claims' in output ? output.claims : []
  if (!Array.isArray(list)) return []
  return list.flatMap(value => {
    if (!value || typeof value !== 'object' || Object.keys(value).some(key => !['type', 'context', 'options'].includes(key)) || typeof value.context !== 'string' || !value.context.trim() || new TextEncoder().encode(value.context).length > 65536) return []
    const type = value.type === 'choice' ? ClaimType.choice : value.type === 'score' ? ClaimType.score : value.type === 'noul' ? ClaimType.noul : ClaimType.unspecified
    const options = value.options ?? []
    if (!Array.isArray(options) || options.some(option => typeof option !== 'string' || !option.trim() || new TextEncoder().encode(option).length > 1024) || new Set(options).size !== options.length) return []
    if (type === ClaimType.unspecified || (type === ClaimType.noul ? options.length !== 0 : options.length < 2 || options.length > (type === ClaimType.score ? 10 : 64))) return []
    return [create(ClaimDefinitionSchema, { type, context: value.context, options })]
  })
}
export function decisionText(value: unknown): string {
  const decoded = typeof value === 'string' ? parseJEVJSON(value) : value
  if (typeof decoded === 'string') return decoded
  if (decoded && typeof decoded === 'object' && !Array.isArray(decoded)) {
    const definition = decoded as Record<string, unknown>
    if (typeof definition.context === 'string') return definition.context
    if (typeof definition.when === 'string') return definition.when
  }
  return decoded == null ? '' : JSON.stringify(decoded)
}
export const evaluationChoice = (v?: Evaluation) => v?.value.case === 'choice' ? v.value.value : undefined
export const evaluationNumber = (v?: Evaluation) => v?.value.case === 'score' || v?.value.case === 'noul' ? v.value.value : undefined
export const evaluationType = (v?: Evaluation) => v?.value.case === 'choice' ? ClaimType.choice : v?.value.case === 'score' ? ClaimType.score : v?.value.case === 'noul' ? ClaimType.noul : ClaimType.unspecified
export function decisionOptions(claim: Pick<Claim, 'type' | 'context' | 'options'>, answer?: Evaluation) {
  const selected = evaluationChoice(answer)
  return claim.options.map((id, index) => {
    const probability = answer?.probabilities[id]
    return { id, description: id, selected: selected === id, index,
      probability: probability !== undefined && Number.isFinite(probability) && probability >= 0 && probability <= 1 ? probability : undefined }
  }).sort((a,b) => claim.type === ClaimType.score ? a.index-b.index : (b.probability ?? -1)-(a.probability ?? -1) || a.id.localeCompare(b.id))
}
export function decisionQuestions(claims: Record<string, Claim>) {
  const order = ['entry','generation']
  return Object.entries(claims).sort(([a],[b]) => {
    const priority = (id: string) => order.includes(id) ? order.indexOf(id) : order.length
    return priority(a)-priority(b) || a.localeCompare(b,undefined,{numeric:true})
  })
}
