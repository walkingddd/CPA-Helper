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
  NPagination,
  NSelect,
  NSpace,
  NTag,
  useMessage,
  type DataTableColumns,
  type DataTableRowKey,
} from 'naive-ui'
import {
  Activity,
  ArrowLeft,
  BarChart3,
  ChevronDown,
  CircleDot,
  Gauge,
  PauseCircle,
  RefreshCw,
  ShieldAlert,
  ShieldCheck,
  Table2,
  Trash2,
  Users,
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
import type {
  CodexKeeperAccount,
  CodexKeeperPriorityRule,
  CodexKeeperQuotaWindowUsage,
  CodexKeeperStatus,
} from '@/shared/types/api'
import { useI18n } from '@/shared/i18n'
import {
  BEIJING_TIME_ZONE,
  formatCompact,
  formatDateTime,
  formatInteger,
  formatUsd,
} from '@/shared/utils/format'

type FixedPriorityFilter = 'all' | 'high' | 'minusOne' | 'low'
type PriorityTypeFilter = `type:${string}`
type PriorityFilter = FixedPriorityFilter | PriorityTypeFilter
type AccountStatusFilter = 'all' | 'enabled' | 'disabled' | 'unauthorized' | 'quotaExhausted'
type AccountDisplaySize = 50 | 100 | 150 | 200 | 'all'
type AccountListViewMode = 'table' | 'bar' | 'ring'
type AccountSortKey = 'quotaDay' | 'quotaWeek' | 'accountType' | 'status' | 'priority' | 'lastCheckedAt'
type SortDirection = 'asc' | 'desc'
type PriorityMode = 'low' | 'high' | 'default'
type AccountAction = 'toggle' | 'priority' | 'delete' | 'refresh'
type AccountConfirmType = 'default' | 'warning' | 'error' | 'primary'
type QuotaWindowItem = {
  label: string
  remainingPercent: number
  resetAt: string | null
  usage: CodexKeeperQuotaWindowUsage | null
}
type QuotaUsageTag = { label: string; value: string; tone?: 'stale' }
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
const CODEX_FIVE_HOUR_WINDOW_SECONDS = 5 * 60 * 60
const CODEX_WEEK_WINDOW_SECONDS = 7 * 24 * 60 * 60
const CODEX_MONTH_WINDOW_SECONDS = 30 * 24 * 60 * 60
const disabledTableScrollX = 1302
const normalTableScrollX = 1816
const KEEPER_STATUS_POLL_INTERVAL_MS = 3000
const REFRESH_STATUS_POLL_INTERVAL_MS = 1500
const message = useMessage()
const { currentLanguage, errorText, keeperStatusText, serverText, t } = useI18n()
const isLoading = ref(false)
const isBulkDeleting = ref(false)
const isBulkRefreshing = ref(false)
const actingActions = ref<Set<string>>(new Set())
const accounts = ref<CodexKeeperAccount[]>([])
const priorityRules = ref<CodexKeeperPriorityRule[]>([])
const keeperStatus = ref<CodexKeeperStatus | null>(null)
const selectedAccount = ref<CodexKeeperAccount | null>(null)
const selectedDisabledAccountKeys = ref<DataTableRowKey[]>([])
const refreshSelectMode = ref(false)
const selectedRefreshAccountNames = ref<string[]>([])
const detailOpen = ref(false)
const accountDisplaySize = ref<AccountDisplaySize>(50)
const disabledAccountPage = ref(1)
const normalAccountPage = ref(1)
const cardAccountPage = ref(1)
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
  source: 'selection' as 'selection' | 'disabled401',
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
let keeperStatusTimer: number | undefined

const priorityRuleMap = computed(() =>
  Object.fromEntries(priorityRules.value.map((rule) => [rule.account_type, rule.priority])),
)
const priorityFilterOptions = computed<Array<{ label: string; value: PriorityFilter }>>(() => [
  { label: t('全部优先级', 'All Priorities'), value: 'all' },
  { label: t('手动优先 >20', 'Manual Priority >20'), value: 'high' },
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
  { label: t('临时降级', 'Temporary Downgrade'), value: 'minusOne' },
  { label: t('手动低优先 <-1', 'Manual Low Priority <-1'), value: 'low' },
])
const accountDisplaySizeOptions = computed<Array<{ label: string; value: AccountDisplaySize }>>(() => [
  { label: '50', value: 50 },
  { label: '100', value: 100 },
  { label: '150', value: 150 },
  { label: '200', value: 200 },
  { label: t('全部', 'All'), value: 'all' },
])
const accountListViewOptions = computed(() => [
  { label: t('表格', 'Table'), key: 'table', icon: () => h(NIcon, null, { default: () => h(Table2) }) },
  { label: t('进度条卡片', 'Progress Cards'), key: 'bar', icon: () => h(NIcon, null, { default: () => h(BarChart3) }) },
  { label: t('圆环卡片', 'Ring Cards'), key: 'ring', icon: () => h(NIcon, null, { default: () => h(CircleDot) }) },
])
const quotaSortOptions = computed(() => [
  { label: t('天', 'Day'), key: 'quotaDay' },
  { label: t('月/周', 'Month/Week'), key: 'quotaWeek' },
])

const accountTypeOptions = computed(() =>
  [...new Set(accounts.value.map((item) => item.account_type).filter(Boolean))]
    .sort((a, b) => String(a).localeCompare(String(b)))
    .map((value) => ({ label: accountTypeLabel(String(value)), value: String(value) })),
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
const showDisabledSection = computed(
  () => filters.status !== 'enabled' && filteredDisabledAccounts.value.length > 0,
)
const showNormalSection = computed(
  () => filters.status !== 'disabled' && filteredNormalAccounts.value.length > 0,
)
const showTableLoadingState = computed(() => isLoading.value && accounts.value.length === 0)
const showEmptyTableState = computed(
  () => !showTableLoadingState.value && !showDisabledSection.value && !showNormalSection.value,
)
const isKeeperRunning = computed(() => keeperStatus.value?.running === true)
const isKeeperDaemonRunning = computed(() => keeperStatus.value?.daemon_running === true)
const keeperStateType = computed(() => {
  if (isKeeperRunning.value || isKeeperDaemonRunning.value) {
    return 'success'
  }
  if (keeperStatus.value?.state === 'error' || keeperStatus.value?.state === 'failed') {
    return 'error'
  }
  if (keeperStatus.value?.state === 'stopping') {
    return 'warning'
  }
  return 'default'
})
const keeperStatusDetailText = computed(() => {
  const detail = keeperStatus.value?.detail
  if (isKeeperDaemonRunning.value && !isKeeperRunning.value) {
    return t('自动巡检已开启', 'Automatic inspection is enabled')
  }
  if (!detail) {
    return t('未运行', 'Not running')
  }
  return keeperStatusText(detail)
})
const keeperStatusFootnoteText = computed(() =>
  isKeeperDaemonRunning.value ? t('等待 Cron 调度', 'Waiting for Cron schedule') : t('后台自动巡检', 'Background automatic inspection'),
)
const unauthorizedErrorAccountCount = computed(
  () => accounts.value.filter((account) => account.last_status_code === 401).length,
)
const quotaExhaustedAccountCount = computed(
  () => accounts.value.filter(isQuotaExhaustedAccount).length,
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
    return t('进度条卡片', 'Progress Cards')
  }
  if (accountListViewMode.value === 'ring') {
    return t('圆环卡片', 'Ring Cards')
  }
  return t('表格', 'Table')
})
const sortedCardAccounts = computed(() => [
  ...filteredDisabledAccounts.value,
  ...filteredNormalAccounts.value,
])
const isDisplayAllAccounts = computed(() => accountDisplaySize.value === 'all')
const disabledTableDisplayProps = computed(() =>
  accountTableDisplayProps(visibleDisabledAccounts.value.length),
)
const normalTableDisplayProps = computed(() =>
  accountTableDisplayProps(visibleNormalAccounts.value.length),
)
const accountPaginationPageSize = computed(() =>
  accountDisplaySize.value === 'all' ? 1 : accountDisplaySize.value,
)
const disabledAccountPageCount = computed(() => accountPageCount(filteredDisabledAccounts.value.length))
const normalAccountPageCount = computed(() => accountPageCount(filteredNormalAccounts.value.length))
const cardAccountPageCount = computed(() => accountPageCount(sortedCardAccounts.value.length))
const showDisabledPagination = computed(() =>
  shouldShowAccountPagination(filteredDisabledAccounts.value.length),
)
const showNormalPagination = computed(() =>
  shouldShowAccountPagination(filteredNormalAccounts.value.length),
)
const showCardPagination = computed(() =>
  shouldShowAccountPagination(sortedCardAccounts.value.length),
)
const visibleDisabledAccounts = computed(() =>
  pagedAccounts(filteredDisabledAccounts.value, disabledAccountPage.value),
)
const visibleNormalAccounts = computed(() =>
  pagedAccounts(filteredNormalAccounts.value, normalAccountPage.value),
)
const visibleCardAccounts = computed(() =>
  pagedAccounts(sortedCardAccounts.value, cardAccountPage.value),
)
const disabledSectionDisplayText = computed(() =>
  accountDisplayText(
    visibleDisabledAccounts.value.length,
    filteredDisabledAccounts.value.length,
    disabledAccountPage.value,
    disabledAccountPageCount.value,
  ),
)
const normalSectionDisplayText = computed(() =>
  accountDisplayText(
    visibleNormalAccounts.value.length,
    filteredNormalAccounts.value.length,
    normalAccountPage.value,
    normalAccountPageCount.value,
  ),
)
const cardSectionDisplayText = computed(() =>
  accountDisplayText(
    visibleCardAccounts.value.length,
    sortedCardAccounts.value.length,
    cardAccountPage.value,
    cardAccountPageCount.value,
  ),
)
const showCardLoadingState = computed(() => isLoading.value && accounts.value.length === 0)
const displaySizeHelpText = computed(() =>
  isTableView.value
    ? isDisplayAllAccounts.value
      ? t('当前筛选结果全部展示，账号较多时自动使用虚拟滚动。', 'All filtered results are shown. Virtual scrolling is used automatically for large account sets.')
      : t(`每个分组每页显示 ${accountDisplaySize.value} 个账号。`, `${accountDisplaySize.value} accounts per group per page.`)
    : isDisplayAllAccounts.value
      ? t('当前筛选结果全部以卡片展示，账号较多时使用轻量渲染优化。', 'All filtered results are shown as cards. Lightweight rendering is used for large account sets.')
      : t(`统一列表每页显示 ${accountDisplaySize.value} 个账号。`, `${accountDisplaySize.value} accounts per page in the unified list.`),
)
const activeQuotaSortLabel = computed(() => {
  if (accountSort.key === 'quotaDay') {
    return t('天', 'Day')
  }
  if (accountSort.key === 'quotaWeek') {
    return t('月/周', 'Month/Week')
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

function accountPageCount(rowCount: number): number {
  if (accountDisplaySize.value === 'all') {
    return 1
  }
  return Math.max(1, Math.ceil(rowCount / accountDisplaySize.value))
}

function shouldShowAccountPagination(rowCount: number): boolean {
  return accountDisplaySize.value !== 'all' && rowCount > accountDisplaySize.value
}

function pagedAccounts(source: CodexKeeperAccount[], page: number): CodexKeeperAccount[] {
  if (accountDisplaySize.value === 'all') {
    return source
  }
  const safePage = clampPage(page, accountPageCount(source.length))
  const start = (safePage - 1) * accountDisplaySize.value
  return source.slice(start, start + accountDisplaySize.value)
}

function accountDisplayText(
  visibleCount: number,
  totalCount: number,
  page: number,
  pageCount: number,
): string {
  if (isDisplayAllAccounts.value) {
    return t(`显示 ${visibleCount} / ${totalCount} 个账号`, `Showing ${visibleCount} / ${totalCount} accounts`)
  }
  return t(
    `第 ${clampPage(page, pageCount)} / ${pageCount} 页，显示 ${visibleCount} / ${totalCount} 个账号`,
    `Page ${clampPage(page, pageCount)} / ${pageCount}, showing ${visibleCount} / ${totalCount} accounts`,
  )
}

function clampPage(page: number, pageCount: number): number {
  return Math.min(Math.max(1, page), pageCount)
}

function resetAccountPages() {
  disabledAccountPage.value = 1
  normalAccountPage.value = 1
  cardAccountPage.value = 1
}

function clampAccountPages() {
  disabledAccountPage.value = clampPage(disabledAccountPage.value, disabledAccountPageCount.value)
  normalAccountPage.value = clampPage(normalAccountPage.value, normalAccountPageCount.value)
  cardAccountPage.value = clampPage(cardAccountPage.value, cardAccountPageCount.value)
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
    // Keep the page usable when local storage is unavailable.
  }
}
const selectedDisabledAccountNames = computed(() =>
  selectedDisabledAccountKeys.value.map((key) => String(key)),
)
const filteredUnauthorizedDisabledAccounts = computed(() =>
  filteredDisabledAccounts.value.filter((account) => account.last_status_code === 401),
)
const filteredUnauthorizedDisabledAccountNames = computed(() =>
  filteredUnauthorizedDisabledAccounts.value.map((account) => account.name),
)
const selectedDisabledCount = computed(() => selectedDisabledAccountNames.value.length)
const canBulkDelete = computed(() => selectedDisabledCount.value > 0 && !isBulkDeleting.value)
const canBulkDeleteFilteredUnauthorizedDisabledAccounts = computed(
  () =>
    filteredUnauthorizedDisabledAccountNames.value.length > 0 &&
    !isBulkDeleting.value &&
    !isBulkRefreshing.value &&
    !isLoading.value,
)
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
const bulkDeleteDialogTitle = computed(() =>
  bulkDeleteDialog.source === 'disabled401'
    ? t('批量删除 401 已禁用账号', 'Bulk Delete 401 Disabled Accounts')
    : t('批量删除已禁用账号', 'Bulk Delete Disabled Accounts'),
)
const bulkDeleteWarningText = computed(() =>
  bulkDeleteDialog.source === 'disabled401'
    ? t(
        `将删除当前筛选下 ${selectedDisabledCount.value} 个 HTTP 401 已禁用账号，并从 CPA 删除 auth file。此操作不可恢复。`,
        `This will delete ${selectedDisabledCount.value} HTTP 401 disabled accounts in the current filter and remove their auth files from CPA. This cannot be undone.`,
      )
    : t(
        `将删除已选 ${selectedDisabledCount.value} 个已禁用账号，并从 CPA 删除 auth file。此操作不可恢复。`,
        `This will delete ${selectedDisabledCount.value} selected disabled accounts and remove their auth files from CPA. This cannot be undone.`,
      ),
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
const priorityDialogTitle = computed(() => t('修改优先级', 'Change Priority'))
const priorityDialogHint = computed(() => {
  if (priorityDialog.mode === 'low') {
    return t('手动低优先级必须小于 -1，巡检永远不会自动调整。', 'Manual low priority must be less than -1. Inspection will never adjust it automatically.')
  }
  if (priorityDialog.mode === 'high') {
    return t('手动优先必须大于 20，额度耗尽时会临时降为 -1，恢复后回到该值。', 'Manual priority must be greater than 20. When quota is exhausted it is temporarily lowered to -1 and restored to this value after recovery.')
  }
  const account = priorityDialog.account
  const value = account ? defaultPriority(account) : null
  return value === null
    ? t('该账号类型没有配置默认优先级，不能使用类型默认值。', 'This account type has no default priority, so the type default cannot be used.')
    : t(`将优先级设置为当前账号类型默认值 ${value}。`, `Set the priority to the current account type default: ${value}.`)
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
    { label: t('手动低优先 (< -1)', 'Manual Low Priority (< -1)'), value: 'low' },
    { label: t('手动优先 (> 20)', 'Manual Priority (> 20)'), value: 'high' },
    {
      label: defaultValue === null
        ? t('类型默认优先级（不可用）', 'Type Default Priority (unavailable)')
        : t(`类型默认优先级 (${defaultValue})`, `Type Default Priority (${defaultValue})`),
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
  if (value === 'unauthorized') {
    return account.last_status_code === 401
  }
  if (value === 'quotaExhausted') {
    return isQuotaExhaustedAccount(account)
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

function isQuotaExhaustedAccount(account: CodexKeeperAccount): boolean {
  return !account.disabled && accountPriority(account) === -1
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
  return compareAccountFileName(left, right)
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
  return result === 0 ? compareAccountFileName(left, right) : result
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

function compareAccountFileName(left: CodexKeeperAccount, right: CodexKeeperAccount): number {
  return left.name.localeCompare(right.name)
}

function defaultPriority(account: CodexKeeperAccount): number | null {
  if (!account.account_type) {
    return null
  }
  return priorityRuleMap.value[account.account_type] ?? null
}

function accountTypeLabel(accountType: string | null): string {
  const normalized = accountType?.trim().toLowerCase()
  if (!normalized || normalized === 'unknown') {
    return t('未知', 'Unknown')
  }
  if (normalized === 'k12') {
    return 'K12'
  }
  return accountType ?? normalized
}

function isPaidQuotaWindowAccount(accountType: string | null): boolean {
  const normalized = accountType?.trim().toLowerCase()
  return normalized === 'plus' || normalized === 'team' || normalized === 'k12' || normalized?.startsWith('pro') === true
}

function isFreeQuotaWindowAccount(accountType: string | null): boolean {
  return accountType?.trim().toLowerCase() === 'free'
}

function quotaWindowSecondsFor(account: CodexKeeperAccount, window: 'primary' | 'secondary'): number | null {
  if (window === 'primary') {
    return account.primary_window_seconds ?? account.primary_window_usage?.window_seconds ?? null
  }
  return account.secondary_window_seconds ?? account.secondary_window_usage?.window_seconds ?? null
}

function isPaidQuotaWindow(account: CodexKeeperAccount): boolean {
  return (
    isPaidQuotaWindowAccount(account.account_type) ||
    (quotaWindowSecondsFor(account, 'primary') === CODEX_FIVE_HOUR_WINDOW_SECONDS &&
      quotaWindowSecondsFor(account, 'secondary') === CODEX_WEEK_WINDOW_SECONDS)
  )
}

function isFreeQuotaWindow(account: CodexKeeperAccount): boolean {
  return (
    isFreeQuotaWindowAccount(account.account_type) ||
    (quotaWindowSecondsFor(account, 'primary') === CODEX_MONTH_WINDOW_SECONDS &&
      quotaWindowSecondsFor(account, 'secondary') === null)
  )
}

function quotaWindowLabels(account: CodexKeeperAccount): { primary: string; secondary: string } {
  if (isFreeQuotaWindow(account)) {
    return { primary: t('月限额', 'Monthly Limit'), secondary: t('次限额', 'Secondary Limit') }
  }
  if (isPaidQuotaWindow(account)) {
    // The upstream usage payload decides the real window length; the plan
    // type alone does not. Pro accounts, for example, can report a weekly
    // primary window, so label by actual seconds and only fall back to the
    // conventional 5-hour/weekly pair when the seconds are unknown.
    const primaryLabel = quotaWindowLabelForSeconds(quotaWindowSecondsFor(account, 'primary'))
    const secondaryLabel = quotaWindowLabelForSeconds(quotaWindowSecondsFor(account, 'secondary'))
    return {
      primary: primaryLabel ?? t('5小时限额', '5-Hour Limit'),
      secondary: secondaryLabel ?? t('周限额', 'Weekly Limit'),
    }
  }
  return { primary: t('主', 'Primary'), secondary: t('次', 'Secondary') }
}

function quotaWindowLabelForSeconds(seconds: number | null): string | null {
  if (seconds === CODEX_FIVE_HOUR_WINDOW_SECONDS) {
    return t('5小时限额', '5-Hour Limit')
  }
  if (seconds === CODEX_WEEK_WINDOW_SECONDS) {
    return t('周限额', 'Weekly Limit')
  }
  if (seconds === CODEX_MONTH_WINDOW_SECONDS) {
    return t('月限额', 'Monthly Limit')
  }
  return null
}

function shouldShowQuotaWindow(account: CodexKeeperAccount): boolean {
  return !account.disabled
}

function quotaWindowItems(account: CodexKeeperAccount): QuotaWindowItem[] {
  if (!shouldShowQuotaWindow(account) || account.primary_used_percent === null) {
    return []
  }
  const labels = quotaWindowLabels(account)
  const items = [
    {
      label: labels.primary,
      remainingPercent: remainingQuotaPercent(account.primary_used_percent),
      resetAt: account.primary_reset_at,
      usage: account.primary_window_usage,
    },
  ]
  if (account.secondary_used_percent !== null && !isFreeQuotaWindow(account)) {
    items.push({
      label: labels.secondary,
      remainingPercent: remainingQuotaPercent(account.secondary_used_percent),
      resetAt: account.secondary_reset_at,
      usage: account.secondary_window_usage,
    })
  }
  return items
}

function quotaSortRemainingPercent(account: CodexKeeperAccount, key: AccountSortKey): number | null {
  if (!shouldShowQuotaWindow(account)) {
    return null
  }
  if (key === 'quotaDay') {
    if (isFreeQuotaWindow(account)) {
      return null
    }
    return nullableRemainingQuotaPercent(account.primary_used_percent)
  }
  if (key === 'quotaWeek') {
    if (isFreeQuotaWindow(account)) {
      return nullableRemainingQuotaPercent(account.primary_used_percent)
    }
    if (isPaidQuotaWindow(account)) {
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
  return new Intl.DateTimeFormat(currentLanguage.value === 'zh' ? 'zh-CN' : 'en-US', {
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
      const usageText = quotaWindowUsageText(item)
      return resetTime
        ? t(
            `${item.label}剩余 ${item.remainingPercent}%，刷新 ${resetTime}，${usageText}`,
            `${item.label} ${item.remainingPercent}% remaining, refreshes ${resetTime}, ${usageText}`,
          )
        : t(
            `${item.label}剩余 ${item.remainingPercent}%，${usageText}`,
            `${item.label} ${item.remainingPercent}% remaining, ${usageText}`,
          )
    })
    .join(' / ')
}

function quotaWindowUsageText(item: QuotaWindowItem): string {
  if (!item.resetAt || item.usage?.stale === true) {
    return t('额度数据需刷新', 'Quota data needs refresh')
  }
  if (!item.usage) {
    return t('本窗口暂无用量', 'No usage in this window')
  }
  return t(
    `${formatInteger(item.usage.records)} 次 / ${formatCompact(item.usage.total_tokens)} Tokens / ${formatUsd(item.usage.estimated_cost_usd)}`,
    `${formatInteger(item.usage.records)} requests / ${formatCompact(item.usage.total_tokens)} Tokens / ${formatUsd(item.usage.estimated_cost_usd)}`,
  )
}

function quotaWindowUsageTags(item: QuotaWindowItem): QuotaUsageTag[] {
  if (!item.resetAt || item.usage?.stale === true) {
    return [{ label: t('状态', 'Status'), value: t('需刷新', 'Needs refresh'), tone: 'stale' }]
  }
  const usage = item.usage
  return [
    { label: t('请求', 'Requests'), value: formatInteger(usage?.records ?? 0) },
    { label: 'Tokens', value: formatCompact(usage?.total_tokens ?? 0) },
    { label: t('费用', 'Cost'), value: formatUsd(usage?.estimated_cost_usd ?? 0) },
  ]
}

function quotaWindowResetText(item: QuotaWindowItem): string {
  const resetTime = formatQuotaResetTime(item.resetAt)
  return resetTime ? t(`刷新 ${resetTime}`, `Refreshes ${resetTime}`) : t('未记录刷新时间', 'No refresh time recorded')
}

function quotaWindowUsageTitle(item: QuotaWindowItem): string {
  const usage = item.usage
  if (!item.resetAt || usage?.stale === true) {
    return t(`${item.label} 额度数据需刷新`, `${item.label} quota data needs refresh`)
  }
  if (!usage) {
    return t(`${item.label} 本窗口暂无用量`, `${item.label} has no usage in this window`)
  }
  const unpriced =
    usage.unpriced_records > 0
      ? t(`，未计价 ${formatInteger(usage.unpriced_records)} 条`, `, ${formatInteger(usage.unpriced_records)} unpriced records`)
      : ''
  return t(
    `${item.label} 当前窗口：${formatInteger(usage.records)} 次请求，${formatCompact(usage.total_tokens)} Tokens，${formatUsd(usage.estimated_cost_usd)}${unpriced}`,
    `${item.label} current window: ${formatInteger(usage.records)} requests, ${formatCompact(usage.total_tokens)} Tokens, ${formatUsd(usage.estimated_cost_usd)}${unpriced}`,
  )
}

function quotaWindowUsageTone(item: QuotaWindowItem): string {
  return !item.resetAt || item.usage?.stale === true ? 'is-stale' : ''
}

function latestActionText(account: CodexKeeperAccount): string {
  const text = account.last_error?.trim() || account.latest_action?.trim()
  return text ? serverText(text, '账号状态', 'Account status') : '-'
}

function disabledCardErrorText(account: CodexKeeperAccount): string {
  if (!account.disabled) {
    return ''
  }
  const text =
    account.last_error?.trim() ||
    account.latest_action?.trim() ||
    disabledStatusCodeTitle(account)
  return text ? serverText(text, '报错信息', 'Error details') : t('暂无报错信息', 'No error details')
}

function disabledStatusCodeText(account: CodexKeeperAccount): string | null {
  if (!account.disabled || account.last_status_code == null) {
    return null
  }
  return `${account.last_status_code}`
}

function disabledStatusCodeTitle(account: CodexKeeperAccount): string | null {
  const text = disabledStatusCodeText(account)
  return text === null ? null : `HTTP ${text}`
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
            ? t(
                `${item.label}剩余 ${item.remainingPercent}%，刷新 ${resetTime}；${quotaWindowUsageTitle(item)}`,
                `${item.label} ${item.remainingPercent}% remaining, refreshes ${resetTime}; ${quotaWindowUsageTitle(item)}`,
              )
            : t(
                `${item.label}剩余 ${item.remainingPercent}%；${quotaWindowUsageTitle(item)}`,
                `${item.label} ${item.remainingPercent}% remaining; ${quotaWindowUsageTitle(item)}`,
              ),
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

function renderQuotaUsageCell(account: CodexKeeperAccount) {
  const items = quotaWindowItems(account)
  if (items.length === 0) {
    return '-'
  }
  return h(
    'div',
    { class: 'quota-usage-cell' },
    items.map((item) =>
      h(
        'div',
        {
          class: ['quota-usage-item', quotaWindowUsageTone(item)],
          title: quotaWindowUsageTitle(item),
        },
        quotaWindowUsageTags(item).map((tag) =>
          h(
            'span',
            { class: ['quota-usage-chip', tag.tone ? `is-${tag.tone}` : undefined] },
            [
              h('span', { class: 'quota-usage-chip-label' }, tag.label),
              h('strong', { class: 'quota-usage-chip-value' }, tag.value),
            ],
          ),
        ),
      ),
    ),
  )
}

function renderAccountIdentityCell(account: CodexKeeperAccount) {
  const primary = account.email ?? account.name
  const statusCode = disabledStatusCodeText(account)
  const statusLabel = `${account.disabled ? t('已禁用', 'Disabled') : t('启用中', 'Enabled')}${statusCode ? ` ${statusCode}` : ''}`
  return h(
    'div',
    {
      class: 'account-table-identity',
      title: `${primary}\n${account.name}\n${t('状态', 'Status')} ${statusLabel}`,
    },
    [
      h('span', { class: 'account-table-email' }, primary),
      h('span', { class: 'account-table-name' }, account.name),
      h('span', { class: 'account-table-meta' }, [
        h(
          'span',
          { class: ['account-table-chip', account.disabled ? 'is-warning' : 'is-success'] },
          statusLabel,
        ),
      ]),
    ],
  )
}

function renderAccountTypeCell(account: CodexKeeperAccount) {
  const typeLabel = accountTypeLabel(account.account_type)
  return h(
    'span',
    { class: ['account-table-chip', 'is-type'], title: typeLabel },
    typeLabel,
  )
}

function renderAccountPriorityCell(account: CodexKeeperAccount) {
  const priorityLabel = formatInteger(accountPriority(account))
  return h(
    'span',
    { class: ['account-table-chip', 'is-priority'], title: t(`优先级 ${priorityLabel}`, `Priority ${priorityLabel}`) },
    priorityLabel,
  )
}

function renderLastCheckedCell(account: CodexKeeperAccount) {
  const text = formatDateTime(account.last_checked_at)
  return h(
    'span',
    {
      class: ['account-table-value-pill', 'is-time', text === '-' ? 'is-empty' : ''],
      title: text,
    },
    text,
  )
}

function renderLatestActionCell(account: CodexKeeperAccount) {
  const text = latestActionText(account)
  return h(
    'span',
    {
      class: ['account-table-value-pill', 'is-action', text === '-' ? 'is-empty' : ''],
      title: text === '-' ? undefined : text,
    },
    text,
  )
}

async function loadAccounts() {
  isLoading.value = true
  try {
    const [accountsResponse, settings, nextStatus] = await Promise.all([
      listCodexKeeperAccounts(),
      getCodexKeeperSettings(),
      getCodexKeeperStatus(),
    ])
    accounts.value = accountsResponse.items
    priorityRules.value = settings.priority_rules
    keeperStatus.value = nextStatus
  } catch (error) {
    message.error(errorText(error, '加载账号状态失败', 'Failed to load account status'))
  } finally {
    isLoading.value = false
  }
}

async function loadKeeperStatus() {
  try {
    keeperStatus.value = await getCodexKeeperStatus()
  } catch {
    return
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
  bulkDeleteDialog.source = 'selection'
  bulkDeleteDialog.show = true
}

function openFilteredUnauthorizedDisabledBulkDeleteDialog() {
  if (!canBulkDeleteFilteredUnauthorizedDisabledAccounts.value) {
    return
  }
  selectedDisabledAccountKeys.value = filteredUnauthorizedDisabledAccountNames.value
  if (!canBulkDelete.value) {
    return
  }
  bulkDeleteDialog.source = 'disabled401'
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
      message.warning(t(`批量删除完成：成功 ${result.deleted.length} 个，失败 ${result.failed.length} 个`, `Bulk delete complete: ${result.deleted.length} succeeded, ${result.failed.length} failed`))
    } else if (result.failed.length > 0) {
      message.error(t(`批量删除失败：失败 ${result.failed.length} 个`, `Bulk delete failed: ${result.failed.length} failed`))
    } else {
      message.success(t(`已删除 ${result.deleted.length} 个已禁用账号`, `Deleted ${result.deleted.length} disabled accounts`))
    }
    bulkDeleteDialog.show = false
    await loadAccounts()
  } catch (error) {
    message.error(errorText(error, '批量删除失败', 'Bulk delete failed'))
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
    t('优先级已更新', 'Priority updated'),
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
    t('启用账号', 'Enable Account'),
    t(`启用 ${account.name}？`, `Enable ${account.name}?`),
    t('确认启用', 'Confirm Enable'),
    'primary',
    () => enableAccount(account),
  )
}

function confirmDisableAccount(account: CodexKeeperAccount) {
  openAccountConfirm(
    t('禁用账号', 'Disable Account'),
    t(`禁用 ${account.name}？`, `Disable ${account.name}?`),
    t('确认禁用', 'Confirm Disable'),
    'warning',
    () => disableAccount(account),
  )
}

function confirmDeleteAccount(account: CodexKeeperAccount) {
  openAccountConfirm(
    t('删除账号', 'Delete Account'),
    t(`删除 ${account.name}？此操作会从 CPA 删除 auth file。`, `Delete ${account.name}? This will remove the auth file from CPA.`),
    t('确认删除', 'Confirm Delete'),
    'error',
    () => deleteAccount(account),
  )
}

function enableAccount(account: CodexKeeperAccount) {
  return runAccountAction(
    account,
    'toggle',
    () => enableCodexKeeperAccount(account.name),
    t('账号已启用', 'Account enabled'),
  )
}

function disableAccount(account: CodexKeeperAccount) {
  return runAccountAction(
    account,
    'toggle',
    () => disableCodexKeeperAccount(account.name),
    t('账号已禁用', 'Account disabled'),
  )
}

function deleteAccount(account: CodexKeeperAccount) {
  return runAccountAction(
    account,
    'delete',
    () => deleteCodexKeeperAccount(account.name),
    t('账号已删除', 'Account deleted'),
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
      keeperStatus.value = status
      const runningModes = status.running_modes ?? []
      const accountRefreshRunning =
        runningModes.length > 0 ? runningModes.includes('accounts') : status.running
      if (accountRefreshRunning) {
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
    message.success(authNames.length === 1
      ? t('已开始刷新账号', 'Started refreshing account')
      : t(`已开始刷新 ${authNames.length} 个账号`, `Started refreshing ${authNames.length} accounts`))
    if (options.closeDetail) {
      detailOpen.value = false
    }
    if (options.clearSelection) {
      exitRefreshSelectMode()
    }
    void pollRefreshUntilIdle()
  } catch (error) {
    message.error(errorText(error, '刷新账号失败', 'Failed to refresh accounts'))
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
    message.error(errorText(error, '账号操作失败', 'Account operation failed'))
  } finally {
    const nextActions = new Set(actingActions.value)
    nextActions.delete(key)
    actingActions.value = nextActions
  }
}

const baseColumns = computed<DataTableColumns<CodexKeeperAccount>>(() => [
  {
    title: t('账号', 'Account'),
    key: 'identity',
    width: 360,
    render: (row) => renderAccountIdentityCell(row),
  },
  {
    title: t('类型', 'Type'),
    key: 'account_type',
    width: 96,
    render: (row) => renderAccountTypeCell(row),
  },
  {
    title: t('优先级', 'Priority'),
    key: 'priority',
    width: 88,
    render: (row) => renderAccountPriorityCell(row),
  },
  {
    title: t('额度窗口', 'Quota Window'),
    key: 'quota',
    width: 260,
    render: (row) => renderQuotaCell(row),
  },
  {
    title: t('窗口用量', 'Window Usage'),
    key: 'quota_usage',
    width: 280,
    render: (row) => renderQuotaUsageCell(row),
  },
  {
    title: t('最近巡检', 'Last Inspection'),
    key: 'last_checked_at',
    width: 150,
    render: (row) => renderLastCheckedCell(row),
  },
  {
    title: t('最近操作', 'Latest Action'),
    key: 'latest_action',
    width: 340,
    render: (row) => renderLatestActionCell(row),
  },
])

const disabledBaseColumns = computed<DataTableColumns<CodexKeeperAccount>>(
  () => baseColumns.value.filter(
    (column) => !('key' in column) || (column.key !== 'quota' && column.key !== 'quota_usage'),
  ),
)

const disabledActionColumn = computed<DataTableColumns<CodexKeeperAccount>[number]>(() => ({
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
            { default: () => t('详情', 'Details') },
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
            { default: () => t('启用', 'Enable') },
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
            { default: () => t('删除', 'Delete') },
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
            { default: () => t('刷新', 'Refresh') },
          ),
        ],
      },
    )
  },
}))

const normalActionColumn = computed<DataTableColumns<CodexKeeperAccount>[number]>(() => ({
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
            { default: () => t('详情', 'Details') },
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
            { default: () => t('禁用', 'Disable') },
          ),
          h(
            NButton,
            {
              size: 'small',
              quaternary: true,
              disabled: isRowActing(row) || isBulkDeleting.value || isBulkRefreshing.value,
              onClick: () => openPriorityDialog(row),
            },
            { default: () => t('优先级', 'Priority') },
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
            { default: () => t('刷新', 'Refresh') },
          ),
        ],
      },
    )
  },
}))

const disabledColumns = computed<DataTableColumns<CodexKeeperAccount>>(() => [
  {
    type: 'selection',
    width: 44,
    disabled: (row: CodexKeeperAccount) => isRowActing(row) || isBulkDeleting.value,
  },
  ...disabledBaseColumns.value,
  disabledActionColumn.value,
])

const normalColumns = computed<DataTableColumns<CodexKeeperAccount>>(() => [
  ...baseColumns.value,
  normalActionColumn.value,
])

restoreAccountStatusPreferences()

watch(
  [accountDisplaySize, accountListViewMode, () => accountSort.key, () => accountSort.direction],
  saveAccountStatusPreferences,
)
watch(
  [
    accountDisplaySize,
    accountListViewMode,
    () => accountSort.key,
    () => accountSort.direction,
    () => filters.keyword,
    () => filters.accountType,
    () => filters.priority,
    () => filters.status,
  ],
  resetAccountPages,
)
watch(
  [disabledAccountPageCount, normalAccountPageCount, cardAccountPageCount],
  clampAccountPages,
)
watch(visibleDisabledAccounts, pruneSelectedDisabledAccountKeys)
watch(filteredAccounts, pruneSelectedRefreshAccountNames)

onMounted(() => {
  void loadAccounts()
  keeperStatusTimer = window.setInterval(() => {
    void loadKeeperStatus()
  }, KEEPER_STATUS_POLL_INTERVAL_MS)
})

onBeforeUnmount(() => {
  refreshPollToken += 1
  if (keeperStatusTimer !== undefined) {
    window.clearInterval(keeperStatusTimer)
  }
})
</script>

<template>
  <section class="page account-status-page">
    <div class="page-header account-page-header">
      <div class="account-header-copy">
        <div class="account-header-title-row">
          <h1 class="page-title">{{ t('账号状态', 'Account Status') }}</h1>
          <div class="header-actions">
            <NButton secondary :loading="isLoading" @click="loadAccounts">
              <template #icon>
                <NIcon :component="RefreshCw" />
              </template>
              {{ t('重新加载', 'Reload') }}
            </NButton>
          </div>
        </div>
        <p class="page-subtitle">{{ t('查看 Codex auth file 的健康、额度和优先级维护结果', 'View Codex auth file health, quota, and priority maintenance results') }}</p>
      </div>
    </div>

    <div class="metric-grid account-metrics">
      <div class="metric-card inspection-status-card">
        <div class="metric-icon" aria-hidden="true">
          <Activity :size="20" :stroke-width="2.2" />
        </div>
        <div class="metric-label">{{ t('运行状态', 'Run Status') }}</div>
        <div class="metric-value inspection-status-value" :title="keeperStatusDetailText">
          <NTag class="inspection-status-tag" :type="keeperStateType" size="small" :bordered="false">
            {{ keeperStatusDetailText }}
          </NTag>
        </div>
        <div class="metric-footnote">{{ keeperStatusFootnoteText }}</div>
      </div>
      <div class="metric-card">
        <div class="metric-icon" aria-hidden="true">
          <Users :size="20" :stroke-width="2.2" />
        </div>
        <div class="metric-label">{{ t('账号总数', 'Total Accounts') }}</div>
        <div class="metric-value">{{ formatInteger(accounts.length) }}</div>
        <div class="metric-footnote">{{ t('全部 auth file', 'All auth files') }}</div>
      </div>
      <button
        type="button"
        class="metric-card metric-action is-green"
        :class="{ 'is-active': isStatusFilterActive('enabled') }"
        :aria-pressed="isStatusFilterActive('enabled')"
        @click="toggleStatusFilter('enabled')"
      >
        <div class="metric-icon" aria-hidden="true">
          <ShieldCheck :size="20" :stroke-width="2.2" />
        </div>
        <div class="metric-label">{{ t('启用中', 'Enabled') }}</div>
        <div class="metric-value">{{ formatInteger(enabledAccountCount) }}</div>
        <div class="metric-footnote">{{ t('可参与调度', 'Available for scheduling') }}</div>
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
        <div class="metric-label">{{ t('已禁用', 'Disabled') }}</div>
        <div class="metric-value">{{ formatInteger(disabledAccountCount) }}</div>
        <div class="metric-footnote">{{ t('停用账号', 'Inactive accounts') }}</div>
      </button>
      <button
        type="button"
        class="metric-card metric-action is-danger"
        :class="{ 'is-active': isStatusFilterActive('unauthorized') }"
        :aria-pressed="isStatusFilterActive('unauthorized')"
        @click="toggleStatusFilter('unauthorized')"
      >
        <div class="metric-icon" aria-hidden="true">
          <ShieldAlert :size="20" :stroke-width="2.2" />
        </div>
        <div class="metric-label">{{ t('401报错', '401 Errors') }}</div>
        <div class="metric-value">{{ formatInteger(unauthorizedErrorAccountCount) }}</div>
        <div class="metric-footnote">HTTP 401</div>
      </button>
      <button
        type="button"
        class="metric-card metric-action is-purple"
        :class="{ 'is-active': isStatusFilterActive('quotaExhausted') }"
        :aria-pressed="isStatusFilterActive('quotaExhausted')"
        @click="toggleStatusFilter('quotaExhausted')"
      >
        <div class="metric-icon" aria-hidden="true">
          <Gauge :size="20" :stroke-width="2.2" />
        </div>
        <div class="metric-label">{{ t('额度耗尽', 'Quota Exhausted') }}</div>
        <div class="metric-value">{{ formatInteger(quotaExhaustedAccountCount) }}</div>
        <div class="metric-footnote">{{ t('临时降级', 'Temporary downgrade') }}</div>
      </button>
    </div>

    <section class="panel account-list-panel">
      <div class="status-toolbar">
        <div class="toolbar-heading">
          <div>
            <h2 class="toolbar-title">{{ t('账号列表', 'Account List') }}</h2>
            <p class="toolbar-subtitle">
              {{ t(`正常 ${filteredNormalAccounts.length} / ${enabledAccountCount} 个账号`, `Normal ${filteredNormalAccounts.length} / ${enabledAccountCount} accounts`) }}
              <template v-if="hasDisabledAccounts">
                {{ t(`，已禁用 ${filteredDisabledAccounts.length} / ${disabledAccountCount} 个账号`, `, disabled ${filteredDisabledAccounts.length} / ${disabledAccountCount} accounts`) }}
              </template>
            </p>
          </div>
          <NTag v-if="activeFilterCount > 0" size="small" type="info" :bordered="false">
            {{ t(`已筛选 ${activeFilterCount} 项`, `${activeFilterCount} filters active`) }}
          </NTag>
        </div>
        <div class="filter-grid">
          <NInput v-model:value="filters.keyword" clearable :placeholder="t('搜索账号或邮箱', 'Search account or email')" />
          <NSelect
            v-model:value="filters.accountType"
            :options="accountTypeOptions"
            clearable
            filterable
            :placeholder="t('账号类型', 'Account Type')"
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
                {{ t(`切换样式：${accountListViewLabel}`, `Switch View: ${accountListViewLabel}`) }}
              </NButton>
            </NDropdown>
            <NButton
              secondary
              size="small"
              :type="refreshSelectMode ? 'primary' : 'default'"
              @click="toggleRefreshSelectMode"
            >
              {{ t('多选刷新', 'Multi-select Refresh') }}
            </NButton>
            <template v-if="refreshSelectMode">
              <NTag size="small" type="info" :bordered="false">
                {{ t(`${selectedRefreshCount} 已选`, `${selectedRefreshCount} selected`) }}
              </NTag>
              <NButton
                secondary
                size="small"
                :disabled="filteredAccountNames.length === 0 || isBulkRefreshing"
                @click="selectAllFilteredRefreshAccounts"
              >
                {{ t('全选当前筛选', 'Select Current Filter') }}
              </NButton>
              <NButton
                secondary
                type="primary"
                size="small"
                :disabled="!canRefreshSelected"
                :loading="isBulkRefreshing"
                @click="refreshSelectedAccounts"
              >
                {{ t('刷新已选', 'Refresh Selected') }}
              </NButton>
              <NButton secondary size="small" :disabled="isBulkRefreshing" @click="exitRefreshSelectMode">
                {{ t('退出选择', 'Exit Selection') }}
              </NButton>
            </template>
          </div>
          <div class="sort-control-row" :aria-label="t('账号排序', 'Account Sorting')">
            <span class="sort-control-label">{{ t('排序', 'Sort') }}</span>
            <NDropdown trigger="click" :options="quotaSortOptions" @select="handleQuotaSortSelect">
              <NButton
                secondary
                size="small"
                :type="accountSort.key === 'quotaDay' || accountSort.key === 'quotaWeek' ? 'primary' : 'default'"
              >
                {{ activeQuotaSortLabel ? t(`额度窗口：${activeQuotaSortLabel} ${sortDirectionMark}`, `Quota Window: ${activeQuotaSortLabel} ${sortDirectionMark}`) : t('额度窗口', 'Quota Window') }}
              </NButton>
            </NDropdown>
            <NButton
              secondary
              size="small"
              :type="isAccountSortActive('accountType') ? 'primary' : 'default'"
              @click="toggleAccountSort('accountType')"
            >
              {{ t('类型', 'Type') }} {{ accountSortMark('accountType') }}
            </NButton>
            <NButton
              secondary
              size="small"
              :type="isAccountSortActive('status') ? 'primary' : 'default'"
              @click="toggleAccountSort('status')"
            >
              {{ t('状态', 'Status') }} {{ accountSortMark('status') }}
            </NButton>
            <NButton
              secondary
              size="small"
              :type="isAccountSortActive('priority') ? 'primary' : 'default'"
              @click="toggleAccountSort('priority')"
            >
              {{ t('优先级', 'Priority') }} {{ accountSortMark('priority') }}
            </NButton>
            <NButton
              secondary
              size="small"
              :type="isAccountSortActive('lastCheckedAt') ? 'primary' : 'default'"
              @click="toggleAccountSort('lastCheckedAt')"
            >
              {{ t('最近巡检', 'Last Inspection') }} {{ accountSortMark('lastCheckedAt') }}
            </NButton>
          </div>
        </div>
      </div>

      <div v-if="isTableView" class="account-sections">
        <div v-if="showTableLoadingState" class="empty-state">{{ t('账号加载中...', 'Loading accounts...') }}</div>
        <div v-else-if="showEmptyTableState" class="empty-state">{{ t('当前筛选下暂无账号', 'No accounts match the current filter') }}</div>
        <section v-if="showDisabledSection" class="account-section">
          <div class="account-section-header">
            <div class="account-section-title-group">
              <h3 class="account-section-title">{{ t('已禁用账号', 'Disabled Accounts') }}</h3>
              <p class="account-section-subtitle">
                {{ disabledSectionDisplayText }}
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
                <template #icon>
                  <NIcon :component="Trash2" />
                </template>
                {{ t(`批量删除（${selectedDisabledCount}）`, `Bulk Delete (${selectedDisabledCount})`) }}
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
              <div class="empty-state">{{ t('当前筛选下暂无已禁用账号', 'No disabled accounts match the current filter') }}</div>
            </template>
          </NDataTable>
          <div v-if="showDisabledPagination" class="account-pagination-row">
            <NPagination
              v-model:page="disabledAccountPage"
              size="small"
              :page-size="accountPaginationPageSize"
              :item-count="filteredDisabledAccounts.length"
            />
          </div>
        </section>

        <section v-if="showNormalSection" class="account-section">
          <div class="account-section-header">
            <div class="account-section-title-group">
              <h3 class="account-section-title">{{ t('正常账号', 'Normal Accounts') }}</h3>
              <p class="account-section-subtitle">
                {{ normalSectionDisplayText }}
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
              <div class="empty-state">{{ t('当前筛选下暂无正常账号', 'No normal accounts match the current filter') }}</div>
            </template>
          </NDataTable>
          <div v-if="showNormalPagination" class="account-pagination-row">
            <NPagination
              v-model:page="normalAccountPage"
              size="small"
              :page-size="accountPaginationPageSize"
              :item-count="filteredNormalAccounts.length"
            />
          </div>
        </section>
      </div>
      <div v-else class="account-card-shell">
        <section class="account-section account-card-section">
          <div class="account-section-header">
            <div class="account-section-title-group">
              <h3 class="account-section-title">{{ accountListViewLabel }}</h3>
              <p class="account-section-subtitle">
                {{ cardSectionDisplayText }}
              </p>
            </div>
            <div v-if="filteredUnauthorizedDisabledAccounts.length > 0" class="account-section-actions">
              <NButton
                secondary
                type="error"
                size="small"
                :disabled="!canBulkDeleteFilteredUnauthorizedDisabledAccounts"
                :loading="isBulkDeleting"
                @click="openFilteredUnauthorizedDisabledBulkDeleteDialog"
              >
                <template #icon>
                  <NIcon :component="Trash2" />
                </template>
                {{ t(`批量删除 401 已禁用（${filteredUnauthorizedDisabledAccounts.length}）`, `Bulk Delete 401 Disabled (${filteredUnauthorizedDisabledAccounts.length})`) }}
              </NButton>
            </div>
          </div>
          <div v-if="showCardLoadingState" class="empty-state">{{ t('账号加载中...', 'Loading accounts...') }}</div>
          <div v-else-if="visibleCardAccounts.length === 0" class="empty-state">
            {{ t('当前筛选下暂无账号', 'No accounts match the current filter') }}
          </div>
          <div
            v-else
            class="account-card-grid"
            :class="{
              'is-bar': isBarCardView,
              'is-ring': !isBarCardView,
            }"
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
                'is-quota-exhausted': isQuotaExhaustedAccount(account),
                'is-select-mode': refreshSelectMode,
                'is-selected': isRefreshAccountSelected(account),
              }"
              :aria-label="
                refreshSelectMode
                  ? t(`选择 ${account.email ?? account.name}`, `Select ${account.email ?? account.name}`)
                  : t(`查看 ${account.email ?? account.name} 详情`, `View details for ${account.email ?? account.name}`)
              "
              :aria-pressed="refreshSelectMode ? isRefreshAccountSelected(account) : undefined"
              @click="handleAccountCardClick(account)"
            >
              <div class="account-card-top">
                <div class="account-card-identity">
                  <span class="account-card-email">{{ account.email ?? account.name }}</span>
                  <span class="account-card-name">{{ account.name }}</span>
                </div>
                <div class="account-card-status-group">
                  <span
                    class="account-status-pill"
                    :class="
                      account.disabled
                        ? 'is-danger'
                        : isQuotaExhaustedAccount(account)
                          ? 'is-quota-exhausted'
                          : 'is-success'
                    "
                  >
                    {{ account.disabled ? t('已禁用', 'Disabled') : isQuotaExhaustedAccount(account) ? t('额度耗尽', 'Quota Exhausted') : t('启用中', 'Enabled') }}
                  </span>
                  <span
                    v-if="disabledStatusCodeText(account)"
                    class="account-status-code-badge"
                    :title="disabledStatusCodeTitle(account) ?? undefined"
                  >
                    {{ disabledStatusCodeText(account) }}
                  </span>
                </div>
              </div>
              <div class="account-card-meta-grid">
                <div class="account-card-meta-item">
                  <span>{{ t('类型', 'Type') }}</span>
                  <strong>{{ accountTypeLabel(account.account_type) }}</strong>
                </div>
                <div class="account-card-meta-item">
                  <span>{{ t('优先级', 'Priority') }}</span>
                  <strong>{{ formatInteger(accountPriority(account)) }}</strong>
                </div>
                <div class="account-card-meta-item">
                  <span>{{ t('最近巡检', 'Last Inspection') }}</span>
                  <strong :title="formatDateTime(account.last_checked_at)">
                    {{ formatDateTime(account.last_checked_at, { includeSecond: false }) }}
                  </strong>
                </div>
              </div>
              <div
                v-if="account.disabled"
                class="account-card-error"
                :title="disabledCardErrorText(account)"
              >
                <span>{{ t('报错信息', 'Error Details') }}</span>
                <strong>{{ disabledCardErrorText(account) }}</strong>
              </div>
              <div v-else-if="shouldShowQuotaWindow(account)" class="account-card-quota">
                <template v-if="quotaWindowItems(account).length > 0">
                  <template v-if="isBarCardView">
                    <div
                      v-for="item in quotaWindowItems(account)"
                      :key="item.label"
                      class="card-quota-bar"
                    >
                      <div class="card-quota-head">
                        <span>{{ item.label }}</span>
                        <strong>{{ t(`剩余 ${item.remainingPercent}%`, `${item.remainingPercent}% remaining`) }}</strong>
                      </div>
                      <div class="card-quota-track">
                        <div
                          class="card-quota-fill"
                          :class="quotaBarTone(item.remainingPercent)"
                          :style="{ width: `${item.remainingPercent}%` }"
                        />
                      </div>
                      <span class="card-quota-reset">
                        {{ quotaWindowResetText(item) }}
                      </span>
                      <div class="card-quota-usage-tags" :title="quotaWindowUsageTitle(item)">
                        <span
                          v-for="tag in quotaWindowUsageTags(item)"
                          :key="`${item.label}-${tag.label}`"
                          class="card-quota-usage-tag"
                          :class="tag.tone ? `is-${tag.tone}` : undefined"
                        >
                          <span>{{ tag.label }}</span>
                          <strong>{{ tag.value }}</strong>
                        </span>
                      </div>
                    </div>
                  </template>
                  <div v-else class="card-quota-rings">
                    <div
                      v-for="item in quotaWindowItems(account)"
                      :key="item.label"
                      class="card-quota-ring-item"
                    >
                      <div class="card-quota-ring-head">
                        <div
                          class="quota-ring"
                          :class="quotaBarTone(item.remainingPercent)"
                          :style="{ '--quota-deg': `${item.remainingPercent * 3.6}deg` }"
                        >
                          <span>{{ item.remainingPercent }}%</span>
                        </div>
                        <div class="quota-ring-caption">
                          <strong>{{ item.label }}</strong>
                          <span>{{ quotaWindowResetText(item) }}</span>
                        </div>
                      </div>
                      <div class="card-quota-usage-tags" :title="quotaWindowUsageTitle(item)">
                        <span
                          v-for="tag in quotaWindowUsageTags(item)"
                          :key="`${item.label}-${tag.label}`"
                          class="card-quota-usage-tag"
                          :class="tag.tone ? `is-${tag.tone}` : undefined"
                        >
                          <span>{{ tag.label }}</span>
                          <strong>{{ tag.value }}</strong>
                        </span>
                      </div>
                    </div>
                  </div>
                </template>
                <div v-else class="card-quota-empty">{{ t('暂无额度窗口', 'No quota windows') }}</div>
              </div>
            </button>
          </div>
          <div v-if="showCardPagination" class="account-pagination-row">
            <NPagination
              v-model:page="cardAccountPage"
              size="small"
              :page-size="accountPaginationPageSize"
              :item-count="sortedCardAccounts.length"
            />
          </div>
        </section>
      </div>

      <div class="display-control-row">
        <div class="display-control-copy">
          <span class="display-control-label">{{ t('每页数量', 'Items Per Page') }}</span>
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
              {{ t('返回', 'Back') }}
            </NButton>
            <span class="detail-drawer-title">{{ t('账号详情', 'Account Details') }}</span>
          </div>
        </template>
        <NDescriptions v-if="selectedAccount" label-placement="left" :column="1" size="small" bordered>
          <NDescriptionsItem :label="t('账号', 'Account')">{{ selectedAccount.name }}</NDescriptionsItem>
          <NDescriptionsItem :label="t('邮箱', 'Email')">{{ selectedAccount.email ?? '-' }}</NDescriptionsItem>
          <NDescriptionsItem :label="t('账号类型', 'Account Type')">
            {{ accountTypeLabel(selectedAccount.account_type) }}
          </NDescriptionsItem>
          <NDescriptionsItem :label="t('启用状态', 'Enabled Status')">
            {{ selectedAccount.disabled ? t('已禁用', 'Disabled') : t('启用中', 'Enabled') }}
          </NDescriptionsItem>
          <NDescriptionsItem :label="t('当前优先级', 'Current Priority')">
            {{ accountPriority(selectedAccount) }}
          </NDescriptionsItem>
          <NDescriptionsItem :label="t('类型默认优先级', 'Type Default Priority')">
            {{ defaultPriority(selectedAccount) ?? '-' }}
          </NDescriptionsItem>
          <NDescriptionsItem v-if="shouldShowQuotaWindow(selectedAccount)" :label="t('额度窗口', 'Quota Window')">
            {{ quotaText(selectedAccount) }}
          </NDescriptionsItem>
          <NDescriptionsItem :label="t('状态码', 'Status Code')">
            {{ selectedAccount.last_status_code ?? '-' }}
          </NDescriptionsItem>
          <NDescriptionsItem :label="t('最近健康', 'Last Healthy')">
            {{ formatDateTime(selectedAccount.last_healthy_at) }}
          </NDescriptionsItem>
          <NDescriptionsItem :label="t('最近巡检', 'Last Inspection')">
            {{ formatDateTime(selectedAccount.last_checked_at) }}
          </NDescriptionsItem>
          <NDescriptionsItem :label="t('最近操作', 'Latest Action')">
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
              {{ t('刷新', 'Refresh') }}
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
              {{ t('启用', 'Enable') }}
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
              {{ t('禁用', 'Disable') }}
            </NButton>
            <NButton
              size="small"
              secondary
              :disabled="isRowActing(selectedAccount) || isBulkDeleting || isBulkRefreshing"
              :loading="isActionLoading(selectedAccount, 'priority')"
              @click="openPriorityDialog(selectedAccount)"
            >
              {{ t('修改优先级', 'Change Priority') }}
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
              {{ t('删除', 'Delete') }}
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
            {{ t('取消', 'Cancel') }}
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
      :title="bulkDeleteDialogTitle"
      :style="{ width: 'min(460px, calc(100vw - 32px))' }"
    >
      <div class="bulk-delete-dialog">
        <p class="bulk-delete-warning">
          {{ bulkDeleteWarningText }}
        </p>
        <div v-if="bulkDeletePreviewNames.length > 0" class="bulk-delete-preview">
          <span v-for="name in bulkDeletePreviewNames" :key="name">{{ name }}</span>
          <span v-if="bulkDeletePreviewOverflow > 0">{{ t(`另 ${bulkDeletePreviewOverflow} 个...`, `${bulkDeletePreviewOverflow} more...`) }}</span>
        </div>
      </div>
      <template #action>
        <NSpace justify="end">
          <NButton :disabled="isBulkDeleting" @click="bulkDeleteDialog.show = false">{{ t('取消', 'Cancel') }}</NButton>
          <NButton
            type="error"
            :disabled="selectedDisabledCount === 0"
            :loading="isBulkDeleting"
            @click="submitBulkDelete"
          >
            {{ t('确认删除', 'Confirm Delete') }}
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
          <NButton @click="priorityDialog.show = false">{{ t('取消', 'Cancel') }}</NButton>
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
            {{ t('确认', 'Confirm') }}
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

