/** What a key is allowed to spend. */
export type Plan = {
  name: string
  perMinute: number
  burst: number
  resetAfterMs: number
}

export const plans: Record<string, Plan> = {
  free: { name: "free", perMinute: 60, burst: 10, resetAfterMs: 60_000 },
  team: { name: "team", perMinute: 600, burst: 100, resetAfterMs: 60_000 },
}

export function planFor(tier: string): Plan {
  return plans[tier] ?? plans.free
}
