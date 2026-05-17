<script setup lang="ts">
import { computed, h, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import {
  NButton,
  NDataTable,
  NDescriptions,
  NDescriptionsItem,
  NDrawer,
  NDrawerContent,
  NDropdown,
  NIcon,
  NInput,
  NInputNumber,
  NModal,
  NSelect,
  NSpace,
  NTag,
  useMessage,
  type DataTableColumns,
  type DataTableRowKey,
} from 'naive-ui'
import {
  Activity,
  AlertTriangle,
  ArrowLeft,
  BarChart3,
  ChevronDown,
  CircleDot,
  Gauge,
  PauseCircle,
  RefreshCw,
  Table2,
  Users,
  Zap,
} from 'lucide-vue-next'

import {
  bulkDeleteCodexKeeperAccounts,
  deleteCodexKeeperAccount,
  disableCodexKeeperAccount,
  enableCodexKeeperAccount,
  getCodexKeeperStatus,
  getCodexKeeperSettings,
  listCodexKeeperAccounts,
  refreshCodexKeeperAccounts,
  updateCodexKeeperPriority,
} from '@/features/codex-keeper/api/codexKeeperApi'
import type { CodexKeeperAccount, CodexKeeperPriorityRule } from '@/shared/types/api'
import { BEIJING_TIME_ZONE, formatDateTime, formatInteger } from '@/shared/utils/format'

type FixedPriorityFilter = 'all' | 'high' | 'minusOne' | 'low'
type PriorityTypeFilter = `type:${string}`
type PriorityFilter = FixedPriorityFilter | PriorityTypeFilter
type AccountStatusFilter = 'all' | 'enabled' | 'disabled' | 'error'
type AccountDisplaySize = 50 | 100 | 150 | 200 | 'all'
type AccountListViewMode = 'table' | 'bar' | 'ring'
type AccountSortKey = 'quotaDay' | 'quotaWeek' | 'accountType' | 'status' | 'priority' | 'lastCheckedAt'
type SortDirection = 'asc' | 'desc'
type PriorityMode = 'low' | 'high' | 'default'
type AccountAction = 'toggle' | 'priority' | 'delete' | 'refresh'
type AccountConfirmType = 'default' | 'warning' | 'error' | 'primary'
type QuotaWindowItem = { label: string; remainingPercent: number; resetAt: string | null }
type AccountStatusPreferences = {
  displaySize?: unknown
  viewMode?: unknown
  sort?: {
    key?: unknown
    direction?: unknown
  }
}

const ACCOUNT_STATUS_PREFERENCE_STORAGE_KEY = 'cpa-helper-codex-keeper-status-preferences'
const ACCOUNT_TABLE_MIN_ROW_HEIGHT = 52
const ACCOUNT_TABLE_MAX_HEIGHT = 'min(620px, max(320px, calc(100dvh - 430px)))'
const ACCOUNT_TABLE_VIRTUAL_THRESHOLD = 200
const disabledTableScrollX = 1850
const normalTableScrollX = 1814
const REFRESH_STATUS_POLL_INTERVAL_MS = 1500
const message = useMessage()
const isLoading = ref(false)
const isBulkDeleting = ref(false)
const isBulkRefreshing = ref(false)
const actingActions = ref<Set<string>>(new Set())
const accounts = ref<CodexKeeperAccount[]>([])
const priorityRules = ref<CodexKeeperPriorityRule[]>([])
const selectedAccount = ref<CodexKeeperAccount | null>(null)
const selectedDisabledAccountKeys = ref<DataTableRowKey[]>([])
const refreshSelectMode = ref(false)
const selectedRefreshAccountNames = ref<string[]>([])
const detailOpen = ref(false)
const accountDisplaySize = ref<AccountDisplaySize>(50)
const accountListViewMode = ref<AccountListViewMode>('table')
const filters = reactive({
  keyword: '',
  accountType: null as string | null,
  priority: 'all' as PriorityFilter,
  status: 'all' as AccountStatusFilter,
})
const accountSort = reactive({
  key: null as AccountSortKey | null,
  direction: 'asc' as SortDirection,
})
const bulkDeleteDialog = reactive({
  show: false,
})
const accountConfirmDialog = reactive({
  show: false,
  title: '',
  content: '',
  positiveText: '',
  type: 'warning' as AccountConfirmType,
  action: null as (() => Promise<void>) | null,
})
const isAccountConfirmSubmitting = ref(false)
const priorityDialog = reactive({
  show: false,
  mode: 'low' as PriorityMode,
  account: null as CodexKeeperAccount | null,
  value: null as number | null,
})
let refreshPollToken = 0

const priorityRuleMap = computed(() =>
  Object.fromEntries(priorityRules.value.map((rule) => [rule.account_type, rule.priority])),
)
const priorityFilterOptions = computed<Array<{ label: string; value: PriorityFilter }>>(() => [
  { label: '全部优先级', value: 'all' },
  { label: '手动优先 >20', value: 'high' },
  ...[...priorityRules.value]
    .filter((rule) => rule.priority >= 0 && rule.priority <= 20)
    .sort((left, right) => {
      const priorityDiff = right.priority - left.priority
      return priorityDiff === 0
        ? left.account_type.localeCompare(right.account_type)
        : priorityDiff
    })
    .map((rule) => ({
      label: `${formatInteger(rule.priority)} (${rule.account_type})`,
      value: priorityTypeFilter(rule.account_type),
    })),
  { label: '临时降级 -1', value: 'minusOne' },
  { label: '手动低优先 <-1', value: 'low' },
])
const accountDisplaySizeOptions: Array<{ label: string; value: AccountDisplaySize }> = [
  { label: '50', value: 50 },
  { label: '100', value: 100 },
  { label: '150', value: 150 },
  { label: '200', value: 200 },
  { label: '全部', value: 'all' },
]
const accountListViewOptions = [
  { label: '表格', key: 'table', icon: () => h(NIcon, null, { default: () => h(Table2) }) },
  { label: '进度条卡片', key: 'bar', icon: () => h(NIcon, null, { default: () => h(BarChart3) }) },
  { label: '圆环卡片', key: 'ring', icon: () => h(NIcon, null, { default: () => h(CircleDot) }) },
]
const quotaSortOptions = [
  { label: '天', key: 'quotaDay' },
  { label: '周', key: 'quotaWeek' },
]

const accountTypeOptions = computed(() =>
  [...new Set(accounts.value.map((item) => item.account_type).filter(Boolean))]
    .sort((a, b) => String(a).localeCompare(String(b)))
    .map((value) => ({ label: String(value), value: String(value) })),
)

const filteredAccounts = computed(() =>
  accounts.value.filter((account) => {
    const keyword = filters.keyword.trim().toLowerCase()
    if (
      keyword &&
      ![account.name, account.email ?? ''].some((value) => value.toLowerCase().includes(keyword))
    ) {
      return false
    }
    if (filters.accountType && account.account_type !== filters.accountType) {
      return false
    }
    return matchesPriorityFilter(account, filters.priority) && matchesStatusFilter(account, filters.status)
  }),
)
const filteredDisabledAccounts = computed(() =>
  sortAccountsForDisplay(filteredAccounts.value.filter((account) => account.disabled)),
)
const filteredNormalAccounts = computed(() =>
  sortAccountsForDisplay(
    filteredAccounts.value.filter((account) => !account.disabled),
    compareNormalAccounts,
  ),
)
const tableLoading = computed(() => isLoading.value)
const enabledAccountCount = computed(() => accounts.value.filter((account) => !account.disabled).length)
const disabledAccountCount = computed(() => accounts.value.filter((account) => account.disabled).length)
const hasDisabledAccounts = computed(() => disabledAccountCount.value > 0)
const errorAccountCount = computed(() => accounts.value.filter(hasAccountError).length)
const showDisabledSection = computed(
  () => filters.status !== 'enabled' && (hasDisabledAccounts.value || filters.status === 'disabled'),
)
const showNormalSection = computed(() => filters.status !== 'disabled')
const systemPriorityCount = computed(
  () =>
    accounts.value.filter(
      (account) =>
        !account.disabled &&
        accountPriority(account) >= -1 &&
        accountPriority(account) <= 20,
    ).length,
)
const highPriorityCount = computed(
  () => accounts.value.filter((account) => accountPriority(account) > 20).length,
)
const activeFilterCount = computed(
  () =>
    Number(filters.keyword.trim() !== '') +
    Number(filters.accountType !== null) +
    Number(filters.priority !== 'all') +
    Number(filters.status !== 'all'),
)
const isTableView = computed(() => accountListViewMode.value === 'table')
const isBarCardView = computed(() => accountListViewMode.value === 'bar')
const accountListViewLabel = computed(() => {
  if (accountListViewMode.value === 'bar') {
    return '进度条卡片'
  }
  if (accountListViewMode.value === 'ring') {
    return '圆环卡片'
  }
  return '表格'
})
const sortedCardAccounts = computed(() => sortAccountsForDisplay(filteredAccounts.value))
const isDisplayAllAccounts = computed(() => accountDisplaySize.value === 'all')
const disabledTableDisplayProps = computed(() =>
  accountTableDisplayProps(visibleDisabledAccounts.value.length),
)
const normalTableDisplayProps = computed(() =>
  accountTableDisplayProps(visibleNormalAccounts.value.length),
)
const displayLimit = computed(() =>
  accountDisplaySize.value === 'all' ? Number.POSITIVE_INFINITY : accountDisplaySize.value,
)
const visibleDisabledAccounts = computed(() =>
  filteredDisabledAccounts.value.slice(0, displayLimit.value),
)
const visibleNormalAccounts = computed(() =>
  filteredNormalAccounts.value.slice(0, displayLimit.value),
)
const visibleCardAccounts = computed(() =>
  sortedCardAccounts.value.slice(0, displayLimit.value),
)
const displaySizeHelpText = computed(() =>
  isTableView.value
    ? isDisplayAllAccounts.value
      ? '当前筛选结果全部展示，账号较多时自动使用虚拟滚动。'
      : `每个分组最多显示 ${accountDisplaySize.value} 个账号。`
    : isDisplayAllAccounts.value
      ? '当前筛选结果全部以卡片展示，账号较多时使用轻量渲染优化。'
      : `统一列表最多显示 ${accountDisplaySize.value} 个账号。`,
)
const activeQuotaSortLabel = computed(() => {
  if (accountSort.key === 'quotaDay') {
    return '天'
  }
  if (accountSort.key === 'quotaWeek') {
    return '周'
  }
  return ''
})
const sortDirectionMark = computed(() => (accountSort.direction === 'asc' ? '↑' : '↓'))

function accountTableDisplayProps(rowCount: number) {
  return isDisplayAllAccounts.value && rowCount > ACCOUNT_TABLE_VIRTUAL_THRESHOLD
    ? {
        virtualScroll: true,
        maxHeight: ACCOUNT_TABLE_MAX_HEIGHT,
        minRowHeight: ACCOUNT_TABLE_MIN_ROW_HEIGHT,
      }
    : {
        virtualScroll: false,
      }
}

function isAccountDisplaySize(value: unknown): value is AccountDisplaySize {
  return value === 50 || value === 100 || value === 150 || value === 200 || value === 'all'
}

function isAccountListViewMode(value: unknown): value is AccountListViewMode {
  return value === 'table' || value === 'bar' || value === 'ring'
}

function isAccountSortKey(value: unknown): value is AccountSortKey {
  return (
    value === 'quotaDay' ||
    value === 'quotaWeek' ||
    value === 'accountType' ||
    value === 'status' ||
    value === 'priority' ||
    value === 'lastCheckedAt'
  )
}

function isSortDirection(value: unknown): value is SortDirection {
  return value === 'asc' || value === 'desc'
}

function readAccountStatusPreferences(): AccountStatusPreferences | null {
  if (typeof localStorage === 'undefined') {
    return null
  }
  const raw = localStorage.getItem(ACCOUNT_STATUS_PREFERENCE_STORAGE_KEY)
  if (!raw) {
    return null
  }
  try {
    const value: unknown = JSON.parse(raw)
    return value && typeof value === 'object' ? (value as AccountStatusPreferences) : null
  } catch {
    return null
  }
}

function restoreAccountStatusPreferences() {
  const preferences = readAccountStatusPreferences()
  if (!preferences) {
    return
  }
  if (isAccountDisplaySize(preferences.displaySize)) {
    accountDisplaySize.value = preferences.displaySize
  }
  if (isAccountListViewMode(preferences.viewMode)) {
    accountListViewMode.value = preferences.viewMode
  }
  const sort = preferences.sort
  if (!sort || typeof sort !== 'object') {
    return
  }
  if (sort.key === null) {
    accountSort.key = null
    accountSort.direction = 'asc'
    return
  }
  if (isAccountSortKey(sort.key) && isSortDirection(sort.direction)) {
    accountSort.key = sort.key
    accountSort.direction = sort.direction
  }
}

function saveAccountStatusPreferences() {
  if (typeof localStorage === 'undefined') {
    return
  }
  try {
    localStorage.setItem(
      ACCOUNT_STATUS_PREFERENCE_STORAGE_KEY,
      JSON.stringify({
        displaySize: accountDisplaySize.value,
        viewMode: accountListViewMode.value,
        sort: {
          key: accountSort.key,
          direction: accountSort.direction,
        },
      }),
    )
  } catch {
    // 本地存储不可用时不影响页面继续使用。
  }
}
const selectedDisabledAccountNames = computed(() =>
  selectedDisabledAccountKeys.value.map((key) => String(key)),
)
const selectedDisabledCount = computed(() => selectedDisabledAccountNames.value.length)
const canBulkDelete = computed(() => selectedDisabledCount.value > 0 && !isBulkDeleting.value)
const filteredAccountNames = computed(() => filteredAccounts.value.map((account) => account.name))
const selectedRefreshAccountNameSet = computed(() => new Set(selectedRefreshAccountNames.value))
const selectedRefreshCount = computed(() => selectedRefreshAccountNames.value.length)
const canRefreshSelected = computed(
  () => selectedRefreshCount.value > 0 && !isBulkRefreshing.value && !isLoading.value,
)
const bulkDeletePreviewNames = computed(() => selectedDisabledAccountNames.value.slice(0, 5))
const bulkDeletePreviewOverflow = computed(() =>
  Math.max(0, selectedDisabledCount.value - bulkDeletePreviewNames.value.length),
)
const canSubmitPriority = computed(() => {
  if (priorityDialog.mode === 'default') {
    return priorityDialog.account !== null && defaultPriority(priorityDialog.account) !== null
  }
  const value = priorityDialog.value
  if (value === null || !Number.isInteger(value)) {
    return false
  }
  return priorityDialog.mode === 'low' ? value < -1 : value > 20
})
const priorityDialogTitle = computed(() => '修改优先级')
const priorityDialogHint = computed(() => {
  if (priorityDialog.mode === 'low') {
    return '手动低优先级必须小于 -1，巡检永远不会自动调整。'
  }
  if (priorityDialog.mode === 'high') {
    return '手动优先必须大于 20，额度耗尽时会临时降为 -1，恢复后回到该值。'
  }
  const account = priorityDialog.account
  const value = account ? defaultPriority(account) : null
  return value === null
    ? '该账号类型没有配置默认优先级，不能使用类型默认值。'
    : `将优先级设置为当前账号类型默认值 ${value}。`
})
const priorityDialogBounds = computed(() => {
  if (priorityDialog.mode === 'low') {
    return { max: -2 }
  }
  if (priorityDialog.mode === 'high') {
    return { min: 21 }
  }
  return {}
})
const priorityModeOptions = computed(() => {
  const defaultValue = priorityDialog.account ? defaultPriority(priorityDialog.account) : null
  return [
    { label: '手动低优先 (< -1)', value: 'low' },
    { label: '手动优先 (> 20)', value: 'high' },
    {
      label: defaultValue === null ? '类型默认优先级（不可用）' : `类型默认优先级 (${defaultValue})`,
      value: 'default',
      disabled: defaultValue === null,
    },
  ]
})

function matchesPriorityFilter(account: CodexKeeperAccount, value: PriorityFilter): boolean {
  const priority = accountPriority(account)
  if (value === 'high') {
    return priority > 20
  }
  if (value === 'minusOne') {
    return priority === -1
  }
  if (value === 'low') {
    return priority < -1
  }
  const accountType = priorityTypeFromFilter(value)
  if (accountType !== null) {
    return (
      account.account_type === accountType &&
      priority >= 0 &&
      priority <= 20
    )
  }
  return true
}

function matchesStatusFilter(account: CodexKeeperAccount, value: AccountStatusFilter): boolean {
  if (value === 'enabled') {
    return !account.disabled
  }
  if (value === 'disabled') {
    return account.disabled
  }
  if (value === 'error') {
    return hasAccountError(account)
  }
  return true
}

function hasAccountError(account: CodexKeeperAccount): boolean {
  return (account.last_error?.trim() ?? '') !== ''
}

function toggleStatusFilter(value: Exclude<AccountStatusFilter, 'all'>) {
  filters.status = filters.status === value ? 'all' : value
}

function isStatusFilterActive(value: Exclude<AccountStatusFilter, 'all'>): boolean {
  return filters.status === value
}

function handleAccountListViewSelect(key: string | number) {
  if (key === 'table' || key === 'bar' || key === 'ring') {
    accountListViewMode.value = key
  }
}

function defaultSortDirection(key: AccountSortKey): SortDirection {
  return key === 'priority' || key === 'lastCheckedAt' ? 'desc' : 'asc'
}

function handleQuotaSortSelect(key: string | number) {
  if (key === 'quotaDay' || key === 'quotaWeek') {
    toggleAccountSort(key)
  }
}

function toggleAccountSort(key: AccountSortKey) {
  if (accountSort.key === key) {
    accountSort.direction = accountSort.direction === 'asc' ? 'desc' : 'asc'
    return
  }
  accountSort.key = key
  accountSort.direction = defaultSortDirection(key)
}

function isAccountSortActive(key: AccountSortKey): boolean {
  return accountSort.key === key
}

function accountSortMark(key: AccountSortKey): string {
  return isAccountSortActive(key) ? sortDirectionMark.value : ''
}

function accountPriority(account: CodexKeeperAccount): number {
  return account.priority ?? 0
}

function priorityTypeFilter(accountType: string): PriorityTypeFilter {
  return `type:${accountType}`
}

function priorityTypeFromFilter(value: PriorityFilter): string | null {
  return value.startsWith('type:') ? value.slice('type:'.length) : null
}

function normalAccountTypePriority(account: CodexKeeperAccount): number {
  if (!account.account_type) {
    return Number.NEGATIVE_INFINITY
  }
  return priorityRuleMap.value[account.account_type] ?? Number.NEGATIVE_INFINITY
}

function compareNormalAccounts(left: CodexKeeperAccount, right: CodexKeeperAccount): number {
  const priorityDiff = normalAccountTypePriority(right) - normalAccountTypePriority(left)
  if (priorityDiff !== 0) {
    return priorityDiff
  }
  return (left.email ?? left.name).localeCompare(right.email ?? right.name)
}

function sortAccountsForDisplay(
  source: CodexKeeperAccount[],
  defaultCompare?: (left: CodexKeeperAccount, right: CodexKeeperAccount) => number,
): CodexKeeperAccount[] {
  const rows = [...source]
  if (accountSort.key === null) {
    return defaultCompare ? rows.sort(defaultCompare) : rows
  }
  return rows.sort(compareAccountsByActiveSort)
}

function compareAccountsByActiveSort(left: CodexKeeperAccount, right: CodexKeeperAccount): number {
  const direction = accountSort.direction
  let result = 0
  switch (accountSort.key) {
    case 'quotaDay':
    case 'quotaWeek':
      result = compareNullableNumber(
        quotaSortRemainingPercent(left, accountSort.key),
        quotaSortRemainingPercent(right, accountSort.key),
        direction,
      )
      break
    case 'accountType':
      result = compareNullableString(left.account_type, right.account_type, direction)
      break
    case 'status':
      result = compareNullableNumber(left.disabled ? 1 : 0, right.disabled ? 1 : 0, direction)
      break
    case 'priority':
      result = compareNullableNumber(accountPriority(left), accountPriority(right), direction)
      break
    case 'lastCheckedAt':
      result = compareNullableNumber(
        timestampValue(left.last_checked_at),
        timestampValue(right.last_checked_at),
        direction,
      )
      break
    default:
      result = 0
  }
  return result === 0 ? accountIdentityText(left).localeCompare(accountIdentityText(right)) : result
}

function compareNullableNumber(
  left: number | null,
  right: number | null,
  direction: SortDirection,
): number {
  if (left === null && right === null) {
    return 0
  }
  if (left === null) {
    return 1
  }
  if (right === null) {
    return -1
  }
  const result = left - right
  return direction === 'asc' ? result : -result
}

function compareNullableString(
  left: string | null,
  right: string | null,
  direction: SortDirection,
): number {
  if (left === null && right === null) {
    return 0
  }
  if (left === null) {
    return 1
  }
  if (right === null) {
    return -1
  }
  const result = left.localeCompare(right)
  return direction === 'asc' ? result : -result
}

function timestampValue(value: string | null): number | null {
  if (!value) {
    return null
  }
  const timestamp = new Date(value).getTime()
  return Number.isNaN(timestamp) ? null : timestamp
}

function accountIdentityText(account: CodexKeeperAccount): string {
  return account.email ?? account.name
}

function defaultPriority(account: CodexKeeperAccount): number | null {
  if (!account.account_type) {
    return null
  }
  return priorityRuleMap.value[account.account_type] ?? null
}

function isPaidQuotaWindowAccount(accountType: string | null): boolean {
  const normalized = accountType?.trim().toLowerCase()
  return normalized === 'plus' || normalized === 'team' || normalized?.startsWith('pro') === true
}

function quotaWindowLabels(accountType: string | null): { primary: string; secondary: string } {
  const normalized = accountType?.trim().toLowerCase()
  if (normalized === 'free') {
    return { primary: '周限额', secondary: '次限额' }
  }
  if (isPaidQuotaWindowAccount(accountType)) {
    return { primary: '5小时限额', secondary: '周限额' }
  }
  return { primary: '主', secondary: '次' }
}

function quotaWindowItems(account: CodexKeeperAccount): QuotaWindowItem[] {
  if (account.primary_used_percent === null) {
    return []
  }
  const labels = quotaWindowLabels(account.account_type)
  const items = [
    {
      label: labels.primary,
      remainingPercent: remainingQuotaPercent(account.primary_used_percent),
      resetAt: account.primary_reset_at,
    },
  ]
  if (account.secondary_used_percent !== null) {
    items.push({
      label: labels.secondary,
      remainingPercent: remainingQuotaPercent(account.secondary_used_percent),
      resetAt: account.secondary_reset_at,
    })
  }
  return items
}

function quotaSortRemainingPercent(account: CodexKeeperAccount, key: AccountSortKey): number | null {
  const normalized = account.account_type?.trim().toLowerCase()
  if (key === 'quotaDay') {
    if (normalized === 'free') {
      return nullableRemainingQuotaPercent(account.secondary_used_percent)
    }
    return nullableRemainingQuotaPercent(account.primary_used_percent)
  }
  if (key === 'quotaWeek') {
    if (normalized === 'free') {
      return nullableRemainingQuotaPercent(account.primary_used_percent)
    }
    if (isPaidQuotaWindowAccount(account.account_type)) {
      return nullableRemainingQuotaPercent(account.secondary_used_percent)
    }
  }
  return null
}

function nullableRemainingQuotaPercent(usedPercent: number | null): number | null {
  return usedPercent === null ? null : remainingQuotaPercent(usedPercent)
}

function remainingQuotaPercent(usedPercent: number): number {
  return Math.max(0, Math.min(100, 100 - usedPercent))
}

function quotaBarTone(percent: number): string {
  if (percent < 30) {
    return 'is-danger'
  }
  if (percent < 70) {
    return 'is-warning'
  }
  return 'is-healthy'
}

function formatQuotaResetTime(value: string | null): string | null {
  if (!value) {
    return null
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return null
  }
  return new Intl.DateTimeFormat('zh-CN', {
    timeZone: BEIJING_TIME_ZONE,
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(date)
}

function quotaText(account: CodexKeeperAccount): string {
  const items = quotaWindowItems(account)
  if (items.length === 0) {
    return '-'
  }
  return items
    .map((item) => {
      const resetTime = formatQuotaResetTime(item.resetAt)
      return resetTime
        ? `${item.label}剩余 ${item.remainingPercent}%，刷新 ${resetTime}`
        : `${item.label}剩余 ${item.remainingPercent}%`
    })
    .join(' / ')
}

function latestActionText(account: CodexKeeperAccount): string {
  return account.last_error?.trim() || account.latest_action?.trim() || '-'
}

function renderQuotaCell(account: CodexKeeperAccount) {
  const items = quotaWindowItems(account)
  if (items.length === 0) {
    return '-'
  }
  return h(
    'div',
    { class: 'quota-window-cell' },
    items.map((item) => {
      const resetTime = formatQuotaResetTime(item.resetAt)
      return h(
        'div',
        {
          class: 'quota-window-item',
          title: resetTime
            ? `${item.label}剩余 ${item.remainingPercent}%，刷新 ${resetTime}`
            : `${item.label}剩余 ${item.remainingPercent}%`,
        },
        [
          h('div', { class: 'quota-window-head' }, [
            h('span', { class: 'quota-window-label' }, item.label),
            h('span', { class: 'quota-window-meta' }, [
              h('span', { class: 'quota-window-percent' }, `${item.remainingPercent}%`),
              resetTime ? h('span', { class: 'quota-window-reset' }, resetTime) : null,
            ]),
          ]),
          h('div', { class: 'quota-window-track' }, [
            h('div', {
              class: ['quota-window-fill', quotaBarTone(item.remainingPercent)],
              style: { width: `${item.remainingPercent}%` },
            }),
          ]),
        ],
      )
    }),
  )
}

async function loadAccounts() {
  isLoading.value = true
  try {
    const [accountsResponse, settings] = await Promise.all([
      listCodexKeeperAccounts(),
      getCodexKeeperSettings(),
    ])
    accounts.value = accountsResponse.items
    priorityRules.value = settings.priority_rules
  } catch (error) {
    message.error(error instanceof Error ? error.message : '加载账号状态失败')
  } finally {
    isLoading.value = false
  }
}

function accountRowKey(account: CodexKeeperAccount): string {
  return account.name
}

function handleDisabledSelectionUpdate(keys: DataTableRowKey[]) {
  selectedDisabledAccountKeys.value = keys
}

function pruneSelectedDisabledAccountKeys() {
  const availableNames = new Set(visibleDisabledAccounts.value.map((account) => account.name))
  selectedDisabledAccountKeys.value = selectedDisabledAccountKeys.value.filter((key) =>
    availableNames.has(String(key)),
  )
}

function pruneSelectedRefreshAccountNames() {
  const availableNames = new Set(filteredAccountNames.value)
  selectedRefreshAccountNames.value = selectedRefreshAccountNames.value.filter((name) =>
    availableNames.has(name),
  )
}

function toggleRefreshSelectMode() {
  refreshSelectMode.value = !refreshSelectMode.value
  if (!refreshSelectMode.value) {
    selectedRefreshAccountNames.value = []
  }
}

function exitRefreshSelectMode() {
  refreshSelectMode.value = false
  selectedRefreshAccountNames.value = []
}

function isRefreshAccountSelected(account: CodexKeeperAccount): boolean {
  return selectedRefreshAccountNameSet.value.has(account.name)
}

function toggleRefreshAccountSelection(account: CodexKeeperAccount) {
  if (isRowActing(account) || isBulkRefreshing.value || isBulkDeleting.value) {
    return
  }
  if (isRefreshAccountSelected(account)) {
    selectedRefreshAccountNames.value = selectedRefreshAccountNames.value.filter(
      (name) => name !== account.name,
    )
    return
  }
  selectedRefreshAccountNames.value = [...selectedRefreshAccountNames.value, account.name]
}

function selectAllFilteredRefreshAccounts() {
  selectedRefreshAccountNames.value = filteredAccountNames.value
}

function handleAccountCardClick(account: CodexKeeperAccount) {
  if (refreshSelectMode.value) {
    toggleRefreshAccountSelection(account)
    return
  }
  openDetail(account)
}

function accountTableRowProps(account: CodexKeeperAccount) {
  const isSelected = isRefreshAccountSelected(account)
  return {
    class: {
      'is-refresh-selectable': refreshSelectMode.value,
      'is-refresh-selected': isSelected,
    },
    onClick: (event: MouseEvent) => {
      if (!refreshSelectMode.value) {
        return
      }
      const target = event.target instanceof HTMLElement ? event.target : null
      if (target?.closest('button, a, input, textarea, select, .n-checkbox, .n-base-selection')) {
        return
      }
      toggleRefreshAccountSelection(account)
    },
  }
}

function openBulkDeleteDialog() {
  if (!canBulkDelete.value) {
    return
  }
  bulkDeleteDialog.show = true
}

async function submitBulkDelete() {
  const authNames = selectedDisabledAccountNames.value
  if (authNames.length === 0) {
    return
  }
  isBulkDeleting.value = true
  try {
    const result = await bulkDeleteCodexKeeperAccounts({ auth_names: authNames })
    const deletedNames = new Set(result.deleted)
    selectedDisabledAccountKeys.value = selectedDisabledAccountKeys.value.filter(
      (key) => !deletedNames.has(String(key)),
    )
    if (result.failed.length > 0 && result.deleted.length > 0) {
      message.warning(`批量删除完成：成功 ${result.deleted.length} 个，失败 ${result.failed.length} 个`)
    } else if (result.failed.length > 0) {
      message.error(`批量删除失败：失败 ${result.failed.length} 个`)
    } else {
      message.success(`已删除 ${result.deleted.length} 个已禁用账号`)
    }
    bulkDeleteDialog.show = false
    await loadAccounts()
  } catch (error) {
    message.error(error instanceof Error ? error.message : '批量删除失败')
  } finally {
    isBulkDeleting.value = false
  }
}

function openDetail(account: CodexKeeperAccount) {
  selectedAccount.value = account
  detailOpen.value = true
}

function openPriorityDialog(account: CodexKeeperAccount) {
  priorityDialog.account = account
  const priority = accountPriority(account)
  const mode =
    priority < -1
      ? 'low'
      : priority > 20
        ? 'high'
        : 'default'
  setPriorityDialogMode(defaultPriority(account) === null && mode === 'default' ? 'low' : mode)
  priorityDialog.show = true
}

function setPriorityDialogMode(mode: PriorityMode) {
  priorityDialog.mode = mode
  const account = priorityDialog.account
  if (!account) {
    priorityDialog.value = null
    return
  }
  if (mode === 'low') {
    const priority = accountPriority(account)
    priorityDialog.value = priority < -1 ? priority : -2
    return
  }
  if (mode === 'high') {
    const priority = accountPriority(account)
    priorityDialog.value = priority > 20 ? priority : 21
    return
  }
  priorityDialog.value = defaultPriority(account)
}

async function submitPriorityDialog() {
  if (!priorityDialog.account || !canSubmitPriority.value) {
    return
  }
  const value =
    priorityDialog.mode === 'default'
      ? defaultPriority(priorityDialog.account)
      : priorityDialog.value
  if (value === null) {
    return
  }
  await runAccountAction(
    priorityDialog.account,
    'priority',
    () => updateCodexKeeperPriority(priorityDialog.account!.name, value),
    '优先级已更新',
  )
  priorityDialog.show = false
}

function openAccountConfirm(
  title: string,
  content: string,
  positiveText: string,
  type: AccountConfirmType,
  action: () => Promise<void>,
) {
  accountConfirmDialog.title = title
  accountConfirmDialog.content = content
  accountConfirmDialog.positiveText = positiveText
  accountConfirmDialog.type = type
  accountConfirmDialog.action = action
  accountConfirmDialog.show = true
}

async function submitAccountConfirm() {
  if (!accountConfirmDialog.action || isAccountConfirmSubmitting.value) {
    return
  }
  isAccountConfirmSubmitting.value = true
  try {
    await accountConfirmDialog.action()
    accountConfirmDialog.show = false
  } finally {
    isAccountConfirmSubmitting.value = false
  }
}

function confirmEnableAccount(account: CodexKeeperAccount) {
  openAccountConfirm(
    '启用账号',
    `启用 ${account.name}？`,
    '确认启用',
    'primary',
    () => enableAccount(account),
  )
}

function confirmDisableAccount(account: CodexKeeperAccount) {
  openAccountConfirm(
    '禁用账号',
    `禁用 ${account.name}？`,
    '确认禁用',
    'warning',
    () => disableAccount(account),
  )
}

function confirmDeleteAccount(account: CodexKeeperAccount) {
  openAccountConfirm(
    '删除账号',
    `删除 ${account.name}？此操作会从 CPA 删除 auth file。`,
    '确认删除',
    'error',
    () => deleteAccount(account),
  )
}

function enableAccount(account: CodexKeeperAccount) {
  return runAccountAction(
    account,
    'toggle',
    () => enableCodexKeeperAccount(account.name),
    '账号已启用',
  )
}

function disableAccount(account: CodexKeeperAccount) {
  return runAccountAction(
    account,
    'toggle',
    () => disableCodexKeeperAccount(account.name),
    '账号已禁用',
  )
}

function deleteAccount(account: CodexKeeperAccount) {
  return runAccountAction(
    account,
    'delete',
    () => deleteCodexKeeperAccount(account.name),
    '账号已删除',
  )
}

function refreshAccount(account: CodexKeeperAccount, options: { closeDetail?: boolean } = {}) {
  return refreshAccounts([account.name], options)
}

async function refreshSelectedAccounts() {
  await refreshAccounts(selectedRefreshAccountNames.value, { clearSelection: true })
}

function uniqueAccountNames(raw: string[]): string[] {
  return [...new Set(raw.map((name) => name.trim()).filter(Boolean))]
}

function sleep(ms: number) {
  return new Promise((resolve) => window.setTimeout(resolve, ms))
}

async function pollRefreshUntilIdle() {
  const token = ++refreshPollToken
  for (;;) {
    await sleep(REFRESH_STATUS_POLL_INTERVAL_MS)
    if (token !== refreshPollToken) {
      return
    }
    try {
      const status = await getCodexKeeperStatus()
      if (status.running) {
        continue
      }
      await loadAccounts()
      return
    } catch {
      continue
    }
  }
}

async function refreshAccounts(
  rawNames: string[],
  options: { closeDetail?: boolean; clearSelection?: boolean } = {},
) {
  const authNames = uniqueAccountNames(rawNames)
  if (authNames.length === 0) {
    return
  }
  const refreshKeys = authNames
    .map((name) => accounts.value.find((account) => account.name === name))
    .filter((account): account is CodexKeeperAccount => account !== undefined)
    .map((account) => accountActionKey(account, 'refresh'))
  if (refreshKeys.some((key) => actingActions.value.has(key)) || isBulkRefreshing.value) {
    return
  }
  const nextActions = new Set(actingActions.value)
  refreshKeys.forEach((key) => nextActions.add(key))
  actingActions.value = nextActions
  if (authNames.length > 1 || options.clearSelection) {
    isBulkRefreshing.value = true
  }
  try {
    await refreshCodexKeeperAccounts({ auth_names: authNames })
    message.success(authNames.length === 1 ? '已开始刷新账号' : `已开始刷新 ${authNames.length} 个账号`)
    if (options.closeDetail) {
      detailOpen.value = false
    }
    if (options.clearSelection) {
      exitRefreshSelectMode()
    }
    void pollRefreshUntilIdle()
  } catch (error) {
    message.error(error instanceof Error ? error.message : '刷新账号失败')
  } finally {
    const restActions = new Set(actingActions.value)
    refreshKeys.forEach((key) => restActions.delete(key))
    actingActions.value = restActions
    isBulkRefreshing.value = false
  }
}

function accountActionKey(account: CodexKeeperAccount, action: AccountAction): string {
  return `${action}\u0000${account.name}`
}

function isActionLoading(account: CodexKeeperAccount, action: AccountAction): boolean {
  return actingActions.value.has(accountActionKey(account, action))
}

function isRowActing(account: CodexKeeperAccount): boolean {
  return (['toggle', 'priority', 'delete', 'refresh'] as const).some((action) =>
    isActionLoading(account, action),
  )
}

async function runAccountAction(
  account: CodexKeeperAccount,
  actionType: AccountAction,
  action: () => Promise<void>,
  successText: string,
) {
  const key = accountActionKey(account, actionType)
  if (actingActions.value.has(key)) {
    return
  }
  actingActions.value = new Set(actingActions.value).add(key)
  try {
    await action()
    message.success(successText)
    await loadAccounts()
    if (selectedAccount.value?.name === account.name) {
      const freshAccount = accounts.value.find((item) => item.name === account.name) ?? null
      selectedAccount.value = freshAccount
      detailOpen.value = freshAccount !== null
    }
  } catch (error) {
    message.error(error instanceof Error ? error.message : '账号操作失败')
  } finally {
    const nextActions = new Set(actingActions.value)
    nextActions.delete(key)
    actingActions.value = nextActions
  }
}

const baseColumns: DataTableColumns<CodexKeeperAccount> = [
  { title: '账号', key: 'name', width: 300, ellipsis: { tooltip: true } },
  { title: '邮箱', key: 'email', width: 260, ellipsis: { tooltip: true } },
  {
    title: '类型',
    key: 'account_type',
    width: 90,
    render: (row) => row.account_type ?? '未知',
  },
  {
    title: '状态',
    key: 'disabled',
    width: 90,
    render: (row) =>
      h(
        NTag,
        { size: 'small', bordered: false, type: row.disabled ? 'warning' : 'success' },
        { default: () => (row.disabled ? '已禁用' : '启用中') },
      ),
  },
  {
    title: '优先级',
    key: 'priority',
    width: 88,
    render: (row) => formatInteger(accountPriority(row)),
  },
  {
    title: '额度窗口',
    key: 'quota',
    width: 260,
    render: (row) => renderQuotaCell(row),
  },
  {
    title: '最近巡检',
    key: 'last_checked_at',
    width: 150,
    render: (row) => formatDateTime(row.last_checked_at),
  },
  {
    title: '最近操作',
    key: 'latest_action',
    width: 340,
    ellipsis: { tooltip: true },
    render: (row) => {
      const text = latestActionText(row)
      return text === '-' ? '-' : h('span', { class: 'latest-action-text', title: text }, text)
    },
  },
]

const disabledActionColumn: DataTableColumns<CodexKeeperAccount>[number] = {
  title: '',
  key: 'actions',
  width: 224,
  fixed: 'right',
  render: (row: CodexKeeperAccount) => {
    return h(
      NSpace,
      { class: 'account-actions', size: 4, wrap: false },
      {
        default: () => [
          h(
            NButton,
            { size: 'small', quaternary: true, onClick: () => openDetail(row) },
            { default: () => '详情' },
          ),
          h(
            NButton,
            {
              size: 'small',
              quaternary: true,
              type: 'primary',
              disabled: isRowActing(row) || isBulkDeleting.value || isBulkRefreshing.value,
              loading: isActionLoading(row, 'toggle'),
              onClick: () => confirmEnableAccount(row),
            },
            { default: () => '启用' },
          ),
          h(
            NButton,
            {
              size: 'small',
              quaternary: true,
              type: 'error',
              disabled: isRowActing(row) || isBulkDeleting.value || isBulkRefreshing.value,
              loading: isActionLoading(row, 'delete'),
              onClick: () => confirmDeleteAccount(row),
            },
            { default: () => '删除' },
          ),
          h(
            NButton,
            {
              size: 'small',
              quaternary: true,
              type: 'primary',
              disabled: isRowActing(row) || isBulkDeleting.value || isBulkRefreshing.value,
              loading: isActionLoading(row, 'refresh'),
              onClick: () => refreshAccount(row),
            },
            { default: () => '刷新' },
          ),
        ],
      },
    )
  },
}

const normalActionColumn: DataTableColumns<CodexKeeperAccount>[number] = {
  title: '',
  key: 'actions',
  width: 232,
  fixed: 'right',
  render: (row: CodexKeeperAccount) => {
    return h(
      NSpace,
      { class: 'account-actions', size: 4, wrap: false },
      {
        default: () => [
          h(
            NButton,
            { size: 'small', quaternary: true, onClick: () => openDetail(row) },
            { default: () => '详情' },
          ),
          h(
            NButton,
            {
              size: 'small',
              quaternary: true,
              type: 'warning',
              disabled: isRowActing(row) || isBulkDeleting.value || isBulkRefreshing.value,
              loading: isActionLoading(row, 'toggle'),
              onClick: () => confirmDisableAccount(row),
            },
            { default: () => '禁用' },
          ),
          h(
            NButton,
            {
              size: 'small',
              quaternary: true,
              disabled: isRowActing(row) || isBulkDeleting.value || isBulkRefreshing.value,
              onClick: () => openPriorityDialog(row),
            },
            { default: () => '优先级' },
          ),
          h(
            NButton,
            {
              size: 'small',
              quaternary: true,
              type: 'primary',
              disabled: isRowActing(row) || isBulkDeleting.value || isBulkRefreshing.value,
              loading: isActionLoading(row, 'refresh'),
              onClick: () => refreshAccount(row),
            },
            { default: () => '刷新' },
          ),
        ],
      },
    )
  },
}

const disabledColumns = computed<DataTableColumns<CodexKeeperAccount>>(() => [
  {
    type: 'selection',
    width: 44,
    disabled: (row: CodexKeeperAccount) => isRowActing(row) || isBulkDeleting.value,
  },
  ...baseColumns,
  disabledActionColumn,
])

const normalColumns = computed<DataTableColumns<CodexKeeperAccount>>(() => [
  ...baseColumns,
  normalActionColumn,
])

restoreAccountStatusPreferences()

watch(
  [accountDisplaySize, accountListViewMode, () => accountSort.key, () => accountSort.direction],
  saveAccountStatusPreferences,
)
watch(visibleDisabledAccounts, pruneSelectedDisabledAccountKeys)
watch(filteredAccounts, pruneSelectedRefreshAccountNames)

onMounted(loadAccounts)

onBeforeUnmount(() => {
  refreshPollToken += 1
})
</script>

<template>
  <section class="page account-status-page">
    <div class="page-header account-page-header">
      <div class="account-header-copy">
        <div class="account-header-title-row">
          <h1 class="page-title">账号状态</h1>
          <div class="header-actions">
            <NButton secondary :loading="isLoading" @click="loadAccounts">
              <template #icon>
                <NIcon :component="RefreshCw" />
              </template>
              重新加载
            </NButton>
          </div>
        </div>
        <p class="page-subtitle">查看 Codex auth file 的健康、额度和优先级维护结果</p>
      </div>
    </div>

    <div class="metric-grid account-metrics">
      <div class="metric-card">
        <div class="metric-icon" aria-hidden="true">
          <Users :size="20" :stroke-width="2.2" />
        </div>
        <div class="metric-label">账号总数</div>
        <div class="metric-value">{{ formatInteger(accounts.length) }}</div>
        <div class="metric-footnote">全部 auth file</div>
      </div>
      <button
        type="button"
        class="metric-card metric-action is-success"
        :class="{ 'is-active': isStatusFilterActive('enabled') }"
        :aria-pressed="isStatusFilterActive('enabled')"
        @click="toggleStatusFilter('enabled')"
      >
        <div class="metric-icon" aria-hidden="true">
          <Activity :size="20" :stroke-width="2.2" />
        </div>
        <div class="metric-label">启用中</div>
        <div class="metric-value">{{ formatInteger(enabledAccountCount) }}</div>
        <div class="metric-footnote">可参与调度</div>
      </button>
      <button
        type="button"
        class="metric-card metric-action is-warning"
        :class="{ 'is-active': isStatusFilterActive('disabled') }"
        :aria-pressed="isStatusFilterActive('disabled')"
        @click="toggleStatusFilter('disabled')"
      >
        <div class="metric-icon" aria-hidden="true">
          <PauseCircle :size="20" :stroke-width="2.2" />
        </div>
        <div class="metric-label">已禁用</div>
        <div class="metric-value">{{ formatInteger(disabledAccountCount) }}</div>
        <div class="metric-footnote">停用账号</div>
      </button>
      <button
        type="button"
        class="metric-card metric-action is-danger"
        :class="{ 'is-active': isStatusFilterActive('error') }"
        :aria-pressed="isStatusFilterActive('error')"
        @click="toggleStatusFilter('error')"
      >
        <div class="metric-icon" aria-hidden="true">
          <AlertTriangle :size="20" :stroke-width="2.2" />
        </div>
        <div class="metric-label">检测异常</div>
        <div class="metric-value">{{ formatInteger(errorAccountCount) }}</div>
        <div class="metric-footnote">最近错误</div>
      </button>
      <div class="metric-card">
        <div class="metric-icon" aria-hidden="true">
          <Gauge :size="20" :stroke-width="2.2" />
        </div>
        <div class="metric-label">巡检托管</div>
        <div class="metric-value">{{ formatInteger(systemPriorityCount) }}</div>
        <div class="metric-footnote">类型默认</div>
      </div>
      <div class="metric-card">
        <div class="metric-icon" aria-hidden="true">
          <Zap :size="20" :stroke-width="2.2" />
        </div>
        <div class="metric-label">手动优先</div>
        <div class="metric-value">{{ formatInteger(highPriorityCount) }}</div>
        <div class="metric-footnote">高优先级</div>
      </div>
    </div>

    <section class="panel account-list-panel">
      <div class="status-toolbar">
        <div class="toolbar-heading">
          <div>
            <h2 class="toolbar-title">账号列表</h2>
            <p class="toolbar-subtitle">
              正常 {{ filteredNormalAccounts.length }} / {{ enabledAccountCount }} 个账号
              <template v-if="hasDisabledAccounts">
                ，已禁用 {{ filteredDisabledAccounts.length }} / {{ disabledAccountCount }} 个账号
              </template>
            </p>
          </div>
          <NTag v-if="activeFilterCount > 0" size="small" type="info" :bordered="false">
            已筛选 {{ activeFilterCount }} 项
          </NTag>
        </div>
        <div class="filter-grid">
          <NInput v-model:value="filters.keyword" clearable placeholder="搜索账号或邮箱" />
          <NSelect
            v-model:value="filters.accountType"
            :options="accountTypeOptions"
            clearable
            filterable
            placeholder="账号类型"
          />
          <NSelect
            v-model:value="filters.priority"
            :options="priorityFilterOptions"
          />
        </div>
        <div class="list-control-row">
          <div class="list-main-controls">
            <NDropdown
              trigger="click"
              :options="accountListViewOptions"
              @select="handleAccountListViewSelect"
            >
              <NButton secondary size="small">
                <template #icon>
                  <NIcon :component="ChevronDown" />
                </template>
                切换样式：{{ accountListViewLabel }}
              </NButton>
            </NDropdown>
            <NButton
              secondary
              size="small"
              :type="refreshSelectMode ? 'primary' : 'default'"
              @click="toggleRefreshSelectMode"
            >
              多选刷新
            </NButton>
            <template v-if="refreshSelectMode">
              <NTag size="small" type="info" :bordered="false">
                {{ selectedRefreshCount }} 已选
              </NTag>
              <NButton
                secondary
                size="small"
                :disabled="filteredAccountNames.length === 0 || isBulkRefreshing"
                @click="selectAllFilteredRefreshAccounts"
              >
                全选当前筛选
              </NButton>
              <NButton
                secondary
                type="primary"
                size="small"
                :disabled="!canRefreshSelected"
                :loading="isBulkRefreshing"
                @click="refreshSelectedAccounts"
              >
                刷新已选
              </NButton>
              <NButton secondary size="small" :disabled="isBulkRefreshing" @click="exitRefreshSelectMode">
                退出选择
              </NButton>
            </template>
          </div>
          <div class="sort-control-row" aria-label="账号排序">
            <span class="sort-control-label">排序</span>
            <NDropdown trigger="click" :options="quotaSortOptions" @select="handleQuotaSortSelect">
              <NButton
                secondary
                size="small"
                :type="accountSort.key === 'quotaDay' || accountSort.key === 'quotaWeek' ? 'primary' : 'default'"
              >
                额度窗口{{ activeQuotaSortLabel ? `：${activeQuotaSortLabel} ${sortDirectionMark}` : '' }}
              </NButton>
            </NDropdown>
            <NButton
              secondary
              size="small"
              :type="isAccountSortActive('accountType') ? 'primary' : 'default'"
              @click="toggleAccountSort('accountType')"
            >
              类型 {{ accountSortMark('accountType') }}
            </NButton>
            <NButton
              secondary
              size="small"
              :type="isAccountSortActive('status') ? 'primary' : 'default'"
              @click="toggleAccountSort('status')"
            >
              状态 {{ accountSortMark('status') }}
            </NButton>
            <NButton
              secondary
              size="small"
              :type="isAccountSortActive('priority') ? 'primary' : 'default'"
              @click="toggleAccountSort('priority')"
            >
              优先级 {{ accountSortMark('priority') }}
            </NButton>
            <NButton
              secondary
              size="small"
              :type="isAccountSortActive('lastCheckedAt') ? 'primary' : 'default'"
              @click="toggleAccountSort('lastCheckedAt')"
            >
              最近巡检 {{ accountSortMark('lastCheckedAt') }}
            </NButton>
          </div>
        </div>
      </div>

      <div v-if="isTableView" class="account-sections">
        <section v-if="showDisabledSection" class="account-section">
          <div class="account-section-header">
            <div class="account-section-title-group">
              <h3 class="account-section-title">已禁用账号</h3>
              <p class="account-section-subtitle">
                显示 {{ visibleDisabledAccounts.length }} / {{ filteredDisabledAccounts.length }} 个账号
              </p>
            </div>
            <div class="account-section-actions">
              <NButton
                secondary
                type="error"
                :disabled="!canBulkDelete"
                :loading="isBulkDeleting"
                @click="openBulkDeleteDialog"
              >
                批量删除（{{ selectedDisabledCount }}）
              </NButton>
            </div>
          </div>
          <NDataTable
            class="account-table"
            size="small"
            :loading="tableLoading"
            :columns="disabledColumns"
            :data="visibleDisabledAccounts"
            :row-key="accountRowKey"
            :row-props="accountTableRowProps"
            :checked-row-keys="selectedDisabledAccountKeys"
            :pagination="false"
            v-bind="disabledTableDisplayProps"
            table-layout="fixed"
            :scroll-x="disabledTableScrollX"
            @update:checked-row-keys="handleDisabledSelectionUpdate"
          >
            <template #empty>
              <div class="empty-state">当前筛选下暂无已禁用账号</div>
            </template>
          </NDataTable>
        </section>

        <section v-if="showNormalSection" class="account-section">
          <div class="account-section-header">
            <div class="account-section-title-group">
              <h3 class="account-section-title">正常账号</h3>
              <p class="account-section-subtitle">
                显示 {{ visibleNormalAccounts.length }} / {{ filteredNormalAccounts.length }} 个账号
              </p>
            </div>
          </div>
          <NDataTable
            class="account-table"
            size="small"
            :loading="tableLoading"
            :columns="normalColumns"
            :data="visibleNormalAccounts"
            :row-key="accountRowKey"
            :row-props="accountTableRowProps"
            :pagination="false"
            v-bind="normalTableDisplayProps"
            table-layout="fixed"
            :scroll-x="normalTableScrollX"
          >
            <template #empty>
              <div class="empty-state">当前筛选下暂无正常账号</div>
            </template>
          </NDataTable>
        </section>
      </div>
      <div v-else class="account-card-shell">
        <section class="account-section account-card-section">
          <div class="account-section-header">
            <div class="account-section-title-group">
              <h3 class="account-section-title">{{ accountListViewLabel }}</h3>
              <p class="account-section-subtitle">
                显示 {{ visibleCardAccounts.length }} / {{ sortedCardAccounts.length }} 个账号
              </p>
            </div>
          </div>
          <div v-if="isLoading" class="empty-state">账号加载中...</div>
          <div v-else-if="visibleCardAccounts.length === 0" class="empty-state">
            当前筛选下暂无账号
          </div>
          <div
            v-else
            class="account-card-grid"
            :class="{ 'is-bar': isBarCardView, 'is-ring': !isBarCardView }"
          >
            <button
              v-for="account in visibleCardAccounts"
              :key="account.name"
              type="button"
              class="account-card"
              :class="{
                'is-disabled': account.disabled,
                'is-enabled': !account.disabled,
                'has-error': hasAccountError(account),
                'is-select-mode': refreshSelectMode,
                'is-selected': isRefreshAccountSelected(account),
              }"
              :aria-label="
                refreshSelectMode
                  ? `选择 ${account.email ?? account.name}`
                  : `查看 ${account.email ?? account.name} 详情`
              "
              :aria-pressed="refreshSelectMode ? isRefreshAccountSelected(account) : undefined"
              @click="handleAccountCardClick(account)"
            >
              <div class="account-card-top">
                <div class="account-card-identity">
                  <span class="account-card-email">{{ account.email ?? account.name }}</span>
                  <span class="account-card-name">{{ account.name }}</span>
                </div>
                <span
                  class="account-status-pill"
                  :class="account.disabled ? 'is-warning' : 'is-success'"
                >
                  {{ account.disabled ? '已禁用' : '启用中' }}
                </span>
              </div>
              <div class="account-card-meta-grid">
                <div class="account-card-meta-item">
                  <span>类型</span>
                  <strong>{{ account.account_type ?? '未知' }}</strong>
                </div>
                <div class="account-card-meta-item">
                  <span>优先级</span>
                  <strong>{{ formatInteger(accountPriority(account)) }}</strong>
                </div>
                <div class="account-card-meta-item">
                  <span>最近巡检</span>
                  <strong>{{ formatDateTime(account.last_checked_at) }}</strong>
                </div>
              </div>
              <div class="account-card-quota">
                <template v-if="quotaWindowItems(account).length > 0">
                  <template v-if="isBarCardView">
                    <div
                      v-for="item in quotaWindowItems(account)"
                      :key="item.label"
                      class="card-quota-bar"
                    >
                      <div class="card-quota-head">
                        <span>{{ item.label }}</span>
                        <strong>{{ item.remainingPercent }}%</strong>
                      </div>
                      <div class="card-quota-track">
                        <div
                          class="card-quota-fill"
                          :class="quotaBarTone(item.remainingPercent)"
                          :style="{ width: `${item.remainingPercent}%` }"
                        />
                      </div>
                      <span class="card-quota-reset">
                        {{ formatQuotaResetTime(item.resetAt) ?? '未记录刷新时间' }}
                      </span>
                    </div>
                  </template>
                  <div v-else class="card-quota-rings">
                    <div
                      v-for="item in quotaWindowItems(account)"
                      :key="item.label"
                      class="card-quota-ring-item"
                    >
                      <div
                        class="quota-ring"
                        :class="quotaBarTone(item.remainingPercent)"
                        :style="{ '--quota-deg': `${item.remainingPercent * 3.6}deg` }"
                      >
                        <span>{{ item.remainingPercent }}%</span>
                      </div>
                      <div class="quota-ring-caption">
                        <strong>{{ item.label }}</strong>
                        <span>{{ formatQuotaResetTime(item.resetAt) ?? '未记录刷新时间' }}</span>
                      </div>
                    </div>
                  </div>
                </template>
                <div v-else class="card-quota-empty">暂无额度窗口</div>
              </div>
            </button>
          </div>
        </section>
      </div>

      <div class="display-control-row">
        <div class="display-control-copy">
          <span class="display-control-label">展示数量</span>
          <span class="display-control-help">{{ displaySizeHelpText }}</span>
        </div>
        <NSelect
          v-model:value="accountDisplaySize"
          class="display-size-select"
          size="small"
          :options="accountDisplaySizeOptions"
        />
      </div>
    </section>

    <NDrawer v-model:show="detailOpen" placement="right" :width="420">
      <NDrawerContent>
        <template #header>
          <div class="detail-drawer-header">
            <NButton quaternary size="small" class="detail-back-button" @click="detailOpen = false">
              <template #icon>
                <NIcon :component="ArrowLeft" />
              </template>
              返回
            </NButton>
            <span class="detail-drawer-title">账号详情</span>
          </div>
        </template>
        <NDescriptions v-if="selectedAccount" label-placement="left" :column="1" size="small" bordered>
          <NDescriptionsItem label="账号">{{ selectedAccount.name }}</NDescriptionsItem>
          <NDescriptionsItem label="邮箱">{{ selectedAccount.email ?? '-' }}</NDescriptionsItem>
          <NDescriptionsItem label="账号类型">
            {{ selectedAccount.account_type ?? '未知' }}
          </NDescriptionsItem>
          <NDescriptionsItem label="启用状态">
            {{ selectedAccount.disabled ? '已禁用' : '启用中' }}
          </NDescriptionsItem>
          <NDescriptionsItem label="当前优先级">
            {{ accountPriority(selectedAccount) }}
          </NDescriptionsItem>
          <NDescriptionsItem label="类型默认优先级">
            {{ defaultPriority(selectedAccount) ?? '-' }}
          </NDescriptionsItem>
          <NDescriptionsItem label="额度窗口">{{ quotaText(selectedAccount) }}</NDescriptionsItem>
          <NDescriptionsItem label="状态码">
            {{ selectedAccount.last_status_code ?? '-' }}
          </NDescriptionsItem>
          <NDescriptionsItem label="最近健康">
            {{ formatDateTime(selectedAccount.last_healthy_at) }}
          </NDescriptionsItem>
          <NDescriptionsItem label="最近巡检">
            {{ formatDateTime(selectedAccount.last_checked_at) }}
          </NDescriptionsItem>
          <NDescriptionsItem label="最近操作">
            {{ latestActionText(selectedAccount) }}
          </NDescriptionsItem>
        </NDescriptions>
        <div v-if="selectedAccount" class="detail-action-row">
          <NSpace :size="8" wrap>
            <NButton
              size="small"
              type="primary"
              secondary
              :disabled="isRowActing(selectedAccount) || isBulkDeleting || isBulkRefreshing"
              :loading="isActionLoading(selectedAccount, 'refresh')"
              @click="refreshAccount(selectedAccount, { closeDetail: true })"
            >
              刷新
            </NButton>
            <NButton
              v-if="selectedAccount.disabled"
              size="small"
              type="primary"
              secondary
              :disabled="isRowActing(selectedAccount) || isBulkDeleting || isBulkRefreshing"
              :loading="isActionLoading(selectedAccount, 'toggle')"
              @click="confirmEnableAccount(selectedAccount)"
            >
              启用
            </NButton>
            <NButton
              v-else
              size="small"
              type="warning"
              secondary
              :disabled="isRowActing(selectedAccount) || isBulkDeleting || isBulkRefreshing"
              :loading="isActionLoading(selectedAccount, 'toggle')"
              @click="confirmDisableAccount(selectedAccount)"
            >
              禁用
            </NButton>
            <NButton
              size="small"
              secondary
              :disabled="isRowActing(selectedAccount) || isBulkDeleting || isBulkRefreshing"
              :loading="isActionLoading(selectedAccount, 'priority')"
              @click="openPriorityDialog(selectedAccount)"
            >
              修改优先级
            </NButton>
            <NButton
              v-if="selectedAccount.disabled"
              size="small"
              type="error"
              secondary
              :disabled="isRowActing(selectedAccount) || isBulkDeleting || isBulkRefreshing"
              :loading="isActionLoading(selectedAccount, 'delete')"
              @click="confirmDeleteAccount(selectedAccount)"
            >
              删除
            </NButton>
          </NSpace>
        </div>
      </NDrawerContent>
    </NDrawer>

    <NModal
      v-model:show="accountConfirmDialog.show"
      preset="dialog"
      :title="accountConfirmDialog.title"
      :style="{ width: 'min(420px, calc(100vw - 32px))' }"
    >
      <p class="account-confirm-content">{{ accountConfirmDialog.content }}</p>
      <template #action>
        <NSpace justify="end">
          <NButton :disabled="isAccountConfirmSubmitting" @click="accountConfirmDialog.show = false">
            取消
          </NButton>
          <NButton
            :type="accountConfirmDialog.type"
            :loading="isAccountConfirmSubmitting"
            @click="submitAccountConfirm"
          >
            {{ accountConfirmDialog.positiveText }}
          </NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal
      v-model:show="bulkDeleteDialog.show"
      preset="dialog"
      title="批量删除已禁用账号"
      :style="{ width: 'min(460px, calc(100vw - 32px))' }"
    >
      <div class="bulk-delete-dialog">
        <p class="bulk-delete-warning">
          将删除已选 {{ selectedDisabledCount }} 个已禁用账号，并从 CPA 删除 auth file。此操作不可恢复。
        </p>
        <div v-if="bulkDeletePreviewNames.length > 0" class="bulk-delete-preview">
          <span v-for="name in bulkDeletePreviewNames" :key="name">{{ name }}</span>
          <span v-if="bulkDeletePreviewOverflow > 0">另 {{ bulkDeletePreviewOverflow }} 个...</span>
        </div>
      </div>
      <template #action>
        <NSpace justify="end">
          <NButton :disabled="isBulkDeleting" @click="bulkDeleteDialog.show = false">取消</NButton>
          <NButton
            type="error"
            :disabled="selectedDisabledCount === 0"
            :loading="isBulkDeleting"
            @click="submitBulkDelete"
          >
            确认删除
          </NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal
      v-model:show="priorityDialog.show"
      preset="dialog"
      :title="priorityDialogTitle"
      :style="{ width: 'min(460px, calc(100vw - 32px))' }"
    >
      <div class="priority-dialog">
        <NSelect
          :value="priorityDialog.mode"
          :options="priorityModeOptions"
          @update:value="(value) => setPriorityDialogMode(value as PriorityMode)"
        />
        <NInputNumber
          v-if="priorityDialog.mode !== 'default'"
          v-model:value="priorityDialog.value"
          :precision="0"
          v-bind="priorityDialogBounds"
        />
        <p class="priority-hint">{{ priorityDialogHint }}</p>
      </div>
      <template #action>
        <NSpace justify="end">
          <NButton @click="priorityDialog.show = false">取消</NButton>
          <NButton
            type="primary"
            :disabled="!canSubmitPriority"
            :loading="
              priorityDialog.account
                ? isActionLoading(priorityDialog.account, 'priority')
                : false
            "
            @click="submitPriorityDialog"
          >
            确认
          </NButton>
        </NSpace>
      </template>
    </NModal>
  </section>
</template>

<style scoped>
.account-status-page,
.account-list-panel,
.account-section,
.account-card-shell,
.account-table {
  min-width: 0;
}

.account-page-header {
  align-items: flex-start;
}

.account-header-copy {
  flex: 1;
  min-width: 0;
}

.account-header-title-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-width: 0;
}

.account-header-title-row .page-title {
  min-width: 0;
}

.header-actions {
  display: flex;
  flex-shrink: 0;
  justify-content: flex-end;
}

.account-metrics {
  grid-template-columns: repeat(6, minmax(112px, 1fr));
}

.account-metrics .metric-card {
  min-height: 104px;
  padding: 14px 12px;
}

.account-metrics .metric-action {
  width: 100%;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
  appearance: none;
}

.account-metrics .metric-action:hover {
  border-color: color-mix(in srgb, var(--metric-color, var(--cpa-primary)) 45%, var(--cpa-border));
  transform: translateY(-1px);
}

.account-metrics .metric-action:focus-visible {
  outline: 2px solid var(--metric-color, var(--cpa-primary));
  outline-offset: 3px;
}

.account-metrics .metric-action.is-active {
  border-color: color-mix(in srgb, var(--metric-color, var(--cpa-primary)) 65%, var(--cpa-border));
  box-shadow:
    0 0 0 3px color-mix(in srgb, var(--metric-color, var(--cpa-primary)) 16%, transparent),
    var(--cpa-shadow-card),
    var(--cpa-shadow-hairline);
}

.account-metrics .metric-value {
  font-size: 20px;
}

.status-toolbar {
  display: grid;
  gap: 12px;
  padding: 14px;
  border-bottom: 1px solid var(--cpa-border);
  background: var(--cpa-glass);
  backdrop-filter: blur(14px);
}

.toolbar-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-width: 0;
}