.inspection-status-card {
  min-width: 0;
}

.inspection-status-value {
  display: flex;
  min-width: 0;
  max-width: 100%;
  overflow: hidden;
}

.inspection-status-tag {
  max-width: 100%;
  min-width: 0;
}

.inspection-status-value :deep(.n-tag) {
  max-width: 100%;
  min-width: 0;
}

.inspection-status-value :deep(.n-tag__content) {
  display: block;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
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
  flex-wrap: wrap;
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

.account-pagination-row {
  display: flex;
  justify-content: flex-end;
  min-width: 0;
}

.account-pagination-row :deep(.n-pagination) {
  flex-wrap: wrap;
  justify-content: flex-end;
}

.account-card-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 10px;
}

.account-card-grid.is-bar {
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 300px), 1fr));
}

.account-card-grid.is-ring {
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 300px), 1fr));
}

.account-card {
  --account-card-bg: var(--cpa-surface-raised);
  --account-card-border: var(--cpa-border);
  --account-card-hover-border: color-mix(in srgb, var(--cpa-primary) 36%, var(--cpa-border));
  --account-card-accent: var(--cpa-primary);
  --account-card-inner-bg: color-mix(
    in srgb,
    var(--account-card-bg) 72%,
    var(--cpa-surface-raised)
  );
  --account-card-inner-border: color-mix(
    in srgb,
    var(--account-card-border) 70%,
    var(--cpa-border)
  );
  display: grid;
  align-content: start;
  gap: 12px;
  width: 100%;
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
  background: var(--account-card-bg);
  border: 1px solid var(--account-card-border);
  border-radius: var(--cpa-radius);
  box-shadow: var(--cpa-shadow-card), var(--cpa-shadow-hairline);
}

