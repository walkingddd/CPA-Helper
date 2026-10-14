<script setup lang="ts">
import { computed, h, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  NButton,
  NDataTable,
  NDatePicker,
  NDrawer,
  NDrawerContent,
  NPagination,
  NSelect,
  NTag,
  useMessage,
  type DataTableColumns,
} from 'naive-ui'

import { getUsageOptions, getUsageRecord, getUsageRecords } from '@/features/usage/api/usageApi'
import type {
  RankingItem,
  UsageFilters,
  UsageOptionsResponse,
  UsageRecordDetail,
  UsageRecordListItem,
} from '@/shared/types/api'
import {
  formatDateTime,
  formatInteger,
  formatLocalDateTimeParam,
  formatUsd,
  jsonPretty,
} from '@/shared/utils/format'
import { useI18n } from '@/shared/i18n'

type FailedFilter = 'all' | 'success' | 'failed'
type QuickRangeKey = 'today' | 'last24h' | 'last3d' | 'last7d' | 'all'
type UsageScope = 'admin' | 'account'
type RecordsTableLayoutProps =
  | { flexHeight: true }
  | { flexHeight: false; maxHeight: string }

interface RefreshOptions {
  resetPage?: boolean
  silent?: boolean
}

interface Props {
  scope: UsageScope
}

const AUTO_REFRESH_INTERVAL_MS = 5000
const HOUR_MS = 60 * 60 * 1000
const DAY_MS = 24 * HOUR_MS
const ALL_RECORDS_START_PARAM = '0001-01-01T00:00:00+08:00'
const ALL_RECORDS_END_PARAM = '9999-12-31T23:59:59+08:00'
const RECORDS_TABLE_MIN_ROW_HEIGHT = 40
const RECORDS_TABLE_COLUMN_WIDTHS = {
  timestamp: 150,
  user: 132,
  apiKeyDescription: 118,
  model: 132,
  source: 190,
  failed: 68,
  ttft: 110,
  latency: 110,
  inputTokens: 100,
  outputTokens: 145,
  reasoningTokens: 100,
  cachedTokens: 160,
  totalTokens: 120,
  estimatedCost: 110,
  provider: 120,
  endpoint: 190,
  requestId: 150,
  actions: 86,
} as const
const ADMIN_RECORDS_TABLE_SCROLL_X = Object.values(RECORDS_TABLE_COLUMN_WIDTHS).reduce(
  (total, width) => total + width,
  0,
)
const ACCOUNT_RECORDS_TABLE_SCROLL_X =
  ADMIN_RECORDS_TABLE_SCROLL_X -
  RECORDS_TABLE_COLUMN_WIDTHS.user -
  RECORDS_TABLE_COLUMN_WIDTHS.source
const RECORDS_TABLE_FALLBACK_MAX_HEIGHT = 'max(320px, calc(100dvh - 318px))'
const desktopRecordsLayoutQuery = window.matchMedia('(min-width: 861px)')

const route = useRoute()
const router = useRouter()
const message = useMessage()
const props = defineProps<Props>()
const { currentLanguage, errorText, t } = useI18n()
const isLoading = ref(false)
const isAutoRefreshing = ref(false)
const autoRefreshError = ref<string | null>(null)
const lastRefreshedAt = ref<Date | null>(null)
const drawerOpen = ref(false)
const selectedRecord = ref<UsageRecordDetail | null>(null)
const records = ref<UsageRecordListItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(50)
const isDesktopRecordsLayout = ref(desktopRecordsLayoutQuery.matches)
const options = ref<UsageOptionsResponse>({
  users: [],
  api_key_descriptions: [],
  providers: [],
  models: [],
  sources: [],
  endpoints: [],
})

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

