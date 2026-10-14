<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, type Component } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NButton, NDatePicker, NSelect, NSpin, useMessage } from 'naive-ui'
import {
  CircleDollarSign,
  ClipboardList,
  Gauge,
  Layers3,
  ShieldCheck,
  Timer,
} from 'lucide-vue-next'

import { getUsageOverview } from '@/features/usage/api/usageApi'
import { getCurrentUserQuota } from '@/features/users/api/usersApi'
import ChartPanel, { type ChartOption } from '@/features/usage/components/ChartPanel.vue'
import type {
  DistributionItem,
  RankingItem,
  TrendPoint,
  UsageDistributionsResponse,
  UsageFilters,
  UsageOptionsResponse,
  UsageOverviewResponse,
  UsageRankingsResponse,
  UsageSummary,
  UserQuotaStatus,
} from '@/shared/types/api'
import {
  BEIJING_TIME_ZONE,
  formatCompact,
  formatDateTime,
  formatInteger,
  formatLocalDateTimeParam,
  formatUsd,
} from '@/shared/utils/format'
import { useI18n } from '@/shared/i18n'

type FailedFilter = 'all' | 'success' | 'failed'
type QuickRangeKey = 'today' | 'last24h' | 'last3d' | 'last7d' | 'last30d'
type UsageScope = 'admin' | 'account'

interface RefreshOptions {
  silent?: boolean
}

interface Props {
  scope: UsageScope
}

interface MetricCardConfig {
  key: string
  label: string
  value: string
  icon: Component
  tone: string
  footnote: string
}

interface DistributionLegendItem {
  key: string
  label: string
  recordsText: string
  percentText: string
  colorIndex: number
}

interface TokenBreakdownItem {
  key: string
  label: string
  value: number
  valueText: string
  percentText: string
  colorIndex: number
}

interface HourActivityItem {
  hour: number
  label: string
  records: number
  tokens: number
  recordTitle: string
  tokenTitle: string
  recordStyle: Record<string, string>
  tokenStyle: Record<string, string>
}

const AUTO_REFRESH_INTERVAL_MS = 5000
const HOUR_MS = 60 * 60 * 1000
const DAY_MS = 24 * HOUR_MS
const THIRTY_MINUTES_MS = 30 * 60 * 1000
const DISTRIBUTION_CHART_COLORS = [
  { token: '--cpa-chart-1', fallback: '#009aa8' },
  { token: '--cpa-chart-2', fallback: '#1d8dff' },
  { token: '--cpa-chart-3', fallback: '#7e66f2' },
  { token: '--cpa-chart-4', fallback: '#f58a2f' },
  { token: '--cpa-chart-5', fallback: '#18a058' },
] as const

const route = useRoute()
const router = useRouter()
const message = useMessage()
const props = defineProps<Props>()
const { currentLanguage, errorText, t } = useI18n()
const isLoading = ref(false)
const isAutoRefreshing = ref(false)
const autoRefreshError = ref<string | null>(null)
const auxiliaryError = ref<string | null>(null)
const lastRefreshedAt = ref<Date | null>(null)
const filtersExpanded = ref(false)
const summary = ref<UsageSummary | null>(null)
const quotaStatus = ref<UserQuotaStatus | null>(null)
const realtimeSummary = ref<UsageSummary | null>(null)
const todayTrends = ref<TrendPoint[]>([])
const failedSummary = ref<UsageSummary | null>(null)
const failedTrends = ref<TrendPoint[]>([])
const trends = ref<TrendPoint[]>([])
const userRanking = ref<RankingItem[]>([])
const modelRanking = ref<RankingItem[]>([])
const distributions = ref<UsageDistributionsResponse>({ providers: [], models: [], endpoints: [] })
const failedEndpointDistribution = ref<DistributionItem[]>([])
const options = ref<UsageOptionsResponse>({
  users: [],
  api_key_descriptions: [],
  providers: [],
  models: [],
  sources: [],
  endpoints: [],
})

function normalizeUsageOptions(nextOptions: UsageOptionsResponse): UsageOptionsResponse {
  return {
    users: nextOptions.users ?? [],
    api_key_descriptions: nextOptions.api_key_descriptions ?? [],
    providers: nextOptions.providers ?? [],
    models: nextOptions.models ?? [],
    sources: nextOptions.sources ?? [],
    endpoints: nextOptions.endpoints ?? [],
  }
}

function normalizeUsageDistributions(
  nextDistributions: Partial<UsageDistributionsResponse> | undefined,
): UsageDistributionsResponse {
  return {
    providers: nextDistributions?.providers ?? [],
    models: nextDistributions?.models ?? [],
    endpoints: nextDistributions?.endpoints ?? [],
  }
}

function emptyRanking(groupBy: UsageRankingsResponse['group_by']): UsageRankingsResponse {
  return { group_by: groupBy, items: [] }
}

function descriptionRanking(overview: UsageOverviewResponse): UsageRankingsResponse {
  return (
    overview.api_key_description_ranking ??
    overview.api_key_ranking ??
    emptyRanking('api_key_description')
  )
}

function initialRange(): [number, number] | null {
  const startQuery = typeof route.query.start === 'string' ? route.query.start : ''
  const endQuery = typeof route.query.end === 'string' ? route.query.end : ''
  if (startQuery && endQuery) {
    return [new Date(startQuery).getTime(), new Date(endQuery).getTime()]
  }
  return null
}

function todayRange(): [number, number] {
  const now = new Date()
  const start = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const tomorrow = new Date(start)
  tomorrow.setDate(start.getDate() + 1)
  return [start.getTime(), tomorrow.getTime()]
}

function rollingRange(durationMs: number): [number, number] {
  const end = Date.now()
  return [end - durationMs, end]
}

function isTodayRange(range: [number, number] | null): boolean {
  if (!range) {
    return false
  }
  const [todayStart, tomorrowStart] = todayRange()
  return range[0] === todayStart && range[1] === tomorrowStart
}

function buildQuickRange(key: QuickRangeKey): [number, number] {
  switch (key) {
    case 'today':
      return todayRange()
    case 'last24h':
      return rollingRange(24 * HOUR_MS)
    case 'last3d':
      return rollingRange(3 * DAY_MS)
    case 'last7d':
      return rollingRange(7 * DAY_MS)
    case 'last30d':
      return rollingRange(30 * DAY_MS)
  }
}

function isQuickRangeKey(value: unknown): value is QuickRangeKey {
  return typeof value === 'string' && quickRangeOptions.value.some((option) => option.key === value)
}

function quickRangeFromQuery(): QuickRangeKey | null {
  const value = route.query.quick_range
  return isQuickRangeKey(value) ? value : null
}

function inferQuickRangeFromRange(range: [number, number] | null): QuickRangeKey | null {
  if (!range) {
    return null
  }
  if (isTodayRange(range)) {
    return 'today'
  }

  const duration = range[1] - range[0]
  const endDrift = Math.abs(Date.now() - range[1])
  const durationToleranceMs = 2 * 60 * 1000
  const refreshToleranceMs = 10 * 60 * 1000
  const rollingRanges: Array<{ key: QuickRangeKey; durationMs: number }> = [
    { key: 'last24h', durationMs: 24 * HOUR_MS },
    { key: 'last3d', durationMs: 3 * DAY_MS },
    { key: 'last7d', durationMs: 7 * DAY_MS },
    { key: 'last30d', durationMs: 30 * DAY_MS },
  ]

  if (endDrift > refreshToleranceMs) {
    return null
  }

  return (
    rollingRanges.find((item) => Math.abs(duration - item.durationMs) <= durationToleranceMs)
      ?.key ?? null
  )
}

function failedFromQuery(): FailedFilter {
  if (route.query.failed === 'true') {
    return 'failed'
  }
  if (route.query.failed === 'false') {
    return 'success'
  }
  return 'all'
}

function numberFromQuery(value: unknown): number | null {
  if (typeof value !== 'string' || !value) {
    return null
  }
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : null
}

const quickRangeOptions = computed<Array<{ key: QuickRangeKey; label: string }>>(() => [
  { key: 'today', label: t('今日', 'Today') },
  { key: 'last24h', label: t('近 24 小时', 'Last 24 hours') },
  { key: 'last3d', label: t('近 3 日', 'Last 3 days') },
  { key: 'last7d', label: t('近 7 日', 'Last 7 days') },
  { key: 'last30d', label: t('近 30 日', 'Last 30 days') },
])

const initialQuickRange = quickRangeFromQuery()
const initialDateRange = initialRange()
const inferredQuickRange = initialQuickRange ?? inferQuickRangeFromRange(initialDateRange)
const dateRange = ref<[number, number] | null>(
  inferredQuickRange ? buildQuickRange(inferredQuickRange) : initialDateRange,
)
const activeQuickRange = ref<QuickRangeKey | null>(
  inferredQuickRange ?? (initialDateRange === null ? 'today' : null),
)
const filterForm = reactive({
  user_id: numberFromQuery(route.query.user_id),
  api_key_description:
    typeof route.query.api_key_description === 'string' ? route.query.api_key_description : null,
  provider: typeof route.query.provider === 'string' ? route.query.provider : null,
  model: typeof route.query.model === 'string' ? route.query.model : null,
  endpoint: typeof route.query.endpoint === 'string' ? route.query.endpoint : null,
  failed: failedFromQuery(),
})

const failedFilterOptions = computed(() => [
  { label: t('全部', 'All'), value: 'all' },
  { label: t('成功', 'Success'), value: 'success' },
  { label: t('失败', 'Failed'), value: 'failed' },
])

function apiKeyFilterLabel(item: UsageOptionsResponse['api_key_descriptions'][number]): string {
  return item.label?.trim() || item.key
}

