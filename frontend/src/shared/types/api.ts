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
  model_request_url: string
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
  model_request_url?: string
  management_key?: string
  collector_enabled?: boolean
  queue_name?: string
  batch_size?: number
  poll_interval_seconds?: number
  retry_interval_seconds?: number
}

export interface ModelRequestGuide {
  model_request_url: string
  openai_base_url: string
  chat_completions_url: string
}

export type ModelRequestEndpoint = 'chat_completions' | 'responses' | 'claude_messages'

export interface ModelRequestTestPayload {
  api_key_hash: string
  endpoint: ModelRequestEndpoint
  model: string
  message: string
}

export interface ModelRequestTestResponse {
  endpoint: ModelRequestEndpoint
  model: string
  reply: string
  status_code: number
  duration_ms: number
  usage?: Record<string, unknown>
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

export interface CardShopProductItem {
  name?: string | null
  price?: number | null
  stockCount?: number | null
  salesCount?: number | null
  itemUrl?: string | null
  category?: string | null
  group?: string | null
}

export interface CardShop {
  id?: string | null
  shopName?: string | null
  shopUrl?: string | null
  telegram?: string | null
  shopSellCount?: number | null
  productsInStock?: string[] | null
  productItems?: CardShopProductItem[] | null
  notes?: string | null
  updatedAt?: string | null
}

export interface CardShopsResponse {
  shops: CardShop[]
  fetched_at: string
}

export interface CardShopFavoritesResponse {
  shop_keys: string[]
}

export interface CardShopFavoriteUpdatePayload {
  shop_key: string
  favorite: boolean
}

export interface CardShopTagsResponse {
  tags: string[]
}

export interface CardShopTagsUpdatePayload {
  tags: string[]
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
  conditional_refresh_interval_seconds: number
  account_refresh_cache_minutes: number
  dry_run: boolean
  enable_credential_websockets: boolean
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
  conditional_refresh_interval_seconds?: number
  account_refresh_cache_minutes?: number
  dry_run?: boolean
  enable_credential_websockets?: boolean
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
  running_modes: string[]
  daemon_running: boolean
  state: string
  detail: string
  mode: string | null
  last_started_at: string | null
  last_finished_at: string | null
  stats: CodexKeeperStats
  logs: string[]
}

export interface CodexKeeperQuotaWindowUsage {
  window_start: string
  window_end: string
  reset_at: string
  window_seconds: number
  records: number
  success_records: number
  failed_records: number
  input_tokens: number
  output_tokens: number
  cached_tokens: number
  reasoning_tokens: number
  total_tokens: number
  estimated_cost_usd: number
  unpriced_records: number
  stale: boolean
  window_source: string
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
  primary_window_seconds: number | null
  secondary_window_seconds: number | null
  primary_window_usage: CodexKeeperQuotaWindowUsage | null
  secondary_window_usage: CodexKeeperQuotaWindowUsage | null
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
  source_key?: string | undefined
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
  average_ttft_ms: number | null
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
  models: DistributionItem[]
  endpoints: DistributionItem[]
}

export interface UsageOptionsResponse {
  users: RankingItem[]
  api_key_descriptions: RankingItem[]
  providers: string[]
  models: string[]
  sources: UsageSourceOption[]
  endpoints: string[]
}

export interface UsageSourceOption {
  key: string
  label: string
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
  reasoning_effort: string | null
  endpoint: string | null
  source: string | null
  request_id: string | null
  auth_index: string | null
  auth: string | null
  latency_ms: number | null
  ttft_ms: number | null
  failed: boolean
  input_tokens: number
  output_tokens: number
  cached_tokens: number
  cache_read_tokens: number
  cache_creation_tokens: number
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
  cache_read_usd_per_million: number
  cache_creation_usd_per_million: number
  request_usd: number | null
  billing_unit: 'token' | 'request' | string
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
  cache_read_usd_per_million: number
  cache_creation_usd_per_million: number
  request_usd: number | null
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

export interface ModelPriceCatalogItem {
  id: string
  name: string
  object: string | null
  owner: string | null
  created: number | null
  metadata: Record<string, string | number | boolean | null>
  suggested_provider: string
  price: ModelPrice | null
  sources: AvailableModelSource[]
}

export interface ModelPriceCatalogResponse {
  has_api_keys: boolean
  api_key_count: number
  queryable_api_key_count: number
  models: ModelPriceCatalogItem[]
  errors: AvailableModelKeyError[]
  priced_models: number
  unpriced_models: number
}

export interface LiteLLMProxySettings {
  enabled: boolean
  proxy_url: string
}

export interface LiteLLMProxySettingsPayload {
  enabled: boolean
  proxy_url: string
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

export interface UserQuotaStatus {
  unlimited: boolean
  lifetime_quota_usd: number | null
  lifetime_remaining_usd: number | null
  monthly_quota_usd: number | null
  monthly_used_usd: number
  monthly_remaining_usd: number | null
  quota_month: string
  paused: boolean
  paused_at: string | null
  pause_reason: string | null
  sync_error: string | null
  unpriced_records: number
  can_create_keys: boolean
  started_at: string | null
}

export interface AvailableModelSource {
  api_key_hash: string
  api_key_preview: string
  description: string
  user_id?: number
  user_label?: string
}

export interface AvailableModelPrice {
  provider: string
  model: string
  input_usd_per_million: number
  output_usd_per_million: number
  cache_read_usd_per_million: number
  cache_creation_usd_per_million: number
  request_usd: number | null
  billing_unit: 'token' | 'request' | string
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
  quota: UserQuotaStatus
}

export interface UserPayload {
  username: string
  password?: string | undefined
  is_admin: boolean
  nickname: string
}

export interface UserQuotaPayload {
  lifetime_quota_usd: number | null
  monthly_quota_usd: number | null
}

export interface UserApiKeyBindPayload {
  api_key?: string
  api_key_hash?: string
  description: string
}

export interface ApiKeyCreatePayload {
  description: string
  api_key?: string
}

export interface ApiKeyUpdatePayload {
  description: string
}