function isTodayRange(range: [number, number] | null): boolean {
  if (!range) {
    return false
  }
  const [todayStart, tomorrowStart] = todayRange()
  return range[0] === todayStart && range[1] === tomorrowStart
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

const initialIsAllRange = route.query.range === 'all'
const initialDateRange = initialIsAllRange ? null : initialRange()
const dateRange = ref<[number, number] | null>(initialDateRange)
const activeQuickRange = ref<QuickRangeKey | null>(
  initialIsAllRange
    ? 'all'
    : initialDateRange === null || isTodayRange(initialDateRange)
      ? 'today'
      : null,
)
const filterForm = reactive({
  user_id: numberFromQuery(route.query.user_id),
  api_key_description:
    typeof route.query.api_key_description === 'string' ? route.query.api_key_description : null,
  provider: typeof route.query.provider === 'string' ? route.query.provider : null,
  model: typeof route.query.model === 'string' ? route.query.model : null,
  source_key: typeof route.query.source_key === 'string' ? route.query.source_key : null,
  endpoint: typeof route.query.endpoint === 'string' ? route.query.endpoint : null,
  failed: failedFromQuery(),
})

const quickRangeOptions = computed<Array<{ key: QuickRangeKey; label: string }>>(() => [
  { key: 'today', label: t('今日', 'Today') },
  { key: 'last24h', label: t('近24小时', 'Last 24 hours') },
  { key: 'last3d', label: t('近3日', 'Last 3 days') },
  { key: 'last7d', label: t('近7日', 'Last 7 days') },
  { key: 'all', label: t('全部', 'All') },
])

const failedFilterOptions = computed(() => [
  { label: t('全部', 'All'), value: 'all' },
  { label: t('成功', 'Success'), value: 'success' },
  { label: t('失败', 'Failed'), value: 'failed' },
])

function apiKeyFilterLabel(item: UsageOptionsResponse['api_key_descriptions'][number]): string {
  return item.label?.trim() || item.key
}

function emptyRankingItem(
  key: string,
  label: string,
  extra: Partial<Pick<RankingItem, 'api_key_description' | 'user_id'>> = {},
): RankingItem {
  return {
    key,
    label,
    records: 0,
    failed_records: 0,
    total_tokens: 0,
    estimated_cost_usd: 0,
    user_id: null,
    api_key_description: null,
    ...extra,
  }
}

function fallbackOptionsFromRecords(items: UsageRecordListItem[]): UsageOptionsResponse {
  const users = new Map<number, RankingItem>()
  const apiKeyDescriptions = new Map<string, RankingItem>()
  const providers = new Set<string>()
  const models = new Set<string>()
  const endpoints = new Set<string>()

  items.forEach((item) => {
    if (item.user_id !== null && !users.has(item.user_id)) {
      users.set(
        item.user_id,
        emptyRankingItem(String(item.user_id), userLabel(item.user_label), { user_id: item.user_id }),
      )
    }
    const description = item.api_key_description?.trim()
    if (description && !apiKeyDescriptions.has(description)) {
      apiKeyDescriptions.set(
        description,
        emptyRankingItem(description, description, { api_key_description: description }),
      )
    }
    if (item.provider) {
      providers.add(item.provider)
    }
    if (item.model) {
      models.add(item.model)
    }
    if (item.endpoint) {
      endpoints.add(item.endpoint)
    }
  })

  return {
    users: [...users.values()],
    api_key_descriptions: [...apiKeyDescriptions.values()],
    providers: [...providers].sort(),
    models: [...models].sort(),
    sources: [],
    endpoints: [...endpoints].sort(),
  }
}

function normalizeUsageOptions(
  nextOptions: Partial<UsageOptionsResponse> | null | undefined,
  fallbackRecords: UsageRecordListItem[],
): UsageOptionsResponse {
  const fallback = fallbackOptionsFromRecords(fallbackRecords)
  return {
    users: nextOptions?.users?.length ? nextOptions.users : fallback.users,
    api_key_descriptions: nextOptions?.api_key_descriptions?.length
      ? nextOptions.api_key_descriptions
      : fallback.api_key_descriptions,
    providers: nextOptions?.providers?.length ? nextOptions.providers : fallback.providers,
    models: nextOptions?.models?.length ? nextOptions.models : fallback.models,
    sources: nextOptions?.sources ?? fallback.sources,
    endpoints: nextOptions?.endpoints?.length ? nextOptions.endpoints : fallback.endpoints,
  }
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
  sources: options.value.sources.map((item) => ({ label: item.label, value: item.key })),
  endpoints: options.value.endpoints.map((item) => ({ label: item, value: item })),
}))