const selectOptions = computed(() => ({
  users: options.value.users
    .filter((item) => item.user_id !== null)
    .map((item) => ({ label: item.label, value: item.user_id as number })),
  apiKeyDescriptions: options.value.api_key_descriptions.map((item) => ({
    label: apiKeyFilterLabel(item),
    value: item.key,
  })),
  providers: options.value.providers.map((item) => ({ label: item, value: item })),
  models: options.value.models.map((item) => ({ label: item, value: item })),
  endpoints: options.value.endpoints.map((item) => ({ label: item, value: item })),
}))

const isAccountScope = computed(() => props.scope === 'account')
const pageTitle = computed(() =>
  isAccountScope.value ? t('我的用量', 'My usage') : t('历史用量', 'Usage history'),
)
const pageSubtitle = computed(() =>
  isAccountScope.value
    ? t('仅聚合当前登录账号自己的本地用量记录', 'Only local usage records for your account are aggregated')
    : t(
        '按本地 SQLite 历史记录实时聚合，费用按当前模型价格估算',
        'Aggregated live from local SQLite history. Costs are estimated using current model prices.',
      ),
)
const rankingTitle = computed(() =>
  isAccountScope.value ? t('KEY 排行', 'Key ranking') : t('用户排行', 'User ranking'),
)

