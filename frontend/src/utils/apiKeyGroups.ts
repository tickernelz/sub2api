import type { ApiKey, Group } from '@/types'

export function dedupeGroupIds(groupIds: Array<number | null | undefined>): number[] {
  const seen = new Set<number>()
  const out: number[] = []
  for (const raw of groupIds) {
    if (typeof raw !== 'number' || raw <= 0 || seen.has(raw)) continue
    seen.add(raw)
    out.push(raw)
  }
  return out
}

export function apiKeyAssignedGroupIds(key: Pick<ApiKey, 'group_id' | 'group_ids'>): number[] {
  if (key.group_ids && key.group_ids.length > 0) {
    return dedupeGroupIds(key.group_ids)
  }
  return dedupeGroupIds([key.group_id])
}

export function defaultGroupForAssignedGroups(
  currentDefaultGroupId: number | null | undefined,
  assignedGroupIds: number[]
): number | null {
  const groupIds = dedupeGroupIds(assignedGroupIds)
  if (groupIds.length === 0) return null
  if (typeof currentDefaultGroupId === 'number' && groupIds.includes(currentDefaultGroupId)) {
    return currentDefaultGroupId
  }
  return groupIds[0]
}

export function groupIdsWithDefaultFirst(
  defaultGroupId: number | null | undefined,
  assignedGroupIds: number[]
): number[] {
  if (typeof defaultGroupId !== 'number' || defaultGroupId <= 0) {
    return dedupeGroupIds(assignedGroupIds)
  }
  return dedupeGroupIds([defaultGroupId, ...assignedGroupIds])
}

export function payloadForDefaultGroupChange(
  key: Pick<ApiKey, 'group_id' | 'group_ids'>,
  newDefaultGroupId: number | null
): { group_id: number | null; group_ids: number[] } {
  const assignedGroupIds = apiKeyAssignedGroupIds(key)
  if (assignedGroupIds.length <= 1) {
    return {
      group_id: newDefaultGroupId,
      group_ids: groupIdsWithDefaultFirst(newDefaultGroupId, [])
    }
  }
  const normalizedDefaultGroupId = defaultGroupForAssignedGroups(newDefaultGroupId, assignedGroupIds)
  return {
    group_id: normalizedDefaultGroupId,
    group_ids: groupIdsWithDefaultFirst(normalizedDefaultGroupId, assignedGroupIds)
  }
}

export function displayGroupForApiKey(
  key: Pick<ApiKey, 'group_id' | 'group' | 'group_ids' | 'groups'>
): Group | null {
  const assignedGroupIds = apiKeyAssignedGroupIds(key)
  const defaultGroupId = defaultGroupForAssignedGroups(key.group_id, assignedGroupIds)
  if (key.group && key.group.id === defaultGroupId) return key.group
  return key.groups?.find((group) => group.id === defaultGroupId) ?? key.group ?? null
}

export function groupSelectorOptionsForApiKey<T extends { value: number }>(
  key: Pick<ApiKey, 'group_id' | 'group_ids'> | null,
  options: T[]
): T[] {
  if (!key) return options
  const assigned = apiKeyAssignedGroupIds(key)
  if (assigned.length === 0) return []
  const assignedSet = new Set(assigned)
  return options.filter((option) => assignedSet.has(option.value))
}