.toolbar-title {
  margin: 0;
  color: var(--cpa-text);
  font-size: 15px;
  font-weight: 700;
  line-height: 1.25;
}

.toolbar-subtitle {
  margin: 3px 0 0;
  color: var(--cpa-text-muted);
  font-size: 13px;
}

.filter-grid {
  display: grid;
  grid-template-columns: minmax(220px, 1.35fr) minmax(150px, 0.8fr) minmax(170px, 0.9fr);
  gap: 8px;
  min-width: 0;
}

.list-control-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  min-width: 0;
}

.list-main-controls {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.sort-control-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  min-width: 0;
}

.sort-control-label {
  color: var(--cpa-text-muted);
  font-size: 12px;
  font-weight: 700;
  white-space: nowrap;
}

.account-sections {
  display: grid;
  gap: 14px;
  padding: 14px;
}

.account-card-shell {
  padding: 14px;
}

.display-control-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 12px 14px;
  border-top: 1px solid var(--cpa-border);
  background: var(--cpa-surface-raised);
}

.display-control-copy {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px 10px;
  min-width: 0;
}

.display-control-label {
  color: var(--cpa-text-muted);
  font-size: 12px;
  font-weight: 600;
  white-space: nowrap;
}

.display-control-help {
  min-width: 0;
  color: var(--cpa-text-muted);
  font-size: 12px;
}