const refreshStatusText = computed(() => {
  const lastRefreshTime = lastRefreshedAt.value
  if (!lastRefreshTime) {
    return autoRefreshError.value
      ? t('自动刷新异常 · 尚无成功同步', 'Auto refresh error · no successful sync yet')
      : t('每 5 秒自动刷新 · 等待首次同步', 'Auto refresh every 5 seconds · waiting for first sync')
  }
  const lastRefreshText = new Intl.DateTimeFormat(currentLanguage.value === 'zh' ? 'zh-CN' : 'en-US', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(lastRefreshTime)
  if (autoRefreshError.value) {
    return t(`自动刷新异常 · 最近成功 ${lastRefreshText}`, `Auto refresh error · last success ${lastRefreshText}`)
  }
  if (auxiliaryError.value) {
    return t(`已同步 ${lastRefreshText} · 辅助指标降级`, `Synced ${lastRefreshText} · auxiliary metrics degraded`)
  }
  return t(`每 5 秒自动刷新 · 最近 ${lastRefreshText}`, `Auto refresh every 5 seconds · latest ${lastRefreshText}`)
})

const dashboardRangeLabel = computed(() => {
  const activeRange = quickRangeOptions.value.find((option) => option.key === activeQuickRange.value)
  if (activeRange) {
    return activeRange.label
  }
  const currentSummary = summary.value
  if (currentSummary) {
    return `${formatDateTime(currentSummary.start, { includeSecond: false })} - ${formatDateTime(
      currentSummary.end,
      { includeSecond: false },
    )}`
  }
  const range = dateRange.value
  if (!range) {
    return t('当前筛选', 'Current filters')
  }
  return `${formatMetricRangeTime(range[0])} - ${formatMetricRangeTime(range[1])}`
})

const rateRangeLabel = computed(() =>
  activeQuickRange.value === 'today' ? t('近 30 分钟', 'Last 30 minutes') : dashboardRangeLabel.value,
)

function formatMetricRangeTime(value: number): string {
  return new Intl.DateTimeFormat(currentLanguage.value === 'zh' ? 'zh-CN' : 'en-US', {
    timeZone: BEIJING_TIME_ZONE,
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(new Date(value))
}

function buildFilters(): UsageFilters {
  const failed =
    filterForm.failed === 'all' ? undefined : filterForm.failed === 'failed' ? true : false
  return {
    scope: props.scope,
    start: dateRange.value ? formatLocalDateTimeParam(dateRange.value[0]) : undefined,
    end: dateRange.value ? formatLocalDateTimeParam(dateRange.value[1]) : undefined,
    user_id: isAccountScope.value ? undefined : (filterForm.user_id ?? undefined),
    api_key_description: filterForm.api_key_description ?? undefined,
    provider: filterForm.provider ?? undefined,
    model: filterForm.model ?? undefined,
    endpoint: filterForm.endpoint ?? undefined,
    failed,
  }
}

function filtersToQuery(
  filters: UsageFilters,
  quickRangeKey: QuickRangeKey | null = null,
): Record<string, string> {
  const query: Record<string, string> = {}
  Object.entries(filters).forEach(([key, value]) => {
    if (key !== 'scope' && value !== undefined && value !== '') {
      query[key] = String(value)
    }
  })
  if (quickRangeKey) {
    query.quick_range = quickRangeKey
  }
  return query
}

function normalizeRangeValue(value: unknown): [number, number] | null {
  if (
    Array.isArray(value) &&
    value.length === 2 &&
    typeof value[0] === 'number' &&
    typeof value[1] === 'number'
  ) {
    return [value[0], value[1]]
  }
  return null
}

function normalizeSelectValue(value: unknown): string | null {
  if (value === null || value === undefined || value === '') {
    return null
  }
  return String(value)
}

function refreshAfterFilterChange() {
  void refresh()
}

function handleCustomRangeChange(value: unknown) {
  dateRange.value = normalizeRangeValue(value)
  activeQuickRange.value = null
  refreshAfterFilterChange()
}

function handleApiKeyChange(value: unknown) {
  filterForm.api_key_description = normalizeSelectValue(value)
  refreshAfterFilterChange()
}

function handleUserChange(value: unknown) {
  filterForm.user_id = typeof value === 'number' ? value : null
  refreshAfterFilterChange()
}

function handleProviderChange(value: unknown) {
  filterForm.provider = normalizeSelectValue(value)
  refreshAfterFilterChange()
}

function handleModelChange(value: unknown) {
  filterForm.model = normalizeSelectValue(value)
  refreshAfterFilterChange()
}

function handleEndpointChange(value: unknown) {
  filterForm.endpoint = normalizeSelectValue(value)
  refreshAfterFilterChange()
}

function handleFailedChange(value: unknown) {
  filterForm.failed = value === 'success' || value === 'failed' ? value : 'all'
  refreshAfterFilterChange()
}

async function applyQuickRange(key: QuickRangeKey) {
  activeQuickRange.value = key
  dateRange.value = buildQuickRange(key)
  await refresh()
}

let queuedRefresh: RefreshOptions | null = null

function queueRefresh(options: RefreshOptions) {
  if (options.silent) {
    return
  }
  queuedRefresh = { silent: false }
}

async function refresh({ silent = false }: RefreshOptions = {}) {
  if (isLoading.value || isAutoRefreshing.value) {
    queueRefresh({ silent })
    return
  }
  if (activeQuickRange.value) {
    dateRange.value = buildQuickRange(activeQuickRange.value)
  }
  if (silent) {
    isAutoRefreshing.value = true
  } else {
    isLoading.value = true
  }
  try {
    const filters = buildFilters()
    const usedServerDefaultRange = filters.start === undefined && filters.end === undefined
    const [todayStart, todayEnd] = todayRange()
    const [realtimeStart, realtimeEnd] = rollingRange(THIRTY_MINUTES_MS)
    const todayFilters: UsageFilters = {
      ...filters,
      start: formatLocalDateTimeParam(todayStart),
      end: formatLocalDateTimeParam(todayEnd),
    }
    const failedFilters: UsageFilters = { ...filters, failed: true }
    const realtimeRequest =
      activeQuickRange.value === 'today'
        ? getUsageOverview({
            ...filters,
            start: formatLocalDateTimeParam(realtimeStart),
            end: formatLocalDateTimeParam(realtimeEnd),
          })
        : Promise.resolve(null)
    const quotaRequest = isAccountScope.value ? getCurrentUserQuota() : Promise.resolve(null)
    const [overviewResult, todayResult, failedResult, realtimeResult, quotaResult] =
      await Promise.allSettled([
        getUsageOverview(filters),
        getUsageOverview(todayFilters),
        getUsageOverview(failedFilters),
        realtimeRequest,
        quotaRequest,
      ] as const)

    if (overviewResult.status === 'rejected') {
      throw overviewResult.reason
    }

    const overview = overviewResult.value
    summary.value = overview.summary
    if (usedServerDefaultRange) {
      dateRange.value = [
        new Date(overview.summary.start).getTime(),
        new Date(overview.summary.end).getTime(),
      ]
    }
    trends.value = overview.trends
    userRanking.value = isAccountScope.value
      ? descriptionRanking(overview).items
      : (overview.user_ranking ?? emptyRanking('user')).items
    modelRanking.value = (overview.model_ranking ?? emptyRanking('model')).items
    distributions.value = normalizeUsageDistributions(overview.distributions)
    options.value = normalizeUsageOptions(overview.options)

    if (todayResult.status === 'fulfilled') {
      todayTrends.value = todayResult.value.trends
    } else {
      todayTrends.value = []
    }

    if (failedResult.status === 'fulfilled') {
      failedSummary.value = failedResult.value.summary
      failedTrends.value = failedResult.value.trends
      failedEndpointDistribution.value = failedResult.value.distributions.endpoints ?? []
    } else {
      failedSummary.value = null
      failedTrends.value = []
      failedEndpointDistribution.value = []
    }

    if (realtimeResult.status === 'fulfilled') {
      realtimeSummary.value = realtimeResult.value?.summary ?? null
    } else {
      realtimeSummary.value = null
    }

    if (quotaResult.status === 'fulfilled') {
      quotaStatus.value = quotaResult.value
    } else if (isAccountScope.value) {
      quotaStatus.value = null
    }

    auxiliaryError.value =
      todayResult.status === 'rejected' ||
      failedResult.status === 'rejected' ||
      realtimeResult.status === 'rejected' ||
      quotaResult.status === 'rejected'
        ? t('部分辅助指标加载失败', 'Some auxiliary metrics failed to load')
        : null

    void router.replace({
      query: filtersToQuery(
        usedServerDefaultRange
          ? { ...filters, start: overview.summary.start, end: overview.summary.end }
          : filters,
        activeQuickRange.value,
      ),
    })
    autoRefreshError.value = null
    lastRefreshedAt.value = new Date()
  } catch (error) {
    const errorMessage = errorText(error, '加载历史用量失败', 'Failed to load usage history')
    if (silent) {
      autoRefreshError.value = errorMessage
    } else {
      message.error(errorMessage)
    }
  } finally {
    if (silent) {
      isAutoRefreshing.value = false
    } else {
      isLoading.value = false
    }
    const nextRefresh = queuedRefresh
    queuedRefresh = null
    if (nextRefresh) {
      void refresh(nextRefresh)
    }
  }
}

function goRecords(extra: UsageFilters = {}) {
  const filters = { ...buildFilters(), ...extra }
  void router.push({
    name: isAccountScope.value ? 'account-records' : 'admin-records',
    query: filtersToQuery(filters),
  })
}

function rankingFilters(row: RankingItem): UsageFilters {
  if (!isAccountScope.value && row.user_id) {
    return { user_id: row.user_id }
  }
  if (row.api_key_description) {
    return { api_key_description: row.api_key_description }
  }
  return {}
}

function modelFilters(row: RankingItem): UsageFilters {
  const [provider, model] = row.key.split('::')
  const filters: UsageFilters = {}
  if (provider) {
    filters.provider = provider
  }
  if (model) {
    filters.model = model
  }
  return filters
}

function cssVar(name: string, fallback: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || fallback
}

function distributionChartColors(): string[] {
  return DISTRIBUTION_CHART_COLORS.map((color) => cssVar(color.token, color.fallback))
}

function distributionMarkerStyle(index: number): Record<string, string> {
  const color =
    DISTRIBUTION_CHART_COLORS[index % DISTRIBUTION_CHART_COLORS.length] ??
    DISTRIBUTION_CHART_COLORS[0]
  return {
    '--distribution-color': `var(${color.token}, ${color.fallback})`,
  }
}

function distributionLegendItems(items: DistributionItem[]): DistributionLegendItem[] {
  const totalRecords = items.reduce((sum, item) => sum + item.records, 0)

  return items.map((item, index) => ({
    key: item.key,
    label: item.label,
    recordsText: formatCompact(item.records),
    percentText: totalRecords > 0 ? `${Math.round((item.records / totalRecords) * 100)}%` : '0%',
    colorIndex: index,
  }))
}

function formatPercent(value: number): string {
  return new Intl.NumberFormat(currentLanguage.value === 'zh' ? 'zh-CN' : 'en-US', {
    style: 'percent',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(value)
}

function formatRate(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return '0'
  }
  if (value >= 1000) {
    return formatCompact(value)
  }
  return new Intl.NumberFormat('en-US', {
    maximumFractionDigits: value < 10 ? 1 : 0,
  }).format(value)
}

function formatLatency(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value) || value <= 0) {
    return '-'
  }
  return `${formatInteger(Math.round(value))} ms`
}

function parseAPITime(value: string | null | undefined): number | null {
  if (!value) {
    return null
  }
  const parsed = new Date(value).getTime()
  return Number.isNaN(parsed) ? null : parsed
}

function summaryDurationMinutes(value: UsageSummary | null): number {
  const start = parseAPITime(value?.start)
  const end = parseAPITime(value?.end)
  if (start === null || end === null || end <= start) {
    return 1
  }
  return Math.max(1, (end - start) / 60_000)
}

const successRate = computed(() => {
  const currentSummary = summary.value
  if (!currentSummary || currentSummary.total_records === 0) {
    return 0
  }
  return currentSummary.success_records / currentSummary.total_records
})

const failedRate = computed(() => {
  const currentSummary = summary.value
  if (!currentSummary || currentSummary.total_records === 0) {
    return 0
  }
  return currentSummary.failed_records / currentSummary.total_records
})

const rateSummary = computed(() =>
  activeQuickRange.value === 'today' && realtimeSummary.value ? realtimeSummary.value : summary.value,
)

const requestsPerMinute = computed(() => {
  const currentSummary = rateSummary.value
  return (currentSummary?.total_records ?? 0) / summaryDurationMinutes(currentSummary)
})

function quotaValueText(quota: UserQuotaStatus | null): string {
  if (!quota) {
    return t('加载中', 'Loading')
  }
  if (quota.unlimited) {
    return t('每月余额 无限制', 'Monthly balance unlimited')
  }
  return t(
    `每月余额 ${formatUsd(quota.monthly_remaining_usd ?? 0)}`,
    `Monthly balance ${formatUsd(quota.monthly_remaining_usd ?? 0)}`,
  )
}

function quotaFootnote(quota: UserQuotaStatus | null): string {
  if (!quota) {
    return t('额度加载中', 'Quota loading')
  }
  if (quota.unlimited) {
    return t('不限时余额 无限制', 'Lifetime balance unlimited')
  }
  const lifetimeText = t(
    `不限时余额 ${formatUsd(quota.lifetime_remaining_usd ?? 0)}`,
    `Lifetime balance ${formatUsd(quota.lifetime_remaining_usd ?? 0)}`,
  )
  const notes: string[] = []
  if (quota.sync_error) {
    notes.push(t('Key 同步异常', 'Key sync error'))
  }
  if (quota.unpriced_records > 0) {
    notes.push(t(`未定价 ${formatInteger(quota.unpriced_records)} 条`, `${formatInteger(quota.unpriced_records)} unpriced records`))
  }
  if (quota.paused) {
    notes.push(t('Key 已因余额暂停', 'Key paused due to balance'))
  }
  return notes.length > 0 ? `${lifetimeText} · ${notes.join(' · ')}` : lifetimeText
}

const metricCards = computed<MetricCardConfig[]>(() => {
  const currentSummary = summary.value
  return [
    {
      key: 'requests',
      label: t('请求数', 'Requests'),
      value: formatInteger(currentSummary?.total_records ?? 0),
      icon: ClipboardList,
      tone: 'blue',
      footnote: dashboardRangeLabel.value,
    },
    {
      key: 'success',
      label: t('成功率', 'Success rate'),
      value: formatPercent(successRate.value),
      icon: ShieldCheck,
      tone: 'green',
      footnote: t(
        `${formatInteger(currentSummary?.success_records ?? 0)} 成功 / ${formatInteger(
          currentSummary?.total_records ?? 0,
        )} 请求`,
        `${formatInteger(currentSummary?.success_records ?? 0)} succeeded / ${formatInteger(
          currentSummary?.total_records ?? 0,
        )} requests`,
      ),
    },
    {
      key: 'total_tokens',
      label: t('总 Token', 'Total tokens'),
      value: formatCompact(currentSummary?.total_tokens ?? 0),
      icon: Layers3,
      tone: 'purple',
      footnote: t(
        `输入 ${formatCompact(currentSummary?.input_tokens ?? 0)} / 输出 ${formatCompact(
          currentSummary?.output_tokens ?? 0,
        )}`,
        `Input ${formatCompact(currentSummary?.input_tokens ?? 0)} / output ${formatCompact(
          currentSummary?.output_tokens ?? 0,
        )}`,
      ),
    },
    {
      key: 'rpm',
      label: 'RPM',
      value: formatRate(requestsPerMinute.value),
      icon: Gauge,
      tone: 'orange',
      footnote: rateRangeLabel.value,
    },
    {
      key: 'average_ttft',
      label: t('平均首字耗时', 'Avg TTFT'),
      value: formatLatency(currentSummary?.average_ttft_ms ?? null),
      icon: Timer,
      tone: 'teal',
      footnote: dashboardRangeLabel.value,
    },
    {
      key: 'cost',
      label: t('估算费用', 'Estimated cost'),
      value: formatUsd(currentSummary?.estimated_cost_usd ?? 0),
      icon: CircleDollarSign,
      tone: 'green',
      footnote:
        (currentSummary?.unpriced_records ?? 0) > 0
          ? t(
              `未计价 ${formatInteger(currentSummary?.unpriced_records ?? 0)} 条`,
              `${formatInteger(currentSummary?.unpriced_records ?? 0)} unpriced records`,
            )
          : t('按当前价格估算', 'Estimated at current prices'),
    },
  ]
})

function formatTrendBucket(value: string): string {
  const formatted = formatDateTime(value, { includeSecond: false })
  return formatted === '-' ? value : formatted
}

const trendOption = computed<ChartOption>(() => {
  const mutedColor = cssVar('--cpa-text-muted', '#6a7d87')
  const gridColor = cssVar('--cpa-chart-grid', 'rgba(120, 146, 151, 0.18)')
  const requestColor = cssVar('--cpa-accent-blue', '#1d8dff')
  const tokenColor = cssVar('--cpa-primary', '#009aa8')
  const dangerColor = cssVar('--cpa-danger', '#d34b4b')

  return {
    tooltip: { trigger: 'axis' },
    legend: {
      top: 2,
      left: 54,
      right: 96,
      itemGap: 14,
      itemWidth: 10,
      itemHeight: 10,
      data: [t('请求数', 'Requests'), 'Token', t('失败请求', 'Failed requests')],
    },
    grid: { left: 42, right: 58, top: 44, bottom: 34 },
    xAxis: {
      type: 'category',
      data: trends.value.map((item) => item.bucket),
      axisLabel: {
        hideOverlap: true,
        color: mutedColor,
        formatter: (value: string) => formatTrendBucket(value),
      },
      axisLine: { lineStyle: { color: gridColor } },
      axisTick: { show: false },
    },
    yAxis: [
      {
        type: 'value',
        name: t('请求', 'Requests'),
        nameTextStyle: { color: mutedColor },
        axisLabel: { color: mutedColor, formatter: (value: number) => formatCompact(value) },
        splitLine: { lineStyle: { color: gridColor } },
      },
      {
        type: 'value',
        name: 'Token',
        nameTextStyle: { color: mutedColor },
        axisLabel: { color: mutedColor, formatter: (value: number) => formatCompact(value) },
        splitLine: { show: false },
      },
    ],
    series: [
      {
        name: t('请求数', 'Requests'),
        type: 'bar',
        data: trends.value.map((item) => item.records),
        barMaxWidth: 18,
        itemStyle: { color: requestColor, borderRadius: [4, 4, 0, 0] },
      },
      {
        name: 'Token',
        type: 'line',
        yAxisIndex: 1,
        smooth: true,
        showSymbol: false,
        data: trends.value.map((item) => item.total_tokens),
        lineStyle: { color: tokenColor, width: 3 },
        itemStyle: { color: tokenColor },
      },
      {
        name: t('失败请求', 'Failed requests'),
        type: 'line',
        data: trends.value.map((item) => item.failed_records),
        showSymbol: true,
        symbolSize: 6,
        lineStyle: { color: dangerColor, width: 1, type: 'dashed' },
        itemStyle: { color: dangerColor },
      },
    ],
  }
})

const tokenBreakdownItems = computed<TokenBreakdownItem[]>(() => {
  const currentSummary = summary.value
  const values = [
    { key: 'input', label: t('输入 Token', 'Input tokens'), value: currentSummary?.input_tokens ?? 0 },
    { key: 'output', label: t('输出 Token', 'Output tokens'), value: currentSummary?.output_tokens ?? 0 },
    { key: 'cached', label: t('缓存 Token', 'Cached tokens'), value: currentSummary?.cached_tokens ?? 0 },
    { key: 'reasoning', label: t('推理 Token', 'Reasoning tokens'), value: currentSummary?.reasoning_tokens ?? 0 },
  ]
  const total = values.reduce((sum, item) => sum + item.value, 0)
  return values.map((item, index) => ({
    ...item,
    valueText: formatCompact(item.value),
    percentText: total > 0 ? `${Math.round((item.value / total) * 100)}%` : '0%',
    colorIndex: index,
  }))
})

const tokenBreakdownTotal = computed(() =>
  tokenBreakdownItems.value.reduce((sum, item) => sum + item.value, 0),
)

const tokenBreakdownOption = computed<ChartOption>(() =>
  breakdownPieOption(
    tokenBreakdownItems.value.map((item) => ({ label: item.label, value: item.value })),
    'Token',
    formatCompact(summary.value?.total_tokens ?? 0),
  ),
)

function breakdownPieOption(
  items: Array<{ label: string; value: number }>,
  name: string,
  centerValue: string,
): ChartOption {
  const surfaceColor = cssVar('--cpa-surface', '#ffffff')
  const textColor = cssVar('--cpa-text-strong', '#172026')
  const mutedColor = cssVar('--cpa-text-muted', '#667981')

  return {
    tooltip: {
      trigger: 'item',
      formatter: `${name}<br/>{b}: {c} ({d}%)`,
    },
    color: distributionChartColors(),
    legend: { show: false },
    series: [
      {
        name,
        type: 'pie',
        radius: ['52%', '74%'],
        center: ['50%', '52%'],
        startAngle: 94,
        minAngle: 4,
        avoidLabelOverlap: true,
        label: { show: false },
        labelLine: { show: false },
        itemStyle: {
          borderColor: surfaceColor,
          borderWidth: 3,
          borderRadius: 5,
        },
        emphasis: {
          scaleSize: 3,
          itemStyle: {
            shadowBlur: 10,
            shadowColor: 'rgba(0, 154, 168, 0.18)',
          },
        },
        data: items.map((item, index) => ({
          name: item.label,
          value: item.value,
          label:
            index === 0
              ? {
                  show: true,
                  position: 'center',
                  formatter: `{total|${centerValue}}\n{caption|${name}}`,
                  rich: {
                    total: {
                      color: textColor,
                      fontSize: 22,
                      fontWeight: 750,
                      lineHeight: 28,
                    },
                    caption: {
                      color: mutedColor,
                      fontSize: 12,
                      lineHeight: 18,
                    },
                  },
                }
              : { show: false },
        })),
      },
    ],
  }
}

function distributionPieOption(items: DistributionItem[], name: string): ChartOption {
  const totalRecords = items.reduce((sum, item) => sum + item.records, 0)
  return breakdownPieOption(
    items.map((item) => ({ label: item.label, value: item.records })),
    name,
    formatCompact(totalRecords),
  )
}

function hourFromBucket(bucket: string): number | null {
  const localHourMatch = bucket.match(/\s(\d{2})(?::\d{2})?$/)
  if (localHourMatch) {
    const hour = Number(localHourMatch[1])
    return hour >= 0 && hour <= 23 ? hour : null
  }
  const parsed = new Date(bucket)
  if (Number.isNaN(parsed.getTime())) {
    return null
  }
  return Number(
    new Intl.DateTimeFormat('en-US', {
      timeZone: BEIJING_TIME_ZONE,
      hour: '2-digit',
      hour12: false,
    }).format(parsed),
  )
}

const hourActivityItems = computed<HourActivityItem[]>(() => {
  const byHour = new Map<number, { records: number; tokens: number }>()
  todayTrends.value.forEach((item) => {
    const hour = hourFromBucket(item.bucket)
    if (hour === null) {
      return
    }
    const current = byHour.get(hour) ?? { records: 0, tokens: 0 }
    current.records += item.records
    current.tokens += item.total_tokens
    byHour.set(hour, current)
  })
  const maxRecords = Math.max(1, ...[...byHour.values()].map((item) => item.records))
  const maxTokens = Math.max(1, ...[...byHour.values()].map((item) => item.tokens))
  return Array.from({ length: 24 }, (_, hour) => {
    const value = byHour.get(hour) ?? { records: 0, tokens: 0 }
    const recordIntensity = value.records === 0 ? 0 : Math.max(0.12, value.records / maxRecords)
    const tokenIntensity = value.tokens === 0 ? 0 : Math.max(0.12, value.tokens / maxTokens)
    const hourLabel = String(hour).padStart(2, '0')
    return {
      hour,
      label: hourLabel,
      records: value.records,
      tokens: value.tokens,
      recordTitle: t(
        `${hourLabel}:00 · ${formatInteger(value.records)} 次请求`,
        `${hourLabel}:00 · ${formatInteger(value.records)} requests`,
      ),
      tokenTitle: `${hourLabel}:00 · ${formatCompact(value.tokens)} Token`,
      recordStyle: { '--heat-intensity': recordIntensity.toFixed(3) },
      tokenStyle: { '--heat-intensity': tokenIntensity.toFixed(3) },
    }
  })
})

const todayRecordTotal = computed(() =>
  hourActivityItems.value.reduce((sum, item) => sum + item.records, 0),
)
const todayTokenTotal = computed(() =>
  hourActivityItems.value.reduce((sum, item) => sum + item.tokens, 0),
)

const primaryRankingRows = computed(() => userRanking.value.slice(0, 5))
const modelRankingRows = computed(() => modelRanking.value.slice(0, 5))
const maxPrimaryRankingTokens = computed(() =>
  Math.max(0, ...primaryRankingRows.value.map((item) => item.total_tokens)),
)
const maxModelRankingTokens = computed(() =>
  Math.max(0, ...modelRankingRows.value.map((item) => item.total_tokens)),
)

function rankingBarStyle(value: number, maxValue: number): Record<string, string> {
  const percent = maxValue > 0 ? Math.max(4, Math.round((value / maxValue) * 100)) : 0
  return { '--ranking-width': `${percent}%` }
}

const topFailedEndpoint = computed(() => failedEndpointDistribution.value[0] ?? null)
const recentFailedRows = computed(() =>
  failedTrends.value
    .filter((item) => item.records > 0)
    .slice(-4)
    .reverse(),
)

const anomalyStats = computed(() => [
  {
    key: 'failed_rate',
    label: t('失败率', 'Failure rate'),
    value: formatPercent(failedRate.value),
    tone: failedRate.value > 0 ? 'danger' : 'success',
  },
  {
    key: 'failed_records',
    label: t('失败请求', 'Failed requests'),
    value: formatInteger(summary.value?.failed_records ?? failedSummary.value?.total_records ?? 0),
    tone: 'danger',
  },
  {
    key: 'unpriced',
    label: t('未计价', 'Unpriced'),
    value: formatInteger(summary.value?.unpriced_records ?? 0),
    tone: (summary.value?.unpriced_records ?? 0) > 0 ? 'warning' : 'success',
  },
])

const providerDistributionLegend = computed(() =>
  distributionLegendItems(distributions.value.providers),
)
const endpointDistributionLegend = computed(() =>
  distributionLegendItems(distributions.value.endpoints),
)

const providerDistributionOption = computed<ChartOption>(() =>
  distributionPieOption(distributions.value.providers, t('服务商', 'Providers')),
)
const endpointDistributionOption = computed<ChartOption>(() =>
  distributionPieOption(distributions.value.endpoints, t('接口', 'Endpoints')),
)

let autoRefreshTimer: number | undefined

onMounted(() => {
  void refresh()
  autoRefreshTimer = window.setInterval(() => {
    void refresh({ silent: true })
  }, AUTO_REFRESH_INTERVAL_MS)
})

onBeforeUnmount(() => {
  if (autoRefreshTimer !== undefined) {
    window.clearInterval(autoRefreshTimer)
  }
})
</script>

<template>
  <section class="page usage-dashboard-page" :aria-busy="isLoading">
    <div class="page-header">
      <div>
        <h1 class="page-title">{{ pageTitle }}</h1>
        <p class="page-subtitle">{{ pageSubtitle }}</p>
      </div>
      <div class="header-actions">
        <span
          v-if="isAccountScope"
          class="quota-status-pill"
          :class="{
            'is-paused': quotaStatus?.paused,
            'is-warning': (quotaStatus?.unpriced_records ?? 0) > 0 || !!quotaStatus?.sync_error,
          }"
          :title="quotaFootnote(quotaStatus)"
        >
          <CircleDollarSign :size="14" :stroke-width="2.2" aria-hidden="true" />
          <strong>{{ quotaValueText(quotaStatus) }}</strong>
          <small>{{ quotaFootnote(quotaStatus) }}</small>
        </span>
        <span class="refresh-status" :class="{ 'is-error': autoRefreshError }">
          {{ refreshStatusText }}
        </span>
        <NButton secondary @click="goRecords()">{{ t('明细', 'Records') }}</NButton>
      </div>
    </div>

    <section class="panel filter-panel" :class="{ 'is-expanded': filtersExpanded }">
      <div class="filter-summary">
        <div>
          <strong>{{ t('筛选', 'Filters') }}</strong>
          <span>{{ dashboardRangeLabel }}</span>
        </div>
        <NButton class="filter-toggle" secondary size="small" @click="filtersExpanded = !filtersExpanded">
          {{ filtersExpanded ? t('收起', 'Collapse') : t('展开', 'Expand') }}
        </NButton>
      </div>
      <div class="panel-inner filter-toolbar">
        <div class="time-row">
          <div class="quick-ranges" role="group" :aria-label="t('快捷时间范围', 'Quick time ranges')">
            <NButton
              v-for="option in quickRangeOptions"
              :key="option.key"
              class="quick-range-button"
              size="small"
              secondary
              :type="activeQuickRange === option.key ? 'primary' : 'default'"
              @click="applyQuickRange(option.key)"
            >
              {{ option.label }}
            </NButton>
          </div>
          <NDatePicker
            :value="dateRange"
            class="range-picker"
            type="datetimerange"
            clearable
            @update:value="handleCustomRangeChange"
          />
        </div>
        <div class="field-row" :class="{ 'is-account-scope': isAccountScope }">
          <NSelect
            v-if="!isAccountScope"
            :value="filterForm.user_id"
            :options="selectOptions.users"
            clearable
            filterable
            :placeholder="t('用户昵称', 'User nickname')"
            @update:value="handleUserChange"
          />
          <NSelect
            :value="filterForm.api_key_description"
            :options="selectOptions.apiKeyDescriptions"
            clearable
            filterable
            :placeholder="t('KEY 描述', 'Key description')"
            @update:value="handleApiKeyChange"
          />
          <NSelect
            :value="filterForm.provider"
            :options="selectOptions.providers"
            clearable
            filterable
            :placeholder="t('服务商', 'Provider')"
            @update:value="handleProviderChange"
          />
          <NSelect
            :value="filterForm.model"
            :options="selectOptions.models"
            clearable
            filterable
            :placeholder="t('模型', 'Model')"
            @update:value="handleModelChange"
          />
          <NSelect
            :value="filterForm.endpoint"
            :options="selectOptions.endpoints"
            clearable
            filterable
            :placeholder="t('接口', 'Endpoint')"
            @update:value="handleEndpointChange"
          />
          <div class="status-actions">
            <NSelect
              :value="filterForm.failed"
              class="status-select"
              :options="failedFilterOptions"
              @update:value="handleFailedChange"
            />
            <NButton secondary :loading="isLoading" @click="refresh()">{{ t('筛选', 'Filter') }}</NButton>
          </div>
        </div>
      </div>
    </section>

    <NSpin :show="isLoading">
      <div class="metric-grid dashboard-metric-grid">
        <div
          v-for="metric in metricCards"
          :key="metric.key"
          class="metric-card dashboard-metric-card"
          :class="`is-${metric.tone}`"
        >
          <div class="metric-icon" aria-hidden="true">
            <component :is="metric.icon" :size="20" :stroke-width="2.2" />
          </div>
          <div class="metric-label">{{ metric.label }}</div>
          <div class="metric-value">{{ metric.value }}</div>
          <div class="metric-footnote usage-metric-footnote" :title="metric.footnote">
            {{ metric.footnote }}
          </div>
        </div>
      </div>

      <div class="dashboard-layout">
        <div class="dashboard-top-grid">
          <ChartPanel
            class="usage-trend-panel area-trend"
            :title="t('用量趋势', 'Usage trend')"
            :option="trendOption"
            :empty="trends.length === 0"
            :loading="isLoading"
          />

          <ChartPanel
            class="token-panel area-token"
            :title="t('Token 构成', 'Token breakdown')"
            :option="tokenBreakdownOption"
            :empty="tokenBreakdownTotal === 0"
            :loading="isLoading"
            :compact-footer="tokenBreakdownItems.length <= 1"
          >
            <ol class="distribution-legend token-legend" :aria-label="t('Token 构成图例', 'Token breakdown legend')">
              <li
                v-for="item in tokenBreakdownItems"
                :key="item.key"
                class="distribution-legend-item"
              >
                <span
                  class="distribution-marker"
                  :style="distributionMarkerStyle(item.colorIndex)"
                  aria-hidden="true"
                />
                <span class="distribution-label">{{ item.label }}</span>
                <span class="distribution-count">{{ item.valueText }}</span>
                <span class="distribution-percent">{{ item.percentText }}</span>
              </li>
            </ol>
          </ChartPanel>
        </div>

        <div class="dashboard-columns">
          <div class="dashboard-column dashboard-column-left">
            <section class="panel heatmap-panel area-heatmap">
              <div class="panel-inner compact-panel-inner">
                <div class="panel-heading-row">
                  <h2 class="section-title">{{ t('小时活跃（今日）', 'Hourly activity (today)') }}</h2>
                  <span class="panel-subtle-text">
                    {{ auxiliaryError ? t('辅助数据降级', 'Auxiliary data degraded') : t('请求数 / Token', 'Requests / tokens') }}
                  </span>
                </div>
                <div class="heatmap-groups">
                  <div class="heatmap-group is-records">
                    <div class="heatmap-group-heading">
                      <span>{{ t('请求数', 'Requests') }}</span>
                      <strong>{{ t(`${formatInteger(todayRecordTotal)} 次`, `${formatInteger(todayRecordTotal)} requests`) }}</strong>
                    </div>
                    <div class="hour-heatmap is-records" :aria-label="t('今日请求数小时活跃热力图', 'Today hourly request heatmap')">
                      <div
                        v-for="item in hourActivityItems"
                        :key="`records-${item.hour}`"
                        class="hour-cell"
                        :class="{ 'is-empty': item.records === 0 }"
                        :style="item.recordStyle"
                        :title="item.recordTitle"
                      >
                        <span>{{ item.label }}</span>
                      </div>
                    </div>
                  </div>
                  <div class="heatmap-group is-tokens">
                    <div class="heatmap-group-heading">
                      <span>Token</span>
                      <strong>{{ formatCompact(todayTokenTotal) }}</strong>
                    </div>
                    <div class="hour-heatmap is-tokens" :aria-label="t('今日 Token 小时活跃热力图', 'Today hourly token heatmap')">
                      <div
                        v-for="item in hourActivityItems"
                        :key="`tokens-${item.hour}`"
                        class="hour-cell"
                        :class="{ 'is-empty': item.tokens === 0 }"
                        :style="item.tokenStyle"
                        :title="item.tokenTitle"
                      >
                        <span>{{ item.label }}</span>
                      </div>
                    </div>
                  </div>
                </div>
                <div class="heatmap-scale">
                  <span>{{ t('低', 'Low') }}</span>
                  <div class="heatmap-scale-bars" aria-hidden="true">
                    <i class="is-records" />
                    <i class="is-tokens" />
                  </div>
                  <span>{{ t('高', 'High') }}</span>
                </div>
              </div>
            </section>

            <ChartPanel
              class="distribution-panel area-provider"
              :title="t('服务商分布', 'Provider distribution')"
              :option="providerDistributionOption"
              :empty="distributions.providers.length === 0"
              :loading="isLoading"
              :compact-footer="providerDistributionLegend.length <= 1"
            >
              <ol
                class="distribution-legend"
                :class="{ 'is-single': providerDistributionLegend.length === 1 }"
                :aria-label="t('服务商分布图例', 'Provider distribution legend')"
              >
                <li
                  v-for="item in providerDistributionLegend"
                  :key="item.key"
                  class="distribution-legend-item"
                >
                  <span
                    class="distribution-marker"
                    :style="distributionMarkerStyle(item.colorIndex)"
                    aria-hidden="true"
                  />
                  <span class="distribution-label" :title="item.label">{{ item.label }}</span>
                  <span class="distribution-count">{{ item.recordsText }}</span>
                  <span class="distribution-percent">{{ item.percentText }}</span>
                </li>
              </ol>
            </ChartPanel>
          </div>

          <div class="dashboard-column dashboard-column-middle">
            <section class="panel anomaly-panel area-anomaly">
              <div class="panel-inner compact-panel-inner">
                <div class="panel-heading-row">
                  <h2 class="section-title">{{ t('异常概览', 'Anomaly overview') }}</h2>
                  <NButton size="small" quaternary @click="goRecords({ failed: true })">{{ t('更多', 'More') }}</NButton>
                </div>
                <div class="anomaly-stat-grid">
                  <div
                    v-for="item in anomalyStats"
                    :key="item.key"
                    class="anomaly-stat"
                    :class="`is-${item.tone}`"
                  >
                    <span>{{ item.label }}</span>
                    <strong>{{ item.value }}</strong>
                  </div>
                </div>
                <div class="top-failed-endpoint">
                  <span>{{ t('Top 失败接口', 'Top failed endpoint') }}</span>
                  <strong :title="topFailedEndpoint?.label ?? t('暂无失败接口', 'No failed endpoints')">
                    {{ topFailedEndpoint?.label ?? t('暂无失败接口', 'No failed endpoints') }}
                  </strong>
                </div>
                <div class="recent-failed-list">
                  <div v-if="recentFailedRows.length === 0" class="empty-inline">
                    {{ t('当前范围暂无失败请求', 'No failed requests in the current range') }}
                  </div>
                  <button
                    v-for="item in recentFailedRows"
                    v-else
                    :key="item.bucket"
                    class="recent-failed-row"
                    type="button"
                    @click="goRecords({ failed: true })"
                  >
                    <span>{{ formatTrendBucket(item.bucket) }}</span>
                    <strong>{{ t(`${formatInteger(item.records)} 次`, `${formatInteger(item.records)} requests`) }}</strong>
                    <em>{{ formatCompact(item.total_tokens) }} Token</em>
                  </button>
                </div>
              </div>
            </section>

            <ChartPanel
              class="distribution-panel area-endpoint"
              :title="t('接口分布', 'Endpoint distribution')"
              :option="endpointDistributionOption"
              :empty="distributions.endpoints.length === 0"
              :loading="isLoading"
              :compact-footer="endpointDistributionLegend.length <= 1"
            >
              <ol
                class="distribution-legend"
                :class="{ 'is-single': endpointDistributionLegend.length === 1 }"
                :aria-label="t('接口分布图例', 'Endpoint distribution legend')"
              >
                <li
                  v-for="item in endpointDistributionLegend"
                  :key="item.key"
                  class="distribution-legend-item"
                >
                  <span
                    class="distribution-marker"
                    :style="distributionMarkerStyle(item.colorIndex)"
                    aria-hidden="true"
                  />
                  <span class="distribution-label" :title="item.label">{{ item.label }}</span>
                  <span class="distribution-count">{{ item.recordsText }}</span>
                  <span class="distribution-percent">{{ item.percentText }}</span>
                </li>
              </ol>
            </ChartPanel>
          </div>

          <div class="dashboard-column dashboard-column-right">
            <section class="panel ranking-panel area-primary-ranking">
              <div class="panel-inner compact-panel-inner">
                <div class="panel-heading-row">
                  <h2 class="section-title">{{ rankingTitle }}</h2>
                  <span class="panel-subtle-text">{{ t('按 Token 排序', 'Sorted by tokens') }}</span>
                </div>
                <div class="ranking-list">
                  <div v-if="primaryRankingRows.length === 0" class="empty-inline">{{ t('暂无排行数据', 'No ranking data') }}</div>
                  <div
                    v-for="(row, index) in primaryRankingRows"
                    v-else
                    :key="row.key"
                    class="ranking-row"
                    :style="rankingBarStyle(row.total_tokens, maxPrimaryRankingTokens)"
                  >
                    <span class="ranking-index">{{ index + 1 }}</span>
                    <div class="ranking-main">
                      <div class="ranking-label-line">
                        <strong :title="row.label">{{ row.label }}</strong>
                        <span>{{ formatUsd(row.estimated_cost_usd) }}</span>
                      </div>
                      <div class="ranking-track" aria-hidden="true"><span /></div>
                    </div>
                    <div class="ranking-values">
                      <strong>{{ formatCompact(row.total_tokens) }}</strong>
                      <span>{{ t(`${formatInteger(row.records)} 次`, `${formatInteger(row.records)} requests`) }}</span>
                    </div>
                    <NButton size="tiny" quaternary @click="goRecords(rankingFilters(row))">
                      {{ t('明细', 'Records') }}
                    </NButton>
                  </div>
                </div>
              </div>
            </section>

            <section class="panel ranking-panel area-model-ranking">
              <div class="panel-inner compact-panel-inner">
                <div class="panel-heading-row">
                  <h2 class="section-title">{{ t('模型排行', 'Model ranking') }}</h2>
                  <span class="panel-subtle-text">{{ t('按 Token 排序', 'Sorted by tokens') }}</span>
                </div>
                <div class="ranking-list">
                  <div v-if="modelRankingRows.length === 0" class="empty-inline">{{ t('暂无模型数据', 'No model data') }}</div>
                  <div
                    v-for="(row, index) in modelRankingRows"
                    v-else
                    :key="row.key"
                    class="ranking-row"
                    :style="rankingBarStyle(row.total_tokens, maxModelRankingTokens)"
                  >
                    <span class="ranking-index">{{ index + 1 }}</span>
                    <div class="ranking-main">
                      <div class="ranking-label-line">
                        <strong :title="row.label">{{ row.label }}</strong>
                        <span>{{ formatUsd(row.estimated_cost_usd) }}</span>
                      </div>
                      <div class="ranking-track" aria-hidden="true"><span /></div>
                    </div>
                    <div class="ranking-values">
                      <strong>{{ formatCompact(row.total_tokens) }}</strong>
                      <span>{{ t(`${formatInteger(row.records)} 次`, `${formatInteger(row.records)} requests`) }}</span>
                    </div>
                    <NButton size="tiny" quaternary @click="goRecords(modelFilters(row))">
                      {{ t('明细', 'Records') }}
                    </NButton>
                  </div>
                </div>
              </div>
            </section>
          </div>
        </div>
      </div>
    </NSpin>
  </section>
</template>

<style scoped>
.usage-dashboard-page {
  gap: 10px;
}

.filter-panel {
  overflow: visible;
}

.filter-summary {
  display: none;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 11px 12px;
  border-bottom: 1px solid var(--cpa-border);
}

.filter-summary > div {
  display: grid;
  min-width: 0;
  gap: 2px;
}

.filter-summary strong {
  color: var(--cpa-text-strong);
  font-size: 13px;
  line-height: 1.2;
}

.filter-summary span {
  min-width: 0;
  overflow: hidden;
  color: var(--cpa-text-muted);
  font-size: 12px;
  line-height: 1.25;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.filter-toggle {
  flex: 0 0 auto;
}

.filter-toolbar {
  display: grid;
  gap: 8px;
  padding: 10px 12px;
}

.time-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(280px, 430px);
  gap: 10px;
  align-items: center;
  min-width: 0;
}

.field-row {
  display: grid;
  grid-template-columns: repeat(5, minmax(118px, 1fr)) auto;
  gap: 8px;
  align-items: end;
  min-width: 0;
}

.field-row.is-account-scope {
  grid-template-columns: repeat(4, minmax(118px, 1fr)) auto;
}

.range-picker {
  min-width: 0;
}

.quick-ranges {
  display: flex;
  flex-wrap: wrap;
  gap: 7px;
  min-width: 0;
}

.quick-range-button {
  flex: 0 0 auto;
  min-width: 72px;
  border-radius: 999px;
  font-weight: 750;
}

.status-actions {
  display: flex;
  gap: 8px;
  min-width: 0;
}

.status-select {
  min-width: 96px;
}

.status-actions :deep(.n-button) {
  min-width: 78px;
}

.header-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
  min-width: 0;
}

