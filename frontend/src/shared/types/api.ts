export type ThemePreference = 'system' | 'light' | 'dark'

export interface AuthUser {
  id: number
  username: string
  is_admin: boolean
  must_change_password: boolean
}

export interface LoginPayload {
  username: string
  password: string
}

export interface ChangeCredentialsPayload {
  password: string
  current_password?: string | undefined
}

export interface SetupState {
  setup_required: boolean
}

export interface FirstAdminSetupPayload {
  username: string
  password: string
  nickname: string
}

export interface SettingsResponse {
  cliaproxy_url: string
  management_key: string
  management_key_set: boolean
  collector_enabled: boolean
  queue_name: string
  batch_size: number
  poll_interval_seconds: number
  retry_interval_seconds: number
}

export interface SettingsUpdatePayload {
  cliaproxy_url?: string
  management_key?: string
  collector_enabled?: boolean
  queue_name?: string
  batch_size?: number
  poll_interval_seconds?: number
  retry_interval_seconds?: number
}

export interface CollectorStatus {
  enabled: boolean
  running: boolean
  queue_name: string
  batch_size: number
  poll_interval_seconds: number
  retry_interval_seconds: number
  last_poll_at: string | null
  last_success_at: string | null
  last_error: string | null
  remote_enabled: boolean | null
  records_collected: number
}

export interface CodexKeeperPriorityRule {
  account_type: string
  priority: number
}

export interface CodexKeeperSettings {
  cliaproxy_url: string
  management_key_set: boolean
  schedule_cron: string
  next_run_times: string[]
  quota_threshold: number
  usage_timeout_seconds: number
  cpa_timeout_seconds: number
  max_retries: number
  worker_threads: number
  dry_run: boolean
  auto_start_daemon: boolean
  priority_rules: CodexKeeperPriorityRule[]
}

export interface CodexKeeperSettingsUpdatePayload {
  schedule_cron?: string
  quota_threshold?: number
  usage_timeout_seconds?: number
  cpa_timeout_seconds?: number
  max_retries?: number
  worker_threads?: number
  dry_run?: boolean
  auto_start_daemon?: boolean
  priority_rules?: CodexKeeperPriorityRule[]
}

export interface CodexKeeperCronPreviewPayload {
  schedule_cron: string
}

export interface CodexKeeperCronPreviewResponse {
  schedule_cron: string
  next_run_times: string[]
}

export interface CodexKeeperStats {
  total: number
  healthy: number
  status_disabled: number
  status_enabled: number
  priority_degraded: number
  priority_restored: number
  skipped: number
  network_error: number
}

export interface CodexKeeperStatus {
  running: boolean
  daemon_running: boolean
  state: string
  detail: string
  mode: string | null
  last_started_at: string | null
  last_finished_at: string | null
  stats: CodexKeeperStats
  logs: string[]
}

export interface CodexKeeperAccount {
  name: string
  email: string | null
  account_type: string | null
  disabled: boolean
  priority: number | null
  primary_used_percent: number | null
  secondary_used_percent: number | null
  primary_reset_at: string | null
  secondary_reset_at: string | null
  quota_threshold: number | null
  last_status_code: number | null
  last_error: string | null
  latest_action: string | null
  last_checked_at: string | null
  last_healthy_at: string | null
}

export interface CodexKeeperAccountsResponse {
  items: CodexKeeperAccount[]
}

export interface CodexKeeperBulkDeletePayload {
  auth_names: string[]
}

export interface CodexKeeperRefreshPayload {
  auth_names: string[]
}

export interface CodexKeeperBulkDeleteFailure {
  name: string
  message: string
}

export interface CodexKeeperBulkDeleteResponse {
  status: string
  deleted: string[]
  failed: CodexKeeperBulkDeleteFailure[]
}

export interface UsageFilters {
  scope?: 'admin' | 'account' | undefined
  start?: string | undefined
  end?: string | undefined
  user_id?: number | undefined
  api_key_description?: string | undefined
  provider?: string | undefined
  model?: string | undefined
  endpoint?: string | undefined
  failed?: boolean | undefined
  request_id?: string | undefined
}

export interface UsageSummary {
  start: string
  end: string
  total_records: number
  failed_records: number
  success_records: number
  input_tokens: number
  output_tokens: number
  cached_tokens: number
  reasoning_tokens: number
  total_tokens: number
  estimated_cost_usd: number
  unpriced_records: number
}

export interface TrendPoint {
  bucket: string
  records: number
  failed_records: number
  total_tokens: number
  estimated_cost_usd: number
}

export interface RankingItem {
  key: string
  label: string
  records: number
  failed_records: number
  total_tokens: number
  estimated_cost_usd: number
  user_id: number | null
  api_key_description: string | null
}

export interface UsageRankingsResponse {
  group_by: 'api_key_description' | 'model' | 'user'
  items: RankingItem[]
}