.display-size-select {
  flex-shrink: 0;
  width: 112px;
}

.account-section {
  display: grid;
  gap: 10px;
}

.account-section + .account-section {
  padding-top: 12px;
  border-top: 1px solid var(--cpa-border);
}

.account-section-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  min-width: 0;
}

.account-section-title-group {
  min-width: 0;
}

.account-section-title {
  margin: 0;
  color: var(--cpa-text);
  font-size: 14px;
  font-weight: 700;
  line-height: 1.25;
}

.account-section-subtitle {
  margin: 3px 0 0;
  color: var(--cpa-text-muted);
  font-size: 12px;
}

.account-section-actions {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: flex-end;
}

.account-card-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
  gap: 10px;
}

.account-card {
  display: grid;
  gap: 12px;
  min-width: 0;
  min-height: 176px;
  padding: 12px;
  color: var(--cpa-text);
  font: inherit;
  text-align: left;
  cursor: pointer;
  appearance: none;
  content-visibility: auto;
  contain-intrinsic-size: 220px;
  background: var(--cpa-surface-raised);
  border: 1px solid var(--cpa-border);
  border-radius: var(--cpa-radius);
  box-shadow: var(--cpa-shadow-card), var(--cpa-shadow-hairline);
}

.account-card:hover {
  border-color: color-mix(in srgb, var(--cpa-primary) 36%, var(--cpa-border));
  transform: translateY(-1px);
}