.quota-status-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  max-width: 420px;
  min-width: 0;
  padding: 5px 9px;
  border: 1px solid color-mix(in srgb, var(--cpa-primary) 18%, var(--cpa-border));
  border-radius: var(--cpa-radius-sm);
  background: color-mix(in srgb, var(--cpa-primary-wash) 72%, var(--cpa-surface));
  color: var(--cpa-primary);
  line-height: 1.2;
  white-space: nowrap;
}

.quota-status-pill strong,
.quota-status-pill small {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

.quota-status-pill strong {
  color: var(--cpa-text-strong);
  font-size: 12px;
  font-weight: 760;
}

.quota-status-pill small {
  color: var(--cpa-text-strong);
  font-size: 12px;
  font-weight: 760;
}

.quota-status-pill.is-warning {
  border-color: color-mix(in srgb, var(--cpa-warning) 24%, var(--cpa-border));
  background: color-mix(in srgb, var(--cpa-warning-weak) 68%, var(--cpa-surface));
  color: var(--cpa-warning);
}

.quota-status-pill.is-paused {
  border-color: color-mix(in srgb, var(--cpa-danger) 24%, var(--cpa-border));
  background: color-mix(in srgb, var(--cpa-danger-weak) 68%, var(--cpa-surface));
  color: var(--cpa-danger);
}

.refresh-status {
  color: var(--cpa-text-muted);
  font-size: 12px;
  white-space: nowrap;
}

.refresh-status.is-error {
  color: var(--cpa-danger);
}

.dashboard-metric-grid {
  gap: 8px;
}

.dashboard-metric-card {
  min-height: 96px;
  padding: 14px;
}

.dashboard-metric-card .metric-value {
  font-size: 22px;
}

.usage-metric-footnote {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dashboard-layout {
  display: grid;
  gap: 12px;
  margin-top: 12px;
  min-width: 0;
}

.dashboard-top-grid,
.dashboard-columns {
  display: grid;
  gap: 12px;
  min-width: 0;
}

.dashboard-top-grid {
  grid-template-columns: minmax(0, 7fr) minmax(360px, 5fr);
  align-items: stretch;
}

.dashboard-columns {
  grid-template-columns: repeat(3, minmax(0, 1fr));
  align-items: start;
  --dashboard-card-height: 306px;
}

.dashboard-column {
  display: grid;
  align-content: start;
  gap: 12px;
  min-width: 0;
}

.area-trend,
.area-token,
.area-anomaly,
.area-heatmap,
.area-primary-ranking,
.area-model-ranking,
.area-provider,
.area-endpoint {
  min-width: 0;
}

.area-heatmap {
  order: 1;
}

.area-anomaly {
  order: 2;
}

.area-primary-ranking {
  order: 3;
}

.area-model-ranking {
  order: 4;
}

.area-provider {
  order: 5;
}

.area-endpoint {
  order: 6;
}

.area-anomaly,
.area-heatmap,
.area-primary-ranking,
.area-model-ranking,
.area-provider,
.area-endpoint {
  height: var(--dashboard-card-height);
}

.usage-trend-panel.chart-panel {
  min-height: 296px;
}

.usage-trend-panel.chart-panel :deep(.chart-body),
.usage-trend-panel.chart-panel :deep(.chart-surface),
.usage-trend-panel.chart-panel :deep(.chart-empty) {
  height: 238px;
}

.token-panel.chart-panel,
.token-panel.chart-panel.has-chart-footer,
.token-panel.chart-panel.has-chart-footer.has-compact-footer {
  min-height: 282px;
}

.token-panel.chart-panel,
.distribution-panel.chart-panel {
  overflow: hidden;
}

.token-panel.chart-panel.has-chart-footer :deep(.chart-body),
.token-panel.chart-panel.has-chart-footer :deep(.chart-surface),
.token-panel.chart-panel.has-chart-footer :deep(.chart-empty) {
  height: 154px;
}

.distribution-panel.chart-panel,
.distribution-panel.chart-panel.has-chart-footer,
.distribution-panel.chart-panel.has-chart-footer.has-compact-footer {
  min-height: var(--dashboard-card-height);
}

.distribution-panel.chart-panel.has-chart-footer :deep(.chart-body),
.distribution-panel.chart-panel.has-chart-footer :deep(.chart-surface),
.distribution-panel.chart-panel.has-chart-footer :deep(.chart-empty) {
  height: 146px;
}

.token-panel.chart-panel :deep(.chart-heading),
.distribution-panel.chart-panel :deep(.chart-heading) {
  padding: 14px 16px 10px;
}

.token-panel.chart-panel :deep(.chart-footer),
.distribution-panel.chart-panel :deep(.chart-footer) {
  padding: 0 16px 14px;
}

.heatmap-panel {
  min-height: var(--dashboard-card-height);
  overflow: hidden;
}

.heatmap-panel .compact-panel-inner {
  grid-template-rows: auto minmax(0, 1fr) auto;
  min-height: 0;
  overflow: hidden;
}

.anomaly-panel {
  min-height: var(--dashboard-card-height);
  overflow: hidden;
}

.ranking-panel {
  min-height: var(--dashboard-card-height);
  overflow: hidden;
}

.anomaly-panel .compact-panel-inner {
  grid-template-rows: auto auto auto minmax(0, 1fr);
  min-height: 0;
  overflow: hidden;
}

.ranking-panel .compact-panel-inner {
  grid-template-rows: auto minmax(0, 1fr);
  min-height: 0;
  overflow: hidden;
}

.compact-panel-inner {
  display: grid;
  align-content: start;
  gap: 10px;
  height: 100%;
  padding: 14px;
}

.panel-heading-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-width: 0;
}

.panel-heading-row .section-title {
  min-width: 0;
  margin: 0;
}

.panel-subtle-text {
  flex: 0 0 auto;
  color: var(--cpa-text-muted);
  font-size: 12px;
  white-space: nowrap;
}

.anomaly-stat-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 7px;
}

