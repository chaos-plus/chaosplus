import type {
  RelationshipCondition,
  RelationshipConditionExpression,
} from "./iam-api"

export interface TrustedContextConditionDraft {
  minimumAcr: string
  clientID: string
  networkZone: string
  timeStart: string
  timeEnd: string
  timezone: string
}

export function trustedContextCondition(
  minimumAcr: string,
  clientID: string,
  networkZone: string,
  timeStart: string,
  timeEnd: string,
  timezone: string
): RelationshipCondition | undefined {
  const all: RelationshipConditionExpression[] = []
  if (minimumAcr)
    all.push({
      gte: [{ context: "auth.acr" }, { value: Number(minimumAcr) }],
    })
  if (clientID.trim())
    all.push({
      eq: [{ context: "client.id" }, { value: clientID.trim() }],
    })
  if (networkZone.trim())
    all.push({
      eq: [{ context: "network.zone" }, { value: networkZone.trim() }],
    })
  if (timeStart && timeEnd)
    all.push({ between_time: [timeStart, timeEnd, timezone.trim()] })
  return all.length ? { version: 1, all } : undefined
}

export function trustedContextConditionDraft(
  condition: RelationshipCondition | undefined,
  fallbackTimezone: string
): TrustedContextConditionDraft {
  const draft: TrustedContextConditionDraft = {
    minimumAcr: "",
    clientID: "",
    networkZone: "",
    timeStart: "",
    timeEnd: "",
    timezone: fallbackTimezone,
  }
  const expressions = condition
    ? "all" in condition
      ? condition.all
      : [condition]
    : []
  for (const expression of expressions) {
    if ("gte" in expression) draft.minimumAcr = String(expression.gte[1].value)
    if ("eq" in expression) {
      if (expression.eq[0].context === "client.id")
        draft.clientID = String(expression.eq[1].value)
      if (expression.eq[0].context === "network.zone")
        draft.networkZone = String(expression.eq[1].value)
    }
    if ("between_time" in expression) {
      const [timeStart, timeEnd, timezone] = expression.between_time
      draft.timeStart = timeStart
      draft.timeEnd = timeEnd
      draft.timezone = timezone
    }
  }
  return draft
}

export function trustedContextConditionSummary(
  condition: RelationshipCondition | undefined
): string {
  if (!condition) return "不限"
  return expressionSummary(condition)
}

function expressionSummary(
  expression: RelationshipConditionExpression
): string {
  if ("all" in expression)
    return expression.all.map(expressionSummary).join(" AND ")
  if ("any" in expression)
    return expression.any.map(expressionSummary).join(" OR ")
  if ("not" in expression) return `NOT (${expressionSummary(expression.not)})`
  if ("eq" in expression)
    return `${expression.eq[0].context} = ${expression.eq[1].value}`
  if ("neq" in expression)
    return `${expression.neq[0].context} != ${expression.neq[1].value}`
  if ("gt" in expression) return `ACR > ${expression.gt[1].value}`
  if ("gte" in expression) return `ACR >= ${expression.gte[1].value}`
  if ("lt" in expression) return `ACR < ${expression.lt[1].value}`
  if ("lte" in expression) return `ACR <= ${expression.lte[1].value}`
  if ("in" in expression)
    return `${expression.in[0].context} IN ${expression.in[1].value.join(", ")}`
  if ("contains" in expression)
    return `AMR contains ${expression.contains[1].value}`
  return `${expression.between_time[0]}-${expression.between_time[1]} ${expression.between_time[2]}`
}