.account-card.is-select-mode {
  background: color-mix(in srgb, var(--cpa-primary) 5%, var(--cpa-surface-raised));
}

.account-card.is-selected {
  background: color-mix(in srgb, var(--cpa-primary) 12%, var(--cpa-surface-raised));
  border-color: color-mix(in srgb, var(--cpa-primary) 64%, var(--cpa-border));
  box-shadow:
    0 0 0 3px color-mix(in srgb, var(--cpa-primary) 14%, transparent),
    var(--cpa-shadow-card),
    var(--cpa-shadow-hairline);
}

.account-card:focus-visible {
  outline: 2px solid color-mix(in srgb, var(--cpa-primary) 70%, transparent);
  outline-offset: 2px;
}

.account-card.has-error {
  border-color: color-mix(in srgb, var(--cpa-danger) 42%, var(--cpa-border));
}

.account-card-top {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
  min-width: 0;
}

.account-card-identity {
  display: grid;
  gap: 3px;
  min-width: 0;
}

.account-card-email,
.account-card-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.account-card-email {
  color: var(--cpa-text-strong);
  font-size: 14px;
  font-weight: 700;
}

.account-card-name {
  color: var(--cpa-text-muted);
  font-size: 12px;
}

.account-status-pill {
  flex-shrink: 0;
  padding: 2px 8px;
  font-size: 12px;
  font-weight: 700;
  line-height: 1.45;
  border: 1px solid transparent;
  border-radius: var(--cpa-radius-sm);
}