const isAccountScope = computed(() => props.scope === 'account')
const pageTitle = computed(() =>
  isAccountScope.value ? t('我的明细', 'My records') : t('请求明细', 'Request records'),
)
const pageSubtitle = computed(() =>
  isAccountScope.value
    ? t('仅查询当前登录账号自己的本地用量记录', 'Only local usage records for your account are shown')
    : t(
        '分页查询本地用量记录，单条原始数据已在接口层脱敏',
        'Browse local usage records by page. Raw record data is masked by the API.',
      ),
)
const recordsTableScrollX = computed(() =>
  isAccountScope.value ? ACCOUNT_RECORDS_TABLE_SCROLL_X : ADMIN_RECORDS_TABLE_SCROLL_X,
)
const recordsTableLayoutProps = computed<RecordsTableLayoutProps>(() =>
  isDesktopRecordsLayout.value
    ? { flexHeight: true }
    : { flexHeight: false, maxHeight: RECORDS_TABLE_FALLBACK_MAX_HEIGHT },
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
  return t(`每 5 秒自动刷新 · 最近 ${lastRefreshText}`, `Auto refresh every 5 seconds · latest ${lastRefreshText}`)
})

function buildFilters(): UsageFilters {
  const failed =
    filterForm.failed === 'all' ? undefined : filterForm.failed === 'failed' ? true : false
  const start =
    activeQuickRange.value === 'all'
      ? ALL_RECORDS_START_PARAM
      : dateRange.value
        ? formatLocalDateTimeParam(dateRange.value[0])
        : undefined
  const end =
    activeQuickRange.value === 'all'
      ? ALL_RECORDS_END_PARAM
      : dateRange.value
        ? formatLocalDateTimeParam(dateRange.value[1])
        : undefined
  return {
    scope: props.scope,
    start,
    end,
    user_id: isAccountScope.value ? undefined : (filterForm.user_id ?? undefined),
    api_key_description: filterForm.api_key_description ?? undefined,
    provider: filterForm.provider ?? undefined,
    model: filterForm.model ?? undefined,
    source_key: isAccountScope.value ? undefined : (filterForm.source_key ?? undefined),
    endpoint: filterForm.endpoint ?? undefined,
    failed,
  }
}

function filtersToQuery(
  filters: UsageFilters,
  rangeKey: QuickRangeKey | null = null,
): Record<string, string> {
  const query: Record<string, string> = {}
  Object.entries(filters).forEach(([key, value]) => {
    if (key !== 'scope' && value !== undefined && value !== '') {
      query[key] = String(value)
    }
  })
  if (rangeKey === 'all') {
    delete query.start
    delete query.end
    query.range = 'all'
  }
  return query
}