export interface DistributionItem {
  key: string
  label: string
  records: number
  total_tokens: number
  estimated_cost_usd: number
}

export interface UsageDistributionsResponse {
  providers: DistributionItem[]
  endpoints: DistributionItem[]
}

export interface UsageOptionsResponse {
  users: RankingItem[]
  api_key_descriptions: RankingItem[]
  providers: string[]
  models: string[]
  endpoints: string[]
}

export interface UsageOverviewResponse {
  summary: UsageSummary
  trends: TrendPoint[]
  user_ranking: UsageRankingsResponse
  api_key_description_ranking?: UsageRankingsResponse
  api_key_ranking?: UsageRankingsResponse
  model_ranking: UsageRankingsResponse
  distributions: UsageDistributionsResponse
  options: UsageOptionsResponse
}

export interface UsageRecordListItem {
  id: number
  timestamp: string
  api_key_description: string | null
  user_id: number | null
  user_label: string
  provider: string | null
  model: string | null
  endpoint: string | null
  source: string | null
  request_id: string | null
  auth_index: string | null
  auth: string | null
  latency_ms: number | null
  failed: boolean
  input_tokens: number
  output_tokens: number
  cached_tokens: number
  reasoning_tokens: number
  total_tokens: number
  estimated_cost_usd: number
  unpriced: boolean
}

export interface UsageRecordsResponse {
  items: UsageRecordListItem[]
  total: number
  page: number
  page_size: number
  start: string
  end: string
}

export interface UsageRecordDetail extends UsageRecordListItem {
  raw_json: Record<string, unknown> | unknown[] | string
}

export interface ModelPrice {
  id: number
  provider: string
  model: string
  input_usd_per_million: number
  output_usd_per_million: number
  cached_usd_per_million: number
  reasoning_usd_per_million: number
  source: 'manual' | 'litellm' | string
  source_model: string | null
  auto_synced: boolean
  last_synced_at: string | null
  updated_at: string
}

export interface ModelPricePayload {
  provider: string
  model: string
  input_usd_per_million: number
  output_usd_per_million: number
  cached_usd_per_million: number
  reasoning_usd_per_million: number
}

export interface ModelPriceSyncResponse {
  source_url: string
  total_entries: number
  imported: number
  created: number
  updated: number
  unchanged: number
  skipped_manual: number
  skipped_invalid: number
}

export interface UserApiKeySummary {
  api_key_hash: string
  api_key: string | null
  description: string
  user_id: number | null
  user_name: string | null
  created_at: string | null
  updated_at: string | null
  records: number
  success_records: number
  failed_records: number
  total_tokens: number
  today_records: number
  today_success_records: number
  today_failed_records: number
  today_input_tokens: number
  today_output_tokens: number
  today_cached_tokens: number
  today_reasoning_tokens: number
  today_total_tokens: number
  today_estimated_cost_usd: number
  today_unpriced_records: number
  first_seen_at: string | null
  last_seen_at: string | null
  last_provider: string | null
  last_model: string | null
  providers: string[]
  models: string[]
}

export interface AvailableModelSource {
  api_key_hash: string
  api_key_preview: string
  description: string
}

export interface AvailableModelPrice {
  provider: string
  model: string
  input_usd_per_million: number
  output_usd_per_million: number
  cached_usd_per_million: number
  reasoning_usd_per_million: number
}

export interface AvailableModel {
  id: string
  name: string
  object: string | null
  owner: string | null
  created: number | null
  metadata: Record<string, string | number | boolean | null>
  price: AvailableModelPrice | null
  sources: AvailableModelSource[]
}

export interface AvailableModelKeyError {
  api_key_hash: string
  api_key_preview: string
  description: string
  message: string
}

export interface AvailableModelsResponse {
  has_api_keys: boolean
  api_key_count: number
  queryable_api_key_count: number
  models: AvailableModel[]
  errors: AvailableModelKeyError[]
}

export interface UserSummary {
  id: number
  username: string
  is_admin: boolean
  nickname: string
  disabled_at: string | null
  password_set: boolean
  created_at: string
  updated_at: string
  api_keys: UserApiKeySummary[]
  key_count: number
  records: number
  success_records: number
  failed_records: number
  total_tokens: number
  today_records: number
  today_success_records: number
  today_failed_records: number
  today_input_tokens: number
  today_output_tokens: number
  today_cached_tokens: number
  today_reasoning_tokens: number
  today_total_tokens: number
  today_estimated_cost_usd: number
  today_unpriced_records: number
  first_seen_at: string | null
  last_seen_at: string | null
  last_provider: string | null
  last_model: string | null
  providers: string[]
  models: string[]
}

export interface UserPayload {
  username: string
  password?: string | undefined
  is_admin: boolean
  nickname: string
}

export interface UserApiKeyBindPayload {
  api_key?: string
  api_key_hash?: string
  description: string
}

export interface ApiKeyCreatePayload {
  description: string
}

export interface ApiKeyUpdatePayload {
  description: string
}