.anomaly-stat {
  display: grid;
  gap: 2px;
  min-width: 0;
  padding: 8px 10px;
  border: 1px solid var(--cpa-border);
  border-radius: var(--cpa-radius-sm);
  background: var(--cpa-surface-muted);
}

.anomaly-stat span {
  color: var(--cpa-text-muted);
  font-size: 12px;
  line-height: 1.2;
}

.anomaly-stat strong {
  min-width: 0;
  overflow-wrap: anywhere;
  color: var(--cpa-text-strong);
  font-size: 17px;
  font-weight: 760;
  line-height: 1.18;
}

.anomaly-stat.is-danger strong {
  color: var(--cpa-danger);
}

.anomaly-stat.is-warning strong {
  color: var(--cpa-warning);
}

.anomaly-stat.is-success strong {
  color: var(--cpa-success);
}

.top-failed-endpoint {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 10px;
  align-items: center;
  min-width: 0;
  padding: 8px 10px;
  border: 1px solid color-mix(in srgb, var(--cpa-danger) 18%, var(--cpa-border));
  border-radius: var(--cpa-radius-sm);
  background: color-mix(in srgb, var(--cpa-danger-weak) 56%, var(--cpa-surface));
}

.top-failed-endpoint span {
  color: var(--cpa-text-muted);
  font-size: 12px;
  white-space: nowrap;
}