function buildQuickRange(key: QuickRangeKey): [number, number] | null {
  switch (key) {
    case 'today':
      return todayRange()
    case 'last24h': {
      const end = Date.now()
      return [end - 24 * HOUR_MS, end]
    }
    case 'last3d': {
      const end = Date.now()
      return [end - 3 * DAY_MS, end]
    }
    case 'last7d': {
      const end = Date.now()
      return [end - 7 * DAY_MS, end]
    }
    case 'all':
      return null
  }
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
  void refresh({ resetPage: true })
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

function handleSourceChange(value: unknown) {
  filterForm.source_key = normalizeSelectValue(value)
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
  await refresh({ resetPage: true })
}

let queuedRefresh: RefreshOptions | null = null

function queueRefresh(options: RefreshOptions) {
  if (options.silent) {
    return
  }
  queuedRefresh = {
    resetPage: Boolean(queuedRefresh?.resetPage || options.resetPage),
    silent: false,
  }
}

async function refresh({ resetPage = false, silent = false }: RefreshOptions = {}) {
  if (isLoading.value || isAutoRefreshing.value) {
    queueRefresh({ resetPage, silent })
    return
  }
  if (resetPage) {
    page.value = 1
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
    const [recordsResult, optionsResult] = await Promise.allSettled([
      getUsageRecords(filters, page.value, pageSize.value),
      silent ? Promise.resolve(null) : getUsageOptions(filters),
    ])
    if (recordsResult.status === 'rejected') {
      throw recordsResult.reason
    }
    const nextRecords = recordsResult.value
    const nextOptions = optionsResult.status === 'fulfilled' ? optionsResult.value : null
    records.value = nextRecords.items
    total.value = nextRecords.total
    if (!silent) {
      options.value = normalizeUsageOptions(nextOptions, nextRecords.items)
    }
    if (usedServerDefaultRange) {
      dateRange.value = [new Date(nextRecords.start).getTime(), new Date(nextRecords.end).getTime()]
    }
    void router.replace({
      query: filtersToQuery(
        usedServerDefaultRange
          ? { ...filters, start: nextRecords.start, end: nextRecords.end }
          : filters,
        activeQuickRange.value,
      ),
    })
    autoRefreshError.value = null
    lastRefreshedAt.value = new Date()
  } catch (error) {
    const errorMessage = errorText(error, '加载明细失败', 'Failed to load records')
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

async function openRecord(record: UsageRecordListItem) {
  try {
    selectedRecord.value = await getUsageRecord(record.id, props.scope)
    drawerOpen.value = true
  } catch (error) {
    message.error(errorText(error, '加载原始数据失败', 'Failed to load raw data'))
  }
}

function textOrDash(value: string | null | undefined): string {
  const normalized = value?.trim()
  return normalized || '-'
}

function userLabel(value: string | null | undefined): string {
  const normalized = value?.trim()
  if (!normalized || normalized === '未绑定') {
    return t('未知', 'Unknown')
  }
  return normalized
}

function userLabelColorKey(row: UsageRecordListItem): string | null {
  const normalized = row.user_label?.trim()
  if (!normalized || normalized === '未绑定') {
    return null
  }
  if (row.user_id !== null) {
    return `user:${row.user_id}`
  }
  return `label:${normalized}`
}

function hashString(value: string): number {
  let hash = 2166136261
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index)
    hash = Math.imul(hash, 16777619)
  }
  return hash >>> 0
}

function userLabelChipStyle(colorKey: string): Record<string, string> {
  return {
    '--user-label-hue': `${hashString(colorKey) % 360}deg`,
  }
}

function renderUserLabel(row: UsageRecordListItem) {
  const label = userLabel(row.user_label)
  const colorKey = userLabelColorKey(row)
  return h(
    'span',
    {
      class: ['user-label-chip', { 'is-neutral': colorKey === null }],
      style: colorKey ? userLabelChipStyle(colorKey) : undefined,
      title: label,
    },
    label,
  )
}

function apiKeyDescriptionLabel(value: string | null | undefined): string {
  const normalized = value?.trim()
  return normalized || t('未知', 'Unknown')
}

function formatLatency(value: number | null): string {
  if (value === null) {
    return '-'
  }
  if (value >= 1000) {
    return `${+(value / 1000).toFixed(2)} s`
  }
  return `${formatInteger(Math.round(value))} ms`
}

function formatPositiveLatency(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value) || value <= 0) {
    return '-'
  }
  return formatLatency(value)
}

function formatModelWithReasoning(
  record: Pick<UsageRecordListItem, 'model' | 'reasoning_effort'>,
): string {
  const model = textOrDash(record.model)
  if (model === '-') {
    return model
  }
  const reasoningEffort = record.reasoning_effort?.trim()
  return reasoningEffort ? `${model} ${reasoningEffort}` : model
}

function formatOutputTps(row: Pick<UsageRecordListItem, 'latency_ms' | 'output_tokens'>): string {
  if (row.latency_ms === null || row.latency_ms <= 0) {
    return '-'
  }
  const tokensPerSecond = (row.output_tokens / row.latency_ms) * 1000
  return new Intl.NumberFormat(currentLanguage.value === 'zh' ? 'zh-CN' : 'en-US', {
    maximumFractionDigits: tokensPerSecond < 10 ? 2 : 1,
  }).format(tokensPerSecond)
}

function formatOutputWithTps(row: Pick<UsageRecordListItem, 'latency_ms' | 'output_tokens'>): string {
  const output = formatInteger(row.output_tokens)
  const outputTps = formatOutputTps(row)
  return outputTps === '-' ? output : `${output} (${outputTps} tps)`
}

