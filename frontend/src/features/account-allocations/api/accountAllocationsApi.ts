import { apiClient } from '@/shared/api/apiClient'
import type {
  AccountAllocationsOverview,
  AccountPool,
  AccountPoolMembersPayload,
  AccountPoolPayload,
  UserAccountAllocation,
  UserAccountAllocationPayload,
} from '@/shared/types/api'

export function getAccountAllocationsOverview(): Promise<AccountAllocationsOverview> {
  return apiClient.get<AccountAllocationsOverview>('/account-allocations/overview')
}

export function createAccountPool(payload: AccountPoolPayload): Promise<AccountPool> {
  return apiClient.post<AccountPool>('/account-allocations/pools', payload)
}

export function updateAccountPool(poolId: number, payload: AccountPoolPayload): Promise<AccountPool> {
  return apiClient.put<AccountPool>(`/account-allocations/pools/${poolId}`, payload)
}

export function deleteAccountPool(poolId: number): Promise<void> {
  return apiClient.delete(`/account-allocations/pools/${poolId}`)
}

export function replaceAccountPoolMembers(
  poolId: number,
  payload: AccountPoolMembersPayload,
): Promise<AccountPool> {
  return apiClient.put<AccountPool>(`/account-allocations/pools/${poolId}/members`, payload)
}

export function createUserAccountAllocation(
  payload: UserAccountAllocationPayload,
): Promise<UserAccountAllocation> {
  return apiClient.post<UserAccountAllocation>('/account-allocations/allocations', payload)
}

export function updateUserAccountAllocation(
  allocationId: number,
  payload: UserAccountAllocationPayload,
): Promise<UserAccountAllocation> {
  return apiClient.put<UserAccountAllocation>(
    `/account-allocations/allocations/${allocationId}`,
    payload,
  )
}

export function deleteUserAccountAllocation(allocationId: number): Promise<void> {
  return apiClient.delete(`/account-allocations/allocations/${allocationId}`)
}