.account-card.is-enabled {
  --account-card-accent: var(--cpa-success);
  --account-card-bg: color-mix(in srgb, var(--cpa-success-weak) 24%, var(--cpa-surface-raised));
  --account-card-border: color-mix(in srgb, var(--cpa-success) 14%, var(--cpa-border));
  --account-card-hover-border: color-mix(in srgb, var(--cpa-success) 26%, var(--cpa-border));
}

.account-card.is-disabled {
  --account-card-accent: var(--cpa-danger);
  --account-card-bg: color-mix(in srgb, var(--cpa-danger-weak) 30%, var(--cpa-surface-raised));
  --account-card-border: color-mix(in srgb, var(--cpa-danger) 18%, var(--cpa-border));
  --account-card-hover-border: color-mix(in srgb, var(--cpa-danger) 32%, var(--cpa-border));
}

.account-card.is-quota-exhausted {
  --account-card-accent: var(--cpa-warning);
  --account-card-bg: color-mix(in srgb, var(--cpa-warning-weak) 34%, var(--cpa-surface-raised));
  --account-card-border: color-mix(in srgb, var(--cpa-warning) 20%, var(--cpa-border));
  --account-card-hover-border: color-mix(in srgb, var(--cpa-warning) 34%, var(--cpa-border));
}