function renderOutputWithTps(row: Pick<UsageRecordListItem, 'latency_ms' | 'output_tokens'>) {
  const output = formatInteger(row.output_tokens)
  const outputTps = formatOutputTps(row)
  if (outputTps === '-') {
    return output
  }
  return h(
    'span',
    {
      class: 'output-with-tps',
      style: { whiteSpace: 'nowrap' },
    },
    [
      output,
      h(
        'span',
        {
          class: 'output-tps-muted',
          style: {
            color: 'var(--cpa-text-muted)',
            fontSize: '11px',
            fontWeight: '400',
            lineHeight: '1',
          },
        },
        ` (${outputTps} tps)`,
      ),
    ],
  )
}

function isClaudeProvider(provider: string | null | undefined): boolean {
  const normalized = provider?.trim().toLowerCase()
  return normalized === 'claude' || normalized === 'anthropic'
}

function formatCacheTokens(row: UsageRecordListItem): string {
  if (isClaudeProvider(row.provider)) {
    return t(
      `(读 ${formatInteger(row.cache_read_tokens)} / 写 ${formatInteger(row.cache_creation_tokens)})`,
      `(read ${formatInteger(row.cache_read_tokens)} / write ${formatInteger(row.cache_creation_tokens)})`,
    )
  }
  return formatInteger(row.cached_tokens)
}

function recordRowKey(row: UsageRecordListItem): number {
  return row.id
}

function handleRecordsLayoutChange(event: MediaQueryListEvent) {
  isDesktopRecordsLayout.value = event.matches
}

const detailRows = computed(() => {
  const record = selectedRecord.value
  if (!record) {
    return []
  }
  const rows = [
    { label: t('时间', 'Time'), value: formatDateTime(record.timestamp) },
    { label: t('模型', 'Model'), value: formatModelWithReasoning(record) },
    { label: t('服务商', 'Provider'), value: textOrDash(record.provider) },
    { label: t('接口', 'Endpoint'), value: textOrDash(record.endpoint) },
    { label: t('API KEY 描述', 'API key description'), value: apiKeyDescriptionLabel(record.api_key_description) },
    { label: t('认证类型', 'Auth type'), value: textOrDash(record.auth) },
    { label: t('请求 ID', 'Request ID'), value: textOrDash(record.request_id) },
    { label: t('结果', 'Result'), value: record.failed ? t('失败', 'Failed') : t('成功', 'Success') },
    { label: t('首字耗时', 'TTFT'), value: formatPositiveLatency(record.ttft_ms) },
    { label: t('总耗时', 'Latency'), value: formatLatency(record.latency_ms) },
    { label: t('输入 Token', 'Input tokens'), value: formatInteger(record.input_tokens) },
    { label: t('缓存 Token', 'Cached tokens'), value: formatInteger(record.cached_tokens) },
    { label: t('缓存读 Token', 'Cache read tokens'), value: formatInteger(record.cache_read_tokens) },
    { label: t('缓存写 Token', 'Cache write tokens'), value: formatInteger(record.cache_creation_tokens) },
    { label: t('输出 Token', 'Output tokens'), value: formatOutputWithTps(record) },
    { label: t('思考 Token', 'Reasoning tokens'), value: formatInteger(record.reasoning_tokens) },
    { label: t('总 Token', 'Total tokens'), value: formatInteger(record.total_tokens) },
    { label: t('费用', 'Cost'), value: formatUsd(record.estimated_cost_usd) },
  ]
  if (!isAccountScope.value) {
    rows.splice(
      4,
      0,
      { label: t('来源', 'Source'), value: textOrDash(record.source) },
      { label: t('用户昵称', 'User nickname'), value: userLabel(record.user_label) },
    )
  }
  return rows
})