.top-failed-endpoint strong {
  min-width: 0;
  overflow: hidden;
  color: var(--cpa-text-strong);
  font-size: 13px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.recent-failed-list,
.ranking-list {
  display: grid;
  align-content: start;
  gap: 6px;
  min-width: 0;
}

.recent-failed-list,
.ranking-list {
  min-height: 0;
  overflow-y: auto;
  padding-right: 2px;
  scrollbar-color: color-mix(in srgb, var(--cpa-text-muted) 34%, transparent) transparent;
  scrollbar-gutter: stable;
  scrollbar-width: thin;
}

.recent-failed-list::-webkit-scrollbar,
.ranking-list::-webkit-scrollbar,
.distribution-legend::-webkit-scrollbar {
  width: 6px;
  height: 6px;
}

.recent-failed-list::-webkit-scrollbar-track,
.recent-failed-list::-webkit-scrollbar-corner,
.ranking-list::-webkit-scrollbar-track,
.ranking-list::-webkit-scrollbar-corner,
.distribution-legend::-webkit-scrollbar-track,
.distribution-legend::-webkit-scrollbar-corner {
  background: transparent;
}

.recent-failed-list::-webkit-scrollbar-thumb,
.ranking-list::-webkit-scrollbar-thumb,
.distribution-legend::-webkit-scrollbar-thumb {
  border-radius: 999px;
  background: color-mix(in srgb, var(--cpa-text-muted) 30%, transparent);
}

.recent-failed-list::-webkit-scrollbar-thumb:hover,
.ranking-list::-webkit-scrollbar-thumb:hover,
.distribution-legend::-webkit-scrollbar-thumb:hover {
  background: color-mix(in srgb, var(--cpa-text-muted) 48%, transparent);
}

.recent-failed-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto;
  gap: 8px;
  align-items: center;
  width: 100%;
  min-width: 0;
  min-height: 32px;
  padding: 5px 8px;
  border: 1px solid var(--cpa-border);
  border-radius: var(--cpa-radius-sm);
  background: var(--cpa-surface-raised);
  color: inherit;
  cursor: pointer;
  font: inherit;
  text-align: left;
}