.account-card:hover {
  border-color: var(--account-card-hover-border);
  transform: translateY(-1px);
}

.account-card.is-select-mode {
  background: color-mix(in srgb, var(--cpa-primary) 5%, var(--account-card-bg));
}

.account-card.is-selected {
  background: color-mix(in srgb, var(--cpa-primary) 12%, var(--account-card-bg));
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

.account-card-status-group {
  display: inline-flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  justify-content: flex-end;
  flex-shrink: 0;
  min-width: 0;
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

.account-status-pill.is-danger {
  color: var(--cpa-danger);
  background: var(--cpa-danger-weak);
  border-color: color-mix(in srgb, var(--cpa-danger) 28%, transparent);
}

.account-status-pill.is-quota-exhausted {
  --quota-exhausted-pill-color: color-mix(in srgb, var(--cpa-warning) 76%, var(--cpa-accent-blue));
  color: var(--quota-exhausted-pill-color);
  background: var(--cpa-warning-weak);
  border-color: color-mix(in srgb, var(--quota-exhausted-pill-color) 30%, transparent);
}

.account-status-code-badge {
  display: inline-flex;
  align-items: center;
  max-width: 100%;
  padding: 1px 6px;
  overflow: hidden;
  color: var(--cpa-danger);
  font-size: 11px;
  font-weight: 800;
  line-height: 1.45;
  text-overflow: ellipsis;
  white-space: nowrap;
  background: var(--cpa-danger-weak);
  border: 1px solid color-mix(in srgb, var(--cpa-danger) 28%, transparent);
  border-radius: var(--cpa-radius-sm);
  font-variant-numeric: tabular-nums;
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
  background: color-mix(in srgb, var(--account-card-accent) 7%, var(--account-card-inner-bg));
  border: 1px solid color-mix(in srgb, var(--account-card-accent) 15%, var(--account-card-inner-border));
  border-radius: var(--cpa-radius-sm);
}

.account-card-meta-item:nth-child(2) {
  background: color-mix(in srgb, var(--account-card-accent) 9%, var(--account-card-inner-bg));
  border-color: color-mix(in srgb, var(--account-card-accent) 17%, var(--account-card-inner-border));
}

.account-card-meta-item:nth-child(3) {
  background: color-mix(in srgb, var(--account-card-accent) 11%, var(--account-card-inner-bg));
  border-color: color-mix(in srgb, var(--account-card-accent) 19%, var(--account-card-inner-border));
}

.account-card-meta-item span {
  overflow: hidden;
  color: var(--cpa-text-muted);
  font-size: 11px;
  line-height: 1.1;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.account-card-meta-item strong {
  min-width: 0;
  overflow: hidden;
  color: color-mix(in srgb, var(--cpa-text) 88%, var(--cpa-text-muted));
  font-size: 12px;
  font-weight: 700;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.account-card-error {
  display: grid;
  gap: 4px;
  min-width: 0;
  padding: 8px 10px;
  background: color-mix(in srgb, var(--cpa-danger) 7%, var(--cpa-surface-muted));
  border: 1px solid color-mix(in srgb, var(--cpa-danger) 24%, var(--cpa-border));
  border-radius: var(--cpa-radius-sm);
}

.account-card-error span {
  overflow: hidden;
  color: var(--cpa-danger);
  font-size: 11px;
  font-weight: 700;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.account-card-error strong {
  display: -webkit-box;
  min-width: 0;
  overflow: hidden;
  color: var(--cpa-text);
  font-size: 12px;
  font-weight: 600;
  line-height: 1.45;
  overflow-wrap: anywhere;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 3;
}

.account-card-quota {
  display: grid;
  gap: 10px;
  min-width: 0;
  padding-top: 2px;
}

.card-quota-bar {
  display: grid;
  gap: 7px;
  min-width: 0;
  padding-top: 9px;
  border-top: 1px solid var(--account-card-inner-border);
}

.card-quota-bar:first-child {
  padding-top: 0;
  border-top: 0;
}

.card-quota-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  min-width: 0;
  line-height: 1.2;
}

.card-quota-head span {
  min-width: 0;
  overflow: hidden;
  color: var(--cpa-text);
  font-size: 12px;
  font-weight: 700;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.card-quota-reset {
  overflow: hidden;
  color: var(--cpa-text-muted);
  font-size: 11px;
  line-height: 1.35;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.card-quota-head strong {
  flex-shrink: 0;
  padding: 2px 7px;
  color: var(--cpa-text-strong);
  font-size: 11px;
  font-weight: 800;
  line-height: 1.35;
  background: color-mix(in srgb, var(--account-card-accent) 8%, var(--account-card-inner-bg));
  border: 1px solid color-mix(in srgb, var(--account-card-accent) 12%, transparent);
  border-radius: 999px;
  font-variant-numeric: tabular-nums;
}

.card-quota-usage-tags {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(68px, 1fr));
  gap: 6px;
  min-width: 0;
}

.card-quota-bar .card-quota-usage-tags {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.card-quota-usage-tag {
  display: grid;
  align-content: space-between;
  gap: 4px;
  min-width: 0;
  min-height: 42px;
  overflow: hidden;
  padding: 7px 8px 6px;
  background: color-mix(in srgb, var(--account-card-accent) 8%, var(--account-card-inner-bg));
  border: 1px solid color-mix(in srgb, var(--account-card-accent) 16%, var(--account-card-inner-border));
  border-radius: var(--cpa-radius-sm);
}

.card-quota-usage-tag:nth-child(2) {
  background: color-mix(in srgb, var(--account-card-accent) 10%, var(--account-card-inner-bg));
  border-color: color-mix(in srgb, var(--account-card-accent) 18%, var(--account-card-inner-border));
}

.card-quota-usage-tag:nth-child(3) {
  background: color-mix(in srgb, var(--account-card-accent) 12%, var(--account-card-inner-bg));
  border-color: color-mix(in srgb, var(--account-card-accent) 20%, var(--account-card-inner-border));
}

.card-quota-usage-tag span,
.card-quota-usage-tag strong {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.card-quota-usage-tag span {
  color: var(--cpa-text-muted);
  font-size: 10px;
  line-height: 1.1;
}

.card-quota-usage-tag strong {
  color: color-mix(in srgb, var(--cpa-text-strong) 88%, var(--cpa-text-muted));
  font-size: 13px;
  font-weight: 800;
  line-height: 1.1;
  font-variant-numeric: tabular-nums;
}

.card-quota-usage-tag.is-stale {
  --usage-accent: var(--cpa-warning);
  grid-column: 1 / -1;
  background: color-mix(in srgb, var(--cpa-warning) 9%, var(--cpa-surface-raised));
  border-color: color-mix(in srgb, var(--cpa-warning) 20%, var(--cpa-border));
}

.card-quota-usage-tag.is-stale strong {
  color: var(--cpa-warning);
}

.card-quota-track {
  height: 8px;
  overflow: hidden;
  background: color-mix(in srgb, var(--cpa-text-muted) 8%, var(--cpa-surface-muted));
  border: 1px solid color-mix(in srgb, var(--cpa-border) 68%, transparent);
  border-radius: 999px;
}

.card-quota-fill {
  height: 100%;
  min-width: 0;
  border-radius: inherit;
  box-shadow: inset 0 -1px 0 color-mix(in srgb, #000 14%, transparent);
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
  display: grid;
  grid-template-columns: 1fr;
  gap: 10px;
}

.card-quota-ring-item {
  --quota-ring-item-bg: color-mix(
    in srgb,
    var(--account-card-accent) 7%,
    var(--account-card-inner-bg)
  );
  --quota-ring-item-border: color-mix(
    in srgb,
    var(--account-card-accent) 16%,
    var(--account-card-inner-border)
  );
  display: grid;
  grid-template-columns: 1fr;
  gap: 8px;
  min-width: 0;
  padding: 8px;
  background: var(--quota-ring-item-bg);
  border: 1px solid var(--quota-ring-item-border);
  border-radius: var(--cpa-radius-sm);
}

.card-quota-ring-head {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.card-quota-ring-item .card-quota-usage-tags {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.quota-ring {
  --quota-color: var(--cpa-success);
  display: grid;
  position: relative;
  width: 48px;
  height: 48px;
  flex-shrink: 0;
  place-items: center;
  overflow: hidden;
  background:
    conic-gradient(
      var(--quota-color) var(--quota-deg),
      color-mix(in srgb, var(--cpa-text-muted) 18%, transparent) 0
    );
  border-radius: 50%;
  box-shadow: 0 0 0 1px color-mix(in srgb, var(--cpa-border) 70%, transparent);
}

.quota-ring::before {
  position: absolute;
  inset: 5px;
  content: "";
  background: var(--quota-ring-item-bg, var(--account-card-inner-bg));
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
  gap: 4px;
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
  align-content: center;
  gap: 4px;
  min-width: 0;
  min-height: 38px;
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

:global(.quota-window-usage) {
  overflow: hidden;
  color: var(--cpa-text-muted);
  font-size: 11px;
  line-height: 1.25;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:global(.quota-window-usage.is-stale) {
  color: var(--cpa-warning);
}

:global(.quota-usage-cell) {
  display: grid;
  gap: 8px;
  min-width: 0;
  padding: 4px 0;
}

:global(.quota-usage-item) {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 6px;
  min-width: 0;
  min-height: 38px;
}

:global(.quota-usage-chip) {
  --usage-accent: var(--cpa-primary);
  display: grid;
  align-content: center;
  gap: 2px;
  min-width: 0;
  min-height: 38px;
  padding: 6px 7px;
  overflow: hidden;
  background:
    linear-gradient(
      180deg,
      color-mix(in srgb, var(--usage-accent) 5%, var(--cpa-surface-raised)),
      color-mix(in srgb, var(--cpa-surface-muted) 90%, var(--cpa-surface-raised))
    );
  border: 1px solid color-mix(in srgb, var(--usage-accent) 18%, var(--cpa-border));
  border-radius: var(--cpa-radius-sm);
  box-shadow: inset 0 1px 0 color-mix(in srgb, var(--cpa-surface-raised) 70%, transparent);
}

:global(.quota-usage-chip:nth-child(2)) {
  --usage-accent: var(--cpa-accent-blue);
}

:global(.quota-usage-chip:nth-child(3)) {
  --usage-accent: var(--cpa-accent-orange);
}

:global(.quota-usage-chip-label),
:global(.quota-usage-chip-value) {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:global(.quota-usage-chip-label) {
  color: color-mix(in srgb, var(--usage-accent) 62%, var(--cpa-text-muted));
  font-size: 10px;
  font-weight: 700;
  line-height: 1.1;
}

:global(.quota-usage-chip-value) {
  color: var(--cpa-text-strong);
  font-size: 12px;
  font-weight: 800;
  line-height: 1.1;
  font-variant-numeric: tabular-nums;
}

:global(.quota-usage-chip.is-stale) {
  --usage-accent: var(--cpa-warning);
  grid-column: 1 / -1;
  background: color-mix(in srgb, var(--cpa-warning) 9%, var(--cpa-surface-raised));
  border-color: color-mix(in srgb, var(--cpa-warning) 20%, var(--cpa-border));
}

:global(.quota-usage-chip.is-stale .quota-usage-chip-value) {
  color: var(--cpa-warning);
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

:global(.account-table-identity) {
  display: grid;
  gap: 3px;
  min-width: 0;
  line-height: 1.25;
}

:global(.account-table-email),
:global(.account-table-name) {
  display: block;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:global(.account-table-email) {
  color: var(--cpa-text-strong);
  font-size: 13px;
  font-weight: 650;
}

:global(.account-table-name) {
  color: var(--cpa-text-muted);
  font-size: 12px;
  font-weight: 500;
}

:global(.account-table-meta) {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  min-width: 0;
  padding-top: 1px;
}

:global(.account-table-chip) {
  display: inline-flex;
  align-items: center;
  max-width: 100%;
  min-width: 0;
  padding: 1px 6px;
  overflow: hidden;
  font-size: 11px;
  font-weight: 700;
  line-height: 1.45;
  text-overflow: ellipsis;
  white-space: nowrap;
  border: 1px solid transparent;
  border-radius: var(--cpa-radius-sm);
  font-variant-numeric: tabular-nums;
}

:global(.account-table-chip.is-type) {
  color: var(--cpa-text);
  font-size: 12px;
  font-weight: 750;
  background: var(--cpa-surface-muted);
  border-color: color-mix(in srgb, var(--cpa-border) 72%, transparent);
}

:global(.account-table-chip.is-success) {
  color: var(--cpa-success);
  background: var(--cpa-success-weak);
  border-color: color-mix(in srgb, var(--cpa-success) 26%, transparent);
}

:global(.account-table-chip.is-warning) {
  color: var(--cpa-warning);
  background: var(--cpa-warning-weak);
  border-color: color-mix(in srgb, var(--cpa-warning) 26%, transparent);
}

:global(.account-table-chip.is-priority) {
  color: var(--cpa-primary);
  background: color-mix(in srgb, var(--cpa-primary) 9%, var(--cpa-surface-muted));
  border-color: color-mix(in srgb, var(--cpa-primary) 24%, transparent);
}

:global(.account-table-value-pill) {
  display: inline-flex;
  align-items: center;
  max-width: 100%;
  min-width: 0;
  padding: 3px 8px;
  overflow: hidden;
  color: var(--cpa-text);
  font-size: 12px;
  font-weight: 600;
  line-height: 1.45;
  text-overflow: ellipsis;
  white-space: nowrap;
  border: 1px solid var(--cpa-border);
  border-radius: var(--cpa-radius-sm);
  font-variant-numeric: tabular-nums;
}

:global(.account-table-value-pill.is-time) {
  color: var(--cpa-primary);
  background: color-mix(in srgb, var(--cpa-primary) 8%, var(--cpa-surface-muted));
  border-color: color-mix(in srgb, var(--cpa-primary) 22%, transparent);
}

:global(.account-table-value-pill.is-action) {
  display: -webkit-box;
  line-height: 1.5;
  white-space: normal;
  overflow-wrap: anywhere;
  background: color-mix(in srgb, var(--cpa-text-muted) 8%, var(--cpa-surface-muted));
  border-color: color-mix(in srgb, var(--cpa-border) 78%, transparent);
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  word-break: break-word;
}

:global(.account-table-value-pill.is-action.is-empty) {
  display: inline-flex;
  white-space: nowrap;
}

:global(.account-table-value-pill.is-empty) {
  color: var(--cpa-text-muted);
  font-weight: 700;
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

  .account-pagination-row {
    justify-content: flex-start;
  }

  .display-control-row {
    align-items: flex-start;
    flex-direction: column;
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

  .account-card-status-group {
    justify-content: flex-start;
  }

  .account-card-meta-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