const columns = computed<DataTableColumns<UsageRecordListItem>>(() => [
  {
    title: t('时间', 'Time'),
    key: 'timestamp',
    width: RECORDS_TABLE_COLUMN_WIDTHS.timestamp,
    render: (row) => formatDateTime(row.timestamp),
  },
  ...(isAccountScope.value
    ? []
    : [
        {
          title: t('用户昵称', 'User nickname'),
          key: 'user_label',
          width: RECORDS_TABLE_COLUMN_WIDTHS.user,
          ellipsis: { tooltip: true },
          render: (row: UsageRecordListItem) => renderUserLabel(row),
        },
      ]),
  {
    title: t('KEY 描述', 'Key description'),
    key: 'api_key_description',
    width: RECORDS_TABLE_COLUMN_WIDTHS.apiKeyDescription,
    ellipsis: { tooltip: true },
    render: (row) => apiKeyDescriptionLabel(row.api_key_description),
  },
  {
    title: t('模型', 'Model'),
    key: 'model',
    width: RECORDS_TABLE_COLUMN_WIDTHS.model,
    ellipsis: { tooltip: true },
    render: (row) => formatModelWithReasoning(row),
  },
  ...(isAccountScope.value
    ? []
    : [
        {
          title: t('来源', 'Source'),
          key: 'source',
          width: RECORDS_TABLE_COLUMN_WIDTHS.source,
          ellipsis: { tooltip: true },
        },
      ]),
  {
    title: t('结果', 'Result'),
    key: 'failed',
    width: RECORDS_TABLE_COLUMN_WIDTHS.failed,
    render: (row) =>
      h(
        NTag,
        { type: row.failed ? 'error' : 'success', size: 'small', bordered: false },
        { default: () => (row.failed ? t('失败', 'Failed') : t('成功', 'Success')) },
      ),
  },
  {
    title: t('首字耗时', 'TTFT'),
    key: 'ttft_ms',
    width: RECORDS_TABLE_COLUMN_WIDTHS.ttft,
    render: (row) => formatPositiveLatency(row.ttft_ms),
  },
  {
    title: t('总耗时', 'Latency'),
    key: 'latency_ms',
    width: RECORDS_TABLE_COLUMN_WIDTHS.latency,
    render: (row) => formatLatency(row.latency_ms),
  },
  {
    title: t('输入', 'Input'),
    key: 'input_tokens',
    width: RECORDS_TABLE_COLUMN_WIDTHS.inputTokens,
    render: (row) => formatInteger(row.input_tokens),
  },
  {
    title: t('输出', 'Output'),
    key: 'output_tokens',
    width: RECORDS_TABLE_COLUMN_WIDTHS.outputTokens,
    render: renderOutputWithTps,
  },
  {
    title: t('思考', 'Reasoning'),
    key: 'reasoning_tokens',
    width: RECORDS_TABLE_COLUMN_WIDTHS.reasoningTokens,
    render: (row) => formatInteger(row.reasoning_tokens),
  },
  {
    title: t('缓存', 'Cache'),
    key: 'cached_tokens',
    width: RECORDS_TABLE_COLUMN_WIDTHS.cachedTokens,
    render: formatCacheTokens,
  },
  {
    title: t('总 Token', 'Total tokens'),
    key: 'total_tokens',
    width: RECORDS_TABLE_COLUMN_WIDTHS.totalTokens,
    render: (row) => formatInteger(row.total_tokens),
  },
  {
    title: t('费用', 'Cost'),
    key: 'estimated_cost_usd',
    width: RECORDS_TABLE_COLUMN_WIDTHS.estimatedCost,
    render: (row) => formatUsd(row.estimated_cost_usd),
  },
  {
    title: t('服务商', 'Provider'),
    key: 'provider',
    width: RECORDS_TABLE_COLUMN_WIDTHS.provider,
    ellipsis: { tooltip: true },
  },
  {
    title: t('接口', 'Endpoint'),
    key: 'endpoint',
    width: RECORDS_TABLE_COLUMN_WIDTHS.endpoint,
    ellipsis: { tooltip: true },
  },
  {
    title: t('请求 ID', 'Request ID'),
    key: 'request_id',
    width: RECORDS_TABLE_COLUMN_WIDTHS.requestId,
    ellipsis: { tooltip: true },
  },
  {
    title: '',
    key: 'actions',
    width: RECORDS_TABLE_COLUMN_WIDTHS.actions,
    fixed: 'right',
    render: (row) =>
      h(
        NButton,
        { size: 'small', quaternary: true, onClick: () => void openRecord(row) },
        { default: () => t('详情', 'Details') },
      ),
  },
])

let autoRefreshTimer: number | undefined

