import { apiClient } from '@/shared/api/apiClient'
import type {
  ApiKeyCreatePayload,
  UserApiKeyBindPayload,
  UserApiKeySummary,
  UserPayload,
  UserQuotaPayload,
  UserQuotaStatus,
  UserSummary,
} from '@/shared/types/api'

export function listUsers(): Promise<UserSummary[]> {
  return apiClient.get<UserSummary[]>('/users')
}

export function createUser(payload: UserPayload): Promise<UserSummary> {
  return apiClient.post<UserSummary>('/users', payload)
}

export function updateUser(userId: number, payload: UserPayload): Promise<UserSummary> {
  return apiClient.put<UserSummary>(`/users/${userId}`, payload)
}

export function updateUserQuota(userId: number, payload: UserQuotaPayload): Promise<UserQuotaStatus> {
  return apiClient.put<UserQuotaStatus>(`/users/${userId}/quota`, payload)
}

export function getCurrentUserQuota(): Promise<UserQuotaStatus> {
  return apiClient.get<UserQuotaStatus>('/account/quota')
}

export function disableUser(userId: number): Promise<void> {
  return apiClient.post<void>(`/users/${userId}/disable`)
}

export function enableUser(userId: number): Promise<void> {
  return apiClient.post<void>(`/users/${userId}/enable`)
}

export function listObservedApiKeys(): Promise<UserApiKeySummary[]> {
  return apiClient.get<UserApiKeySummary[]>('/users/observed-api-keys')
}

export function bindUserApiKey(
  userId: number,
  payload: UserApiKeyBindPayload,
): Promise<UserApiKeySummary> {
  return apiClient.post<UserApiKeySummary>(`/users/${userId}/api-keys`, payload)
}

export function createUserApiKey(
  userId: number,
  payload: ApiKeyCreatePayload,
): Promise<UserApiKeySummary> {
  return apiClient.post<UserApiKeySummary>(`/users/${userId}/api-keys/create`, payload)
}

export function unbindUserApiKey(userId: number, apiKeyHash: string): Promise<void> {
  return apiClient.delete(`/users/${userId}/api-keys/${apiKeyHash}`)
}