.recent-failed-row:hover {
  border-color: color-mix(in srgb, var(--cpa-danger) 22%, var(--cpa-border));
  background: color-mix(in srgb, var(--cpa-danger-weak) 42%, var(--cpa-surface));
}

.recent-failed-row span,
.recent-failed-row em {
  min-width: 0;
  overflow: hidden;
  color: var(--cpa-text-muted);
  font-size: 12px;
  font-style: normal;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.recent-failed-row strong {
  color: var(--cpa-danger);
  font-size: 12px;
  font-weight: 760;
  white-space: nowrap;
}

.heatmap-groups {
  display: grid;
  align-content: stretch;
  grid-template-rows: repeat(2, minmax(0, 1fr));
  gap: 10px;
  min-width: 0;
  min-height: 0;
}

.heatmap-group {
  display: grid;
  grid-template-columns: minmax(70px, 84px) minmax(0, 1fr);
  gap: 10px;
  align-items: center;
  min-width: 0;
  min-height: 0;
  padding: 8px;
  border: 1px solid color-mix(in srgb, var(--cpa-border) 72%, transparent);
  border-radius: var(--cpa-radius-sm);
  background: color-mix(in srgb, var(--cpa-surface-muted) 72%, transparent);
}

.heatmap-group.is-records {
  --heat-color-start: var(--cpa-accent-blue);
  --heat-color-end: var(--cpa-primary);
}

.heatmap-group.is-tokens {
  --heat-color-start: var(--cpa-primary);
  --heat-color-end: var(--cpa-chart-3, #7e66f2);
}

.heatmap-group-heading {
  display: grid;
  gap: 5px;
  align-content: center;
  min-width: 0;
  color: var(--cpa-text-muted);
  font-size: 12px;
  line-height: 1.2;
}

.heatmap-group-heading span {
  display: inline-flex;
  gap: 6px;
  align-items: center;
}

.heatmap-group-heading span::before {
  width: 8px;
  height: 8px;
  flex: 0 0 auto;
  border-radius: 3px;
  background: var(--heat-color-end);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--heat-color-end) 14%, transparent);
  content: "";
}