onMounted(() => {
  desktopRecordsLayoutQuery.addEventListener('change', handleRecordsLayoutChange)
  void refresh()
  autoRefreshTimer = window.setInterval(() => {
    void refresh({ silent: true })
  }, AUTO_REFRESH_INTERVAL_MS)
})

onBeforeUnmount(() => {
  desktopRecordsLayoutQuery.removeEventListener('change', handleRecordsLayoutChange)
  if (autoRefreshTimer !== undefined) {
    window.clearInterval(autoRefreshTimer)
  }
})
</script>

<template>
  <section class="page records-page">
    <div class="page-header">
      <div>
        <h1 class="page-title">{{ pageTitle }}</h1>
        <p class="page-subtitle">{{ pageSubtitle }}</p>
      </div>
      <div class="header-actions">
        <span class="refresh-status" :class="{ 'is-error': autoRefreshError }">
          {{ refreshStatusText }}
        </span>
      </div>
    </div>

    <section class="panel">
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
            v-if="!isAccountScope"
            :value="filterForm.source_key"
            :options="selectOptions.sources"
            clearable
            filterable
            :placeholder="t('来源', 'Source')"
            @update:value="handleSourceChange"
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
            <NButton secondary :loading="isLoading" @click="refresh({ resetPage: true })">
              {{ t('筛选', 'Filter') }}
            </NButton>
          </div>
        </div>
      </div>
    </section>

    <section class="panel table-panel records-table-panel">
      <NDataTable
        class="records-table"
        v-bind="recordsTableLayoutProps"
        remote
        virtual-scroll
        size="small"
        table-layout="fixed"
        :loading="isLoading"
        :columns="columns"
        :data="records"
        :min-row-height="RECORDS_TABLE_MIN_ROW_HEIGHT"
        :pagination="false"
        :row-key="recordRowKey"
        :scroll-x="recordsTableScrollX"
      />
      <div class="pagination-row">
        <NPagination
          v-model:page="page"
          v-model:page-size="pageSize"
          show-size-picker
          :page-sizes="[20, 50, 100, 200]"
          :item-count="total"
          @update:page="refresh()"
          @update:page-size="refresh({ resetPage: true })"
        />
      </div>
    </section>

    <NDrawer v-model:show="drawerOpen" placement="right" width="min(760px, 100vw)">
      <NDrawerContent :title="t('请求事件详情', 'Request event details')">
        <h3 class="drawer-section-title">{{ t('结构化信息', 'Structured information') }}</h3>
        <div class="detail-grid">
          <div v-for="row in detailRows" :key="row.label" class="detail-item">
            <div class="detail-label">{{ row.label }}</div>
            <div class="detail-value">{{ row.value }}</div>
          </div>
        </div>
        <h3 class="drawer-section-title">{{ t('原始数据', 'Raw data') }}</h3>
        <pre class="mono-json">{{ jsonPretty(selectedRecord?.raw_json ?? {}) }}</pre>
      </NDrawerContent>
    </NDrawer>
  </section>
</template>

<style scoped>
.records-table-panel,
.records-table {
  min-width: 0;
}

:global(.records-table .user-label-chip) {
  display: inline-flex;
  max-width: 100%;
  min-width: 0;
  align-items: center;
  height: 24px;
  padding: 0 8px;
  overflow: hidden;
  border: 1px solid hsl(var(--user-label-hue) 56% 64% / 42%);
  border-radius: 6px;
  background: hsl(var(--user-label-hue) 78% 93% / 92%);
  color: hsl(var(--user-label-hue) 58% 27%);
  font-size: 12px;
  font-weight: 600;
  line-height: 22px;
  text-overflow: ellipsis;
  vertical-align: middle;
  white-space: nowrap;
}

:global(.records-table .user-label-chip.is-neutral) {
  border-color: var(--cpa-border);
  background: var(--cpa-surface-muted);
  color: var(--cpa-text-muted);
}

:global(:root.dark .records-table .user-label-chip) {
  border-color: hsl(var(--user-label-hue) 48% 52% / 52%);
  background: hsl(var(--user-label-hue) 42% 24% / 76%);
  color: hsl(var(--user-label-hue) 70% 84%);
}

:global(:root.dark .records-table .user-label-chip.is-neutral) {
  border-color: var(--cpa-border-strong);
  background: var(--cpa-surface-muted);
  color: var(--cpa-text-muted);
}

