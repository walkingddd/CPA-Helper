import { apiClient } from '@/shared/api/apiClient'
import type {
  ChannelStatusResponse,
  CodexKeeperBulkDeletePayload,
  CodexKeeperBulkDeleteResponse,
  CodexKeeperCronPreviewPayload,
  CodexKeeperCronPreviewResponse,
  CodexKeeperAccountsResponse,
  CodexKeeperOAuthCallbackPayload,
  CodexKeeperOAuthCallbackResponse,
  CodexKeeperOAuthStartPayload,
  CodexKeeperOAuthStartResponse,
  CodexKeeperOAuthStatusResponse,
  CodexKeeperRefreshPayload,
  CodexKeeperSettings,
  CodexKeeperSettingsUpdatePayload,
  CodexKeeperStatus,
} from '@/shared/types/api'

export function getChannelStatus(): Promise<ChannelStatusResponse> {
  return apiClient.get<ChannelStatusResponse>('/channel-status')
}

export function getCodexKeeperSettings(): Promise<CodexKeeperSettings> {
  return apiClient.get<CodexKeeperSettings>('/codex-keeper/settings')
}

export function updateCodexKeeperSettings(
  payload: CodexKeeperSettingsUpdatePayload,
): Promise<CodexKeeperSettings> {
  return apiClient.put<CodexKeeperSettings>('/codex-keeper/settings', payload)
}

export function previewCodexKeeperSchedule(
  payload: CodexKeeperCronPreviewPayload,
): Promise<CodexKeeperCronPreviewResponse> {
  return apiClient.post<CodexKeeperCronPreviewResponse>('/codex-keeper/schedule/preview', payload)
}

export function getCodexKeeperStatus(): Promise<CodexKeeperStatus> {
  return apiClient.get<CodexKeeperStatus>('/codex-keeper/status')
}

export function listCodexKeeperAccounts(): Promise<CodexKeeperAccountsResponse> {
  return apiClient.get<CodexKeeperAccountsResponse>('/codex-keeper/accounts')
}

export function runCodexKeeperOnce(): Promise<void> {
  return apiClient.post<void>('/codex-keeper/run-once')
}

export function startCodexKeeper(): Promise<void> {
  return apiClient.post<void>('/codex-keeper/start')
}

export function stopCodexKeeper(): Promise<void> {
  return apiClient.post<void>('/codex-keeper/stop')
}

export function clearCodexKeeperLogs(): Promise<void> {
  return apiClient.post<void>('/codex-keeper/logs/clear')
}

export function startCodexKeeperOAuth(
  payload: CodexKeeperOAuthStartPayload,
): Promise<CodexKeeperOAuthStartResponse> {
  return apiClient.post<CodexKeeperOAuthStartResponse>('/codex-keeper/oauth/start', payload)
}

export function getCodexKeeperOAuthStatus(state: string): Promise<CodexKeeperOAuthStatusResponse> {
  return apiClient.get<CodexKeeperOAuthStatusResponse>('/codex-keeper/oauth/status', { state })
}

export function submitCodexKeeperOAuthCallback(
  payload: CodexKeeperOAuthCallbackPayload,
): Promise<CodexKeeperOAuthCallbackResponse> {
  return apiClient.post<CodexKeeperOAuthCallbackResponse>('/codex-keeper/oauth/callback', payload)
}

export function enableCodexKeeperAccount(authName: string): Promise<void> {
  return apiClient.post<void>(`/codex-keeper/accounts/${encodeURIComponent(authName)}/enable`)
}

export function disableCodexKeeperAccount(authName: string): Promise<void> {
  return apiClient.post<void>(`/codex-keeper/accounts/${encodeURIComponent(authName)}/disable`)
}

export function deleteCodexKeeperAccount(authName: string): Promise<void> {
  return apiClient.delete(`/codex-keeper/accounts/${encodeURIComponent(authName)}`)
}

export function bulkDeleteCodexKeeperAccounts(
  payload: CodexKeeperBulkDeletePayload,
): Promise<CodexKeeperBulkDeleteResponse> {
  return apiClient.post<CodexKeeperBulkDeleteResponse>(
    '/codex-keeper/accounts/bulk-delete',
    payload,
  )
}

export function refreshCodexKeeperAccounts(payload: CodexKeeperRefreshPayload): Promise<void> {
  return apiClient.post<void>('/codex-keeper/accounts/refresh', payload)
}

export function updateCodexKeeperPriority(authName: string, priority: number): Promise<void> {
  return apiClient.patch<void>(`/codex-keeper/accounts/${encodeURIComponent(authName)}/priority`, {
    priority,
  })
}