.account-status-pill.is-success {
  color: var(--cpa-success);
  background: var(--cpa-success-weak);
  border-color: color-mix(in srgb, var(--cpa-success) 28%, transparent);
}

.account-status-pill.is-warning {
  color: var(--cpa-warning);
  background: var(--cpa-warning-weak);
  border-color: color-mix(in srgb, var(--cpa-warning) 28%, transparent);
}

.account-card-meta-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
}

.account-card-meta-item {
  display: grid;
  gap: 2px;
  min-width: 0;
  padding: 7px 8px;
  background: var(--cpa-surface-muted);
  border: 1px solid color-mix(in srgb, var(--cpa-border) 70%, transparent);
  border-radius: var(--cpa-radius-sm);
}

.account-card-meta-item span {
  overflow: hidden;
  color: var(--cpa-text-muted);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.account-card-meta-item strong {
  min-width: 0;
  overflow: hidden;
  color: var(--cpa-text);
  font-size: 12px;
  font-weight: 700;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.account-card-quota {
  display: grid;
  gap: 8px;
  min-width: 0;
}

.card-quota-bar {
  display: grid;
  gap: 5px;
  min-width: 0;
}

.card-quota-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
  min-width: 0;
}

.card-quota-head span,
.card-quota-reset {
  overflow: hidden;
  color: var(--cpa-text-muted);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.card-quota-head strong {
  flex-shrink: 0;
  color: var(--cpa-text);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}

.card-quota-track {
  height: 6px;
  overflow: hidden;
  background: var(--cpa-surface-muted);
  border-radius: 999px;
}

.card-quota-fill {
  height: 100%;
  min-width: 3px;
  border-radius: inherit;
}

.card-quota-fill.is-healthy,
.quota-ring.is-healthy {
  --quota-color: var(--cpa-success);
}

.card-quota-fill.is-warning,
.quota-ring.is-warning {
  --quota-color: var(--cpa-warning);
}

.card-quota-fill.is-danger,
.quota-ring.is-danger {
  --quota-color: var(--cpa-danger);
}

.card-quota-fill {
  background: var(--quota-color, var(--cpa-success));
}

.card-quota-rings {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.card-quota-ring-item {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  align-items: center;
  gap: 8px;
  min-width: 132px;
  flex: 1 1 132px;
}

.quota-ring {
  --quota-color: var(--cpa-success);
  display: grid;
  position: relative;
  width: 54px;
  height: 54px;
  flex-shrink: 0;
  place-items: center;
  overflow: hidden;
  background:
    conic-gradient(
      var(--quota-color) var(--quota-deg),
      color-mix(in srgb, var(--cpa-text-muted) 18%, transparent) 0
    );
  border-radius: 50%;
}

.quota-ring::before {
  position: absolute;
  inset: 6px;
  content: "";
  background: var(--cpa-surface-raised);
  border-radius: inherit;
}

.quota-ring span {
  position: relative;
  color: var(--cpa-text-strong);
  font-size: 11px;
  font-weight: 800;
  font-variant-numeric: tabular-nums;
}

.quota-ring-caption {
  display: grid;
  gap: 2px;
  min-width: 0;
}

.quota-ring-caption strong,
.quota-ring-caption span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.quota-ring-caption strong {
  color: var(--cpa-text);
  font-size: 12px;
}

.quota-ring-caption span,
.card-quota-empty {
  color: var(--cpa-text-muted);
  font-size: 11px;
}

.card-quota-empty {
  padding: 10px;
  text-align: center;
  background: var(--cpa-surface-muted);
  border: 1px dashed var(--cpa-border);
  border-radius: var(--cpa-radius-sm);
}

.detail-action-row {
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--cpa-border);
}

.detail-drawer-header {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.detail-back-button {
  flex-shrink: 0;
}

.detail-drawer-title {
  min-width: 0;
  overflow: hidden;
  color: var(--cpa-text-strong);
  font-size: 16px;
  font-weight: 700;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.account-table :deep(.n-data-table-th) {
  white-space: nowrap;
}

.account-table :deep(.n-data-table-td) {
  vertical-align: middle;
}

.account-table :deep(.n-data-table-tr.is-refresh-selectable) {
  cursor: pointer;
}

.account-table :deep(.n-data-table-tr.is-refresh-selected .n-data-table-td) {
  background: color-mix(in srgb, var(--cpa-primary) 12%, var(--cpa-surface-raised));
}

.account-table :deep(.n-data-table-tr.is-refresh-selected:hover .n-data-table-td) {
  background: color-mix(in srgb, var(--cpa-primary) 16%, var(--cpa-surface-raised));
}

:global(.quota-window-cell) {
  display: grid;
  gap: 8px;
  min-width: 0;
  padding: 4px 0;
}

:global(.quota-window-item) {
  display: grid;
  gap: 4px;
  min-width: 0;
}

:global(.quota-window-head) {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  min-width: 0;
  line-height: 1.2;
}

:global(.quota-window-label) {
  min-width: 0;
  overflow: hidden;
  color: var(--cpa-text);
  font-size: 12px;
  font-weight: 700;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:global(.quota-window-meta) {
  display: inline-flex;
  flex-shrink: 0;
  align-items: baseline;
  gap: 6px;
  min-width: 0;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

:global(.quota-window-percent) {
  color: var(--cpa-text);
  font-size: 12px;
  font-weight: 700;
}

:global(.quota-window-reset) {
  color: var(--cpa-text-muted);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}

:global(.quota-window-track) {
  height: 5px;
  overflow: hidden;
  background: var(--cpa-surface-muted);
  border-radius: 999px;
}

:global(.quota-window-fill) {
  height: 100%;
  min-width: 3px;
  border-radius: inherit;
}

:global(.quota-window-fill.is-healthy) {
  background: var(--cpa-success);
}

:global(.quota-window-fill.is-warning) {
  background: var(--cpa-warning);
}

:global(.quota-window-fill.is-danger) {
  background: var(--cpa-danger);
}

:global(.latest-action-text) {
  display: block;
  min-width: 0;
  overflow: hidden;
  color: var(--cpa-text);
  text-overflow: ellipsis;
  white-space: nowrap;
}

:global(.account-actions) {
  justify-content: flex-end;
}

.empty-state {
  padding: 42px 0;
  color: var(--cpa-text-muted);
  font-size: 13px;
  text-align: center;
}

.bulk-delete-dialog,
.priority-dialog {
  display: grid;
  gap: 8px;
}

.account-confirm-content {
  margin: 0;
  overflow-wrap: anywhere;
  color: var(--cpa-text);
  font-size: 13px;
  line-height: 1.6;
}

.bulk-delete-warning,
.priority-hint {
  margin: 0;
  color: var(--cpa-text-muted);
  font-size: 13px;
}

.bulk-delete-preview {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  max-height: 116px;
  padding: 8px;
  overflow: auto;
  color: var(--cpa-text);
  font-size: 12px;
  background: var(--cpa-surface-muted);
  border: 1px solid var(--cpa-border);
  border-radius: var(--cpa-radius-sm);
}

.bulk-delete-preview span {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 1280px) {
  .account-metrics {
    grid-template-columns: repeat(3, minmax(112px, 1fr));
  }
}

@media (max-width: 980px) {
  .filter-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .list-main-controls,
  .list-control-row,
  .sort-control-row {
    justify-content: flex-start;
  }
}

@media (max-width: 560px) {
  .account-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .account-page-header {
    align-items: stretch;
    flex-direction: row;
  }

  .account-header-title-row {
    gap: 8px;
  }

  .toolbar-heading,
  .account-section-header {
    align-items: flex-start;
    flex-direction: column;
  }

  .account-section-actions {
    width: 100%;
    justify-content: flex-start;
  }

  .sort-control-row {
    width: 100%;
  }

  .list-main-controls {
    width: 100%;
  }

  .filter-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .filter-grid .n-input {
    grid-column: 1 / -1;
  }

  .account-card-grid {
    grid-template-columns: 1fr;
  }

  .account-card-top {
    align-items: flex-start;
    flex-direction: column;
  }

  .account-card-meta-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