.output-with-tps {
  white-space: nowrap;
}

.output-tps-muted {
  color: var(--cpa-text-muted);
  font-size: 12px;
}

.status-select {
  min-width: 96px;
}

.filter-toolbar {
  display: grid;
  gap: 12px;
  padding-block: 14px;
}

.time-row {
  display: grid;
  grid-template-columns: auto minmax(280px, 460px);
  gap: 12px;
  align-items: center;
  min-width: 0;
}

.field-row {
  display: grid;
  grid-template-columns: repeat(6, minmax(120px, 1fr)) auto;
  gap: 10px;
  align-items: end;
  min-width: 0;
}

.field-row.is-account-scope {
  grid-template-columns: repeat(4, minmax(120px, 1fr)) auto;
}

.range-picker {
  min-width: 0;
}

.quick-ranges {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  min-width: 0;
}

.quick-range-button {
  flex: 0 0 auto;
  min-width: 64px;
  border-radius: 999px;
  font-weight: 750;
}

.status-actions {
  display: flex;
  gap: 8px;
  min-width: 0;
}

.status-actions :deep(.n-button) {
  min-width: 82px;
}

.pagination-row {
  display: flex;
  justify-content: flex-end;
  padding: 12px 16px;
  border: 1px solid var(--cpa-border);
  border-top: 0;
  border-radius: 0 0 var(--cpa-radius) var(--cpa-radius);
  background: var(--cpa-surface-raised);
  box-shadow: var(--cpa-shadow-hairline);
}

.header-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
}

.refresh-status {
  color: var(--cpa-text-muted);
  font-size: 12px;
  white-space: nowrap;
}

.refresh-status.is-error {
  color: var(--cpa-danger);
}

.drawer-section-title {
  margin: 0 0 8px;
  color: var(--cpa-text);
  font-size: 14px;
  font-weight: 700;
}

.drawer-section-title:not(:first-child) {
  margin-top: 16px;
}

.detail-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}

.detail-item {
  min-width: 0;
  padding: 10px 12px;
  border: 1px solid var(--cpa-border);
  border-radius: var(--cpa-radius);
  background: var(--cpa-surface-muted);
  box-shadow: var(--cpa-shadow-hairline);
}

.detail-label {
  color: var(--cpa-text-muted);
  font-size: 12px;
}

.detail-value {
  margin-top: 3px;
  color: var(--cpa-text);
  font-weight: 600;
  overflow-wrap: anywhere;
}

.records-table :deep(.v-vl),
.records-table :deep(.n-scrollbar-container) {
  overscroll-behavior: contain;
  scrollbar-gutter: stable;
}

.records-table :deep(.n-data-table-wrapper) {
  border-radius: var(--cpa-radius) var(--cpa-radius) 0 0;
}

@media (min-width: 861px) {
  .records-page {
    grid-template-rows: auto auto minmax(0, 1fr);
    height: calc(100dvh - 60px);
    min-height: 0;
    overflow: hidden;
  }

  .records-table-panel {
    display: grid;
    grid-template-rows: minmax(0, 1fr) auto;
    min-height: 0;
  }

  .records-table {
    height: 100%;
    min-height: 0;
  }

  .records-table :deep(.n-data-table-wrapper),
  .records-table :deep(.n-data-table-base-table),
  .records-table :deep(.n-data-table-base-table-body) {
    min-height: 0;
  }
}

@media (max-width: 1180px) {
  .field-row,
  .field-row.is-account-scope {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}

@media (max-width: 720px) {
  .field-row,
  .field-row.is-account-scope {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .filter-toolbar {
    gap: 8px;
    padding-block: 10px;
  }

  .time-row {
    grid-template-columns: 1fr;
    align-items: stretch;
    gap: 8px;
  }

  .field-row,
  .field-row.is-account-scope {
    gap: 8px;
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

  .pagination-row {
    justify-content: flex-start;
    overflow-x: auto;
  }

  .header-actions {
    width: 100%;
    align-items: flex-start;
    justify-content: space-between;
  }

  .refresh-status {
    white-space: normal;
  }

  .detail-grid {
    grid-template-columns: 1fr;
  }
}
</style>