.heatmap-group-heading span,
.heatmap-group-heading strong {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.heatmap-group-heading strong {
  color: var(--cpa-text-strong);
  font-variant-numeric: tabular-nums;
  font-weight: 760;
}

.hour-heatmap {
  display: grid;
  grid-template-columns: repeat(12, minmax(0, 1fr));
  gap: 5px;
  min-width: 0;
}

.hour-heatmap.is-records {
  --heat-color-start: var(--cpa-accent-blue);
  --heat-color-end: var(--cpa-primary);
}

.hour-heatmap.is-tokens {
  --heat-color-start: var(--cpa-primary);
  --heat-color-end: var(--cpa-chart-3, #7e66f2);
}

.hour-cell {
  display: grid;
  position: relative;
  min-width: 0;
  min-height: 28px;
  overflow: hidden;
  place-items: center;
  border: 1px solid color-mix(in srgb, var(--heat-color-end) 14%, var(--cpa-border));
  border-radius: 5px;
  background: var(--cpa-surface-muted);
  color: var(--cpa-text-strong);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  font-weight: 700;
  line-height: 1;
}

.hour-cell::before {
  position: absolute;
  inset: 0;
  border-radius: inherit;
  background: linear-gradient(180deg, var(--heat-color-start), var(--heat-color-end));
  content: "";
  opacity: var(--heat-intensity);
}

.hour-cell span {
  position: relative;
  z-index: 1;
}

.hour-cell.is-empty {
  border-color: color-mix(in srgb, var(--cpa-border) 70%, transparent);
  background: var(--cpa-surface-muted);
  color: var(--cpa-text-muted);
}

.heatmap-scale {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  gap: 8px;
  align-items: center;
  color: var(--cpa-text-muted);
  font-size: 12px;
}

.heatmap-scale-bars {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px;
}

.heatmap-scale-bars i {
  display: block;
  height: 6px;
  border-radius: 999px;
}

.heatmap-scale-bars .is-records {
  background: linear-gradient(90deg, var(--cpa-surface-muted), var(--cpa-accent-blue));
}

.heatmap-scale-bars .is-tokens {
  background: linear-gradient(90deg, var(--cpa-surface-muted), var(--cpa-chart-3, #7e66f2));
}

.ranking-row {
  display: grid;
  grid-template-columns: 24px minmax(0, 1fr) auto auto;
  gap: 8px;
  align-items: center;
  min-width: 0;
  min-height: 43px;
  padding: 6px 8px;
  border: 1px solid var(--cpa-border);
  border-radius: var(--cpa-radius-sm);
  background: var(--cpa-surface-raised);
}

.ranking-index {
  display: grid;
  width: 24px;
  height: 24px;
  place-items: center;
  border-radius: 6px;
  background: var(--cpa-primary-wash);
  color: var(--cpa-primary);
  font-size: 12px;
  font-weight: 760;
}

.ranking-main {
  display: grid;
  min-width: 0;
  gap: 4px;
}

.ranking-label-line {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 10px;
  align-items: center;
  min-width: 0;
}

.ranking-label-line strong {
  min-width: 0;
  overflow: hidden;
  color: var(--cpa-text-strong);
  font-size: 13px;
  font-weight: 760;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ranking-label-line span {
  color: var(--cpa-text-muted);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.ranking-track {
  height: 6px;
  overflow: hidden;
  border-radius: 999px;
  background: var(--cpa-surface-muted);
}

.ranking-track span {
  display: block;
  width: var(--ranking-width);
  height: 100%;
  border-radius: inherit;
  background: linear-gradient(90deg, var(--cpa-primary), var(--cpa-accent-blue));
}

.ranking-values {
  display: grid;
  gap: 1px;
  min-width: 72px;
  text-align: right;
}

.ranking-values strong {
  color: var(--cpa-text-strong);
  font-size: 13px;
  font-weight: 760;
}

.ranking-values span {
  color: var(--cpa-text-muted);
  font-size: 11px;
  white-space: nowrap;
}

.distribution-legend {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(168px, 1fr));
  gap: 5px 7px;
  max-height: 74px;
  margin: 0;
  overflow-x: hidden;
  overflow-y: auto;
  padding: 0;
  padding-right: 2px;
  list-style: none;
  scrollbar-color: color-mix(in srgb, var(--cpa-text-muted) 34%, transparent) transparent;
  scrollbar-gutter: stable;
  scrollbar-width: thin;
}

.distribution-legend.is-single {
  grid-template-columns: minmax(0, 300px);
  justify-content: center;
  max-height: none;
  overflow: visible;
}

.distribution-legend-item {
  display: grid;
  grid-template-columns: 10px minmax(0, 1fr) auto auto;
  gap: 8px;
  align-items: center;
  min-width: 0;
  padding: 4px 7px;
  border: 1px solid color-mix(in srgb, var(--cpa-border) 68%, transparent);
  border-radius: 6px;
  background: color-mix(in srgb, var(--cpa-surface-muted) 72%, transparent);
}

.distribution-marker {
  width: 9px;
  height: 9px;
  border-radius: 3px;
  background: var(--distribution-color);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--distribution-color) 14%, transparent);
}

.distribution-label {
  min-width: 0;
  overflow: hidden;
  color: var(--cpa-text);
  font-size: 12px;
  line-height: 18px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.distribution-count {
  color: var(--cpa-text-strong);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  font-weight: 750;
  line-height: 18px;
}

.distribution-percent {
  min-width: 36px;
  color: var(--cpa-text-muted);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  line-height: 18px;
  text-align: right;
}

.empty-inline {
  display: grid;
  min-height: 48px;
  place-items: center;
  border: 1px dashed var(--cpa-border);
  border-radius: var(--cpa-radius-sm);
  color: var(--cpa-text-muted);
  font-size: 12px;
}

@media (max-width: 1680px) {
  .dashboard-columns {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}

@media (max-width: 1320px) {
  .dashboard-top-grid {
    grid-template-columns: minmax(0, 1.45fr) minmax(320px, 0.75fr);
  }

  .dashboard-columns {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .field-row,
  .field-row.is-account-scope {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}

@media (max-width: 980px) {
  .dashboard-layout {
    grid-template-columns: minmax(0, 1fr);
    gap: 10px;
  }

  .dashboard-top-grid,
  .dashboard-columns,
  .dashboard-column {
    display: contents;
  }

  .area-trend {
    order: 1;
  }

  .area-anomaly {
    order: 2;
  }

  .area-token {
    order: 3;
  }

  .area-heatmap {
    order: 4;
  }

  .area-primary-ranking {
    order: 5;
  }

  .area-model-ranking {
    order: 6;
  }

  .area-provider {
    order: 7;
  }

  .area-endpoint {
    order: 8;
  }

  .area-anomaly,
  .area-heatmap,
  .area-primary-ranking,
  .area-model-ranking,
  .area-provider,
  .area-endpoint {
    height: auto;
  }

  .usage-trend-panel.chart-panel {
    min-height: 280px;
  }

  .token-panel.chart-panel,
  .token-panel.chart-panel.has-chart-footer,
  .token-panel.chart-panel.has-chart-footer.has-compact-footer {
    min-height: 252px;
  }

  .distribution-panel.chart-panel,
  .distribution-panel.chart-panel.has-chart-footer,
  .distribution-panel.chart-panel.has-chart-footer.has-compact-footer {
    min-height: 236px;
  }

  .usage-trend-panel.chart-panel :deep(.chart-body),
  .usage-trend-panel.chart-panel :deep(.chart-surface),
  .usage-trend-panel.chart-panel :deep(.chart-empty) {
    height: 224px;
  }

  .token-panel.chart-panel.has-chart-footer :deep(.chart-body),
  .token-panel.chart-panel.has-chart-footer :deep(.chart-surface),
  .token-panel.chart-panel.has-chart-footer :deep(.chart-empty) {
    height: 136px;
  }

  .time-row {
    grid-template-columns: 1fr;
  }

  .hour-heatmap {
    grid-template-columns: repeat(12, minmax(0, 1fr));
  }
}

@media (max-width: 720px) {
  .usage-dashboard-page {
    gap: 10px;
  }

  .filter-summary {
    display: flex;
  }

  .filter-panel:not(.is-expanded) .filter-toolbar {
    display: none;
  }

  .filter-toolbar {
    gap: 8px;
    padding: 10px;
  }

  .field-row,
  .field-row.is-account-scope {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 8px;
  }

  .quick-ranges {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .quick-range-button {
    min-width: 0;
  }

  .status-actions {
    display: grid;
    grid-column: 1 / -1;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: stretch;
  }

  .status-actions :deep(.n-select) {
    min-width: 0;
  }

  .header-actions {
    width: 100%;
    align-items: flex-start;
    justify-content: space-between;
  }

  .quota-status-pill {
    max-width: 100%;
  }

  .refresh-status {
    white-space: normal;
  }

  .dashboard-metric-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .dashboard-metric-card {
    min-height: 90px;
  }

  .compact-panel-inner {
    padding: 12px;
  }

  .panel-heading-row {
    align-items: flex-start;
  }

  .panel-subtle-text {
    white-space: normal;
  }

  .anomaly-stat-grid {
    grid-template-columns: 1fr;
  }

  .top-failed-endpoint {
    grid-template-columns: 1fr;
    gap: 4px;
  }

  .recent-failed-row {
    grid-template-columns: minmax(0, 1fr) auto;
  }

  .recent-failed-row em {
    grid-column: 1 / -1;
  }

  .heatmap-groups {
    grid-template-rows: none;
  }

  .heatmap-group {
    grid-template-columns: 1fr;
    gap: 7px;
  }

  .heatmap-group-heading {
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
  }

  .hour-heatmap {
    grid-template-columns: repeat(6, minmax(0, 1fr));
  }

  .hour-cell {
    min-height: 24px;
  }

  .ranking-row {
    grid-template-columns: 22px minmax(0, 1fr) auto;
    gap: 8px;
  }

  .ranking-row :deep(.n-button) {
    grid-column: 2 / -1;
    justify-self: end;
  }

  .ranking-values {
    min-width: 58px;
  }

  .distribution-legend,
  .token-legend {
    grid-template-columns: 1fr;
    max-height: none;
    overflow: visible;
  }
}
</style>
