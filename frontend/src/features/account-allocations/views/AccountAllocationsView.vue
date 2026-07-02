<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue'
import {
  NAlert,
  NButton,
  NDataTable,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NModal,
  NPopconfirm,
  NProgress,
  NSelect,
  NSpace,
  NSwitch,
  NTag,
  useMessage,
  type DataTableColumns,
} from 'naive-ui'
import { BellRing, Boxes, Gauge, ShieldAlert } from 'lucide-vue-next'

import {
  createAccountPool,
  createUserAccountAllocation,
  deleteAccountPool,
  deleteUserAccountAllocation,
  getAccountAllocationsOverview,
  replaceAccountPoolMembers,
  updateAccountPool,
  updateUserAccountAllocation,
} from '@/features/account-allocations/api/accountAllocationsApi'
import { useI18n } from '@/shared/i18n'
import type {
  AccountAllocationAccount,
  AccountAllocationPeriod,
  AccountAllocationQuotaType,
  AccountAllocationScopeType,
  AccountAllocationUsage,
  AccountAllocationsOverview,
  AccountPool,
  UserAccountAllocation,
  UserAccountAllocationPayload,
} from '@/shared/types/api'
import { formatCompact, formatDateTime, formatInteger, formatUsd } from '@/shared/utils/format'

const message = useMessage()
const { errorText, t } = useI18n()
const isLoading = ref(false)
const isSavingPool = ref(false)
const isSavingAllocation = ref(false)
const overview = ref<AccountAllocationsOverview | null>(null)

const poolEditorVisible = ref(false)
const editingPoolId = ref<number | null>(null)
const poolName = ref('')
const poolDescription = ref('')
const poolAuthNames = ref<string[]>([])

const allocationEditorVisible = ref(false)
const editingAllocationId = ref<number | null>(null)
const allocationUserId = ref<number | null>(null)
const allocationScopeType = ref<AccountAllocationScopeType>('auth')
const allocationAuthName = ref<string | null>(null)
const allocationPoolId = ref<number | null>(null)
const allocationQuotaType = ref<AccountAllocationQuotaType>('tokens')
const allocationQuotaLimit = ref(100000)
const allocationPeriod = ref<AccountAllocationPeriod>('monthly')
const allocationHardLimit = ref(false)
const allocationEnabled = ref(true)
const allocationNote = ref('')

const accounts = computed(() => overview.value?.accounts ?? [])
const pools = computed(() => overview.value?.pools ?? [])
const allocations = computed(() => overview.value?.allocations ?? [])
const usageRows = computed(() => overview.value?.usage ?? [])
const users = computed(() => overview.value?.users ?? [])

const accountOptions = computed(() =>
  accounts.value.map((account) => ({
    label: accountLabel(account),
    value: account.auth_name,
  })),
)

const userOptions = computed(() =>
  users.value.map((user) => ({
    label: user.disabled ? `${user.label} (${t('已禁用', 'disabled')})` : user.label,
    value: user.id,
  })),
)

const poolOptions = computed(() =>
  pools.value.map((pool) => ({
    label: `${pool.name} (${formatInteger(pool.members.length)})`,
    value: pool.id,
  })),
)

const scopeOptions = computed(() => [
  { label: t('单账号', 'Single account'), value: 'auth' },
  { label: t('账号池', 'Account pool'), value: 'pool' },
])

const quotaTypeOptions = computed(() => [
  { label: t('请求数', 'Requests'), value: 'requests' },
  { label: 'Tokens', value: 'tokens' },
  { label: 'USD', value: 'usd' },
])

const periodOptions = computed(() => [
  { label: t('每日', 'Daily'), value: 'daily' },
  { label: t('每月', 'Monthly'), value: 'monthly' },
  { label: t('累计', 'All time'), value: 'all_time' },
])

const overQuotaCount = computed(
  () => usageRows.value.filter((item) => item.warning_level === 'over_quota').length,
)
const warningCount = computed(
  () => usageRows.value.filter((item) => item.warning_level === 'warning').length,
)
const hardLimitCount = computed(() => allocations.value.filter((item) => item.hard_limit).length)
const enabledAllocationCount = computed(() => allocations.value.filter((item) => item.enabled).length)

const metricCards = computed(() => [
  {
    key: 'over',
    label: t('超额告警', 'Over quota'),
    value: formatInteger(overQuotaCount.value),
    footnote: t('需要管理员处理', 'Needs admin action'),
    tone: 'red',
    icon: ShieldAlert,
  },
  {
    key: 'warning',
    label: t('接近上限', 'Near limit'),
    value: formatInteger(warningCount.value),
    footnote: '>= 80%',
    tone: 'amber',
    icon: BellRing,
  },
  {
    key: 'allocations',
    label: t('启用策略', 'Enabled policies'),
    value: formatInteger(enabledAllocationCount.value),
    footnote: t(
      `${formatInteger(hardLimitCount.value)} 条硬限制意图`,
      `${formatInteger(hardLimitCount.value)} hard-limit intents`,
    ),
    tone: 'teal',
    icon: Gauge,
  },
  {
    key: 'pools',
    label: t('账号池', 'Account pools'),
    value: formatInteger(pools.value.length),
    footnote: t(
      `${formatInteger(accounts.value.length)} 个账号可选`,
      `${formatInteger(accounts.value.length)} accounts available`,
    ),
    tone: 'blue',
    icon: Boxes,
  },
])

function accountLabel(account: AccountAllocationAccount): string {
  const email = account.email?.trim()
  const type = account.account_type ? ` / ${account.account_type}` : ''
  return `${email || account.auth_name}${type}`
}

function poolMemberLabel(pool: AccountPool): string {
  if (pool.members.length === 0) {
    return t('未分配账号', 'No accounts')
  }
  return pool.members
    .slice(0, 3)
    .map((member) => member.account?.email || member.auth_name)
    .join(', ')
}

function targetLabel(allocation: UserAccountAllocation): string {
  if (allocation.scope_type === 'pool') {
    return allocation.pool_name || `#${allocation.pool_id ?? '-'}`
  }
  const account = accounts.value.find((item) => item.auth_name === allocation.auth_name)
  return account ? accountLabel(account) : allocation.auth_name || '-'
}

function quotaTypeLabel(value: AccountAllocationQuotaType): string {
  if (value === 'requests') {
    return t('请求', 'requests')
  }
  if (value === 'usd') {
    return 'USD'
  }
  return 'tokens'
}

function periodLabel(value: AccountAllocationPeriod): string {
  if (value === 'daily') {
    return t('每日', 'daily')
  }
  if (value === 'monthly') {
    return t('每月', 'monthly')
  }
  return t('累计', 'all time')
}

function quotaValue(value: number, type: AccountAllocationQuotaType): string {
  if (type === 'usd') {
    return formatUsd(value)
  }
  if (type === 'requests') {
    return formatInteger(Math.round(value))
  }
  return formatCompact(value)
}

function statusTagType(row: AccountAllocationUsage): 'success' | 'warning' | 'error' | 'default' {
  if (row.warning_level === 'over_quota') {
    return 'error'
  }
  if (row.warning_level === 'warning') {
    return 'warning'
  }
  if (row.warning_level === 'disabled') {
    return 'default'
  }
  return 'success'
}

function statusLabel(row: AccountAllocationUsage): string {
  if (row.warning_level === 'over_quota') {
    return t('已超额', 'Over quota')
  }
  if (row.warning_level === 'warning') {
    return t('接近上限', 'Near limit')
  }
  if (row.warning_level === 'disabled') {
    return t('已停用', 'Disabled')
  }
  return t('正常', 'OK')
}

function setPoolFromRow(row: AccountPool | null) {
  editingPoolId.value = row?.id ?? null
  poolName.value = row?.name ?? ''
  poolDescription.value = row?.description ?? ''
  poolAuthNames.value = row?.members.map((member) => member.auth_name) ?? []
}

function openCreatePool() {
  setPoolFromRow(null)
  poolEditorVisible.value = true
}

function openEditPool(row: AccountPool) {
  setPoolFromRow(row)
  poolEditorVisible.value = true
}

async function savePool() {
  const name = poolName.value.trim()
  if (!name) {
    message.error(t('账号池名称不能为空', 'Pool name is required'))
    return
  }
  isSavingPool.value = true
  try {
    const payload = { name, description: poolDescription.value.trim() }
    const pool =
      editingPoolId.value === null
        ? await createAccountPool(payload)
        : await updateAccountPool(editingPoolId.value, payload)
    await replaceAccountPoolMembers(pool.id, { auth_names: poolAuthNames.value })
    message.success(t('账号池已保存', 'Pool saved'))
    poolEditorVisible.value = false
    await refresh()
  } catch (error) {
    message.error(errorText(error, '保存账号池失败', 'Failed to save pool'))
  } finally {
    isSavingPool.value = false
  }
}

async function removePool(row: AccountPool) {
  try {
    await deleteAccountPool(row.id)
    message.success(t('账号池已删除', 'Pool deleted'))
    await refresh()
  } catch (error) {
    message.error(errorText(error, '删除账号池失败', 'Failed to delete pool'))
  }
}

function resetAllocationEditor() {
  editingAllocationId.value = null
  allocationUserId.value = users.value[0]?.id ?? null
  allocationScopeType.value = 'auth'
  allocationAuthName.value = accounts.value[0]?.auth_name ?? null
  allocationPoolId.value = pools.value[0]?.id ?? null
  allocationQuotaType.value = 'tokens'
  allocationQuotaLimit.value = 100000
  allocationPeriod.value = 'monthly'
  allocationHardLimit.value = false
  allocationEnabled.value = true
  allocationNote.value = ''
}

function openCreateAllocation() {
  resetAllocationEditor()
  allocationEditorVisible.value = true
}

function openEditAllocation(row: UserAccountAllocation) {
  editingAllocationId.value = row.id
  allocationUserId.value = row.user_id
  allocationScopeType.value = row.scope_type
  allocationAuthName.value = row.auth_name
  allocationPoolId.value = row.pool_id
  allocationQuotaType.value = row.quota_type
  allocationQuotaLimit.value = row.quota_limit
  allocationPeriod.value = row.period
  allocationHardLimit.value = row.hard_limit
  allocationEnabled.value = row.enabled
  allocationNote.value = row.note
  allocationEditorVisible.value = true
}

function setAllocationQuotaLimit(value: number | null) {
  allocationQuotaLimit.value = value ?? 0
}

function buildAllocationPayload(): UserAccountAllocationPayload | null {
  if (!allocationUserId.value) {
    message.error(t('请选择用户', 'Select a user'))
    return null
  }
  if (allocationQuotaLimit.value <= 0) {
    message.error(t('额度必须大于 0', 'Quota must be greater than 0'))
    return null
  }
  if (allocationScopeType.value === 'auth' && !allocationAuthName.value) {
    message.error(t('请选择账号', 'Select an account'))
    return null
  }
  if (allocationScopeType.value === 'pool' && !allocationPoolId.value) {
    message.error(t('请选择账号池', 'Select a pool'))
    return null
  }
  return {
    user_id: allocationUserId.value,
    scope_type: allocationScopeType.value,
    auth_name: allocationScopeType.value === 'auth' ? allocationAuthName.value : null,
    pool_id: allocationScopeType.value === 'pool' ? allocationPoolId.value : null,
    quota_type: allocationQuotaType.value,
    quota_limit: allocationQuotaLimit.value,
    period: allocationPeriod.value,
    hard_limit: allocationHardLimit.value,
    enabled: allocationEnabled.value,
    note: allocationNote.value.trim(),
  }
}

async function saveAllocation() {
  const payload = buildAllocationPayload()
  if (!payload) {
    return
  }
  isSavingAllocation.value = true
  try {
    if (editingAllocationId.value === null) {
      await createUserAccountAllocation(payload)
    } else {
      await updateUserAccountAllocation(editingAllocationId.value, payload)
    }
    message.success(t('分配策略已保存', 'Allocation saved'))
    allocationEditorVisible.value = false
    await refresh()
  } catch (error) {
    message.error(errorText(error, '保存分配策略失败', 'Failed to save allocation'))
  } finally {
    isSavingAllocation.value = false
  }
}

async function removeAllocation(row: UserAccountAllocation) {
  try {
    await deleteUserAccountAllocation(row.id)
    message.success(t('分配策略已删除', 'Allocation deleted'))
    await refresh()
  } catch (error) {
    message.error(errorText(error, '删除分配策略失败', 'Failed to delete allocation'))
  }
}

async function refresh() {
  isLoading.value = true
  try {
    overview.value = await getAccountAllocationsOverview()
  } catch (error) {
    message.error(errorText(error, '加载额度分配失败', 'Failed to load allocations'))
  } finally {
    isLoading.value = false
  }
}

const poolColumns = computed<DataTableColumns<AccountPool>>(() => [
  {
    title: t('账号池', 'Pool'),
    key: 'name',
    width: 180,
    render: (row) =>
      h('div', { class: 'metric-stack' }, [
        h('span', { class: 'metric-primary' }, row.name),
        h('span', { class: 'metric-muted' }, row.description || t('无备注', 'No note')),
      ]),
  },
  {
    title: t('成员', 'Members'),
    key: 'members',
    minWidth: 260,
    render: (row) =>
      h('div', { class: 'metric-stack' }, [
        h(
          'span',
          { class: 'metric-primary' },
          t(`${formatInteger(row.members.length)} 个账号`, `${formatInteger(row.members.length)} accounts`),
        ),
        h('span', { class: 'metric-muted' }, poolMemberLabel(row)),
      ]),
  },
  {
    title: t('更新时间', 'Updated'),
    key: 'updated_at',
    width: 150,
    render: (row) => formatDateTime(row.updated_at),
  },
  {
    title: '',
    key: 'actions',
    width: 130,
    fixed: 'right',
    render: (row) =>
      h(
        NSpace,
        { size: 4 },
        {
          default: () => [
            h(
              NButton,
              { size: 'small', quaternary: true, onClick: () => openEditPool(row) },
              { default: () => t('编辑', 'Edit') },
            ),
            h(
              NPopconfirm,
              { onPositiveClick: () => removePool(row) },
              {
                trigger: () =>
                  h(
                    NButton,
                    { size: 'small', quaternary: true, type: 'error' },
                    { default: () => t('删除', 'Delete') },
                  ),
                default: () => t(`删除账号池 ${row.name}？`, `Delete pool ${row.name}?`),
              },
            ),
          ],
        },
      ),
  },
])

const usageColumns = computed<DataTableColumns<AccountAllocationUsage>>(() => [
  {
    title: t('用户', 'User'),
    key: 'user',
    width: 140,
    render: (row) =>
      h('div', { class: 'metric-stack' }, [
        h('span', { class: 'metric-primary' }, row.allocation.user_label),
        h('span', { class: 'metric-muted' }, row.allocation.username),
      ]),
  },
  {
    title: t('目标账号', 'Target'),
    key: 'target',
    minWidth: 220,
    render: (row) =>
      h('div', { class: 'metric-stack' }, [
        h('span', { class: 'metric-primary' }, targetLabel(row.allocation)),
        h(
          'span',
          { class: 'metric-muted' },
          row.allocation.scope_type === 'pool'
            ? t(
                `${formatInteger(row.matched_auth_names.length)} 个池成员`,
                `${formatInteger(row.matched_auth_names.length)} pool members`,
              )
            : row.allocation.auth_name || '-',
        ),
      ]),
  },
  {
    title: t('额度', 'Quota'),
    key: 'quota',
    width: 170,
    render: (row) =>
      h('div', { class: 'metric-stack' }, [
        h('span', { class: 'metric-primary' }, quotaValue(row.quota_limit, row.allocation.quota_type)),
        h(
          'span',
          { class: 'metric-muted' },
          `${periodLabel(row.allocation.period)} / ${quotaTypeLabel(row.allocation.quota_type)}`,
        ),
      ]),
  },
  {
    title: t('已用', 'Used'),
    key: 'used',
    width: 190,
    render: (row) =>
      h('div', { class: 'usage-progress-cell' }, [
        h('div', { class: 'usage-progress-title' }, [
          h('span', quotaValue(row.used_value, row.allocation.quota_type)),
          h('strong', `${row.used_percent.toFixed(1)}%`),
        ]),
        h(NProgress, {
          type: 'line',
          percentage: Math.min(row.used_percent, 100),
          status:
            row.warning_level === 'over_quota'
              ? 'error'
              : row.warning_level === 'warning'
                ? 'warning'
                : 'success',
          height: 7,
          showIndicator: false,
        }),
      ]),
  },
  {
    title: t('剩余', 'Remaining'),
    key: 'remaining',
    width: 120,
    render: (row) => quotaValue(row.remaining, row.allocation.quota_type),
  },
  {
    title: t('采集用量', 'Collected usage'),
    key: 'usage',
    width: 180,
    render: (row) =>
      h('div', { class: 'metric-stack' }, [
        h(
          'span',
          { class: 'metric-primary' },
          t(`${formatInteger(row.records)} 次请求`, `${formatInteger(row.records)} requests`),
        ),
        h(
          'span',
          { class: 'metric-muted' },
          `${formatCompact(row.total_tokens)} tokens / ${formatUsd(row.estimated_cost_usd)}`,
        ),
      ]),
  },
  {
    title: t('状态', 'Status'),
    key: 'status',
    width: 120,
    render: (row) =>
      h(NTag, { size: 'small', type: statusTagType(row), bordered: false }, { default: () => statusLabel(row) }),
  },
  {
    title: t('限制', 'Limit'),
    key: 'hard_limit',
    width: 120,
    render: (row) =>
      h(
        NTag,
        { size: 'small', type: row.allocation.hard_limit ? 'warning' : 'default', bordered: false },
        {
          default: () =>
            row.allocation.hard_limit ? t('硬限制意图', 'Hard intent') : t('仅告警', 'Alert only'),
        },
      ),
  },
  {
    title: t('最近告警', 'Last alert'),
    key: 'last_alert_at',
    width: 150,
    render: (row) => formatDateTime(row.last_alert_at),
  },
  {
    title: '',
    key: 'actions',
    width: 130,
    fixed: 'right',
    render: (row) =>
      h(
        NSpace,
        { size: 4 },
        {
          default: () => [
            h(
              NButton,
              { size: 'small', quaternary: true, onClick: () => openEditAllocation(row.allocation) },
              { default: () => t('编辑', 'Edit') },
            ),
            h(
              NPopconfirm,
              { onPositiveClick: () => removeAllocation(row.allocation) },
              {
                trigger: () =>
                  h(
                    NButton,
                    { size: 'small', quaternary: true, type: 'error' },
                    { default: () => t('删除', 'Delete') },
                  ),
                default: () => t('删除这条分配策略？', 'Delete this allocation?'),
              },
            ),
          ],
        },
      ),
  },
])

onMounted(refresh)
</script>

<template>
  <section class="page account-allocations-page">
    <div class="page-header">
      <div>
        <h1 class="page-title">{{ t('额度分配', 'Account Allocations') }}</h1>
        <p class="page-subtitle">
          {{
            t(
              '把 CLIProxyAPI 账号额度切分给指定用户；当前版本先采集归因和告警，后续再接强制拦截。',
              'Split CLIProxyAPI account capacity by user. This MVP observes usage and alerts before enforcement is wired in.',
            )
          }}
        </p>
      </div>
      <NSpace>
        <NButton secondary :loading="isLoading" @click="refresh">{{ t('刷新', 'Refresh') }}</NButton>
        <NButton secondary @click="openCreatePool">{{ t('新建账号池', 'New pool') }}</NButton>
        <NButton type="primary" @click="openCreateAllocation">{{ t('新建分配', 'New allocation') }}</NButton>
      </NSpace>
    </div>

    <NAlert type="info" :bordered="false" class="enforcement-alert">
      {{ overview?.enforcement.message || t('当前为观察告警模式。', 'Current mode: observe and alert.') }}
    </NAlert>

    <div class="metric-grid allocation-metrics">
      <div v-for="metric in metricCards" :key="metric.key" class="metric-card" :class="`is-${metric.tone}`">
        <div class="metric-icon" aria-hidden="true">
          <component :is="metric.icon" :size="20" :stroke-width="2.2" />
        </div>
        <div class="metric-label">{{ metric.label }}</div>
        <div class="metric-value">{{ metric.value }}</div>
        <div class="metric-footnote">{{ metric.footnote }}</div>
      </div>
    </div>

    <section class="panel table-panel">
      <div class="panel-heading">
        <div>
          <h2>{{ t('账号池', 'Account pools') }}</h2>
          <p>{{ t('先把多个 auth 文件合成池，再把池额度分给用户。', 'Group auth files into pools, then allocate pool quota to users.') }}</p>
        </div>
      </div>
      <NDataTable
        size="small"
        :loading="isLoading"
        :columns="poolColumns"
        :data="pools"
        :pagination="{ pageSize: 6 }"
        table-layout="fixed"
        :scroll-x="760"
      />
    </section>

    <section class="panel table-panel allocation-table-panel">
      <div class="panel-heading">
        <div>
          <h2>{{ t('用户账号额度', 'User account quota') }}</h2>
          <p>{{ t('按用户 + 单账号/账号池 + 周期统计用量，超额项会置顶。', 'Usage is grouped by user, account or pool, and period. Alerts are pinned first.') }}</p>
        </div>
      </div>
      <NDataTable
        size="small"
        :loading="isLoading"
        :columns="usageColumns"
        :data="usageRows"
        :pagination="{ pageSize: 10 }"
        table-layout="fixed"
        :scroll-x="1500"
      />
    </section>

    <NModal
      v-model:show="poolEditorVisible"
      preset="card"
      :mask-closable="false"
      :title="editingPoolId === null ? t('新建账号池', 'New pool') : t('编辑账号池', 'Edit pool')"
      :style="{ width: 'min(560px, calc(100vw - 32px))' }"
    >
      <NForm label-placement="top">
        <NFormItem :label="t('名称', 'Name')" required>
          <NInput v-model:value="poolName" placeholder="Pro pool" @keyup.enter="savePool" />
        </NFormItem>
        <NFormItem :label="t('备注', 'Note')">
          <NInput v-model:value="poolDescription" type="textarea" :autosize="{ minRows: 2, maxRows: 4 }" />
        </NFormItem>
        <NFormItem :label="t('池成员', 'Pool members')">
          <NSelect
            v-model:value="poolAuthNames"
            multiple
            filterable
            :options="accountOptions"
            :placeholder="t('选择 auth 账号', 'Select auth accounts')"
          />
        </NFormItem>
        <div class="modal-actions">
          <NButton secondary @click="poolEditorVisible = false">{{ t('取消', 'Cancel') }}</NButton>
          <NButton type="primary" :loading="isSavingPool" @click="savePool">{{ t('保存', 'Save') }}</NButton>
        </div>
      </NForm>
    </NModal>

    <NModal
      v-model:show="allocationEditorVisible"
      preset="card"
      :mask-closable="false"
      :title="editingAllocationId === null ? t('新建分配', 'New allocation') : t('编辑分配', 'Edit allocation')"
      :style="{ width: 'min(680px, calc(100vw - 32px))' }"
    >
      <NForm label-placement="top">
        <div class="allocation-form-grid">
          <NFormItem :label="t('用户', 'User')" required>
            <NSelect v-model:value="allocationUserId" filterable :options="userOptions" />
          </NFormItem>
          <NFormItem :label="t('范围', 'Scope')" required>
            <NSelect v-model:value="allocationScopeType" :options="scopeOptions" />
          </NFormItem>
          <NFormItem v-if="allocationScopeType === 'auth'" :label="t('账号', 'Account')" required>
            <NSelect v-model:value="allocationAuthName" filterable :options="accountOptions" />
          </NFormItem>
          <NFormItem v-else :label="t('账号池', 'Pool')" required>
            <NSelect v-model:value="allocationPoolId" filterable :options="poolOptions" />
          </NFormItem>
          <NFormItem :label="t('额度类型', 'Quota type')" required>
            <NSelect v-model:value="allocationQuotaType" :options="quotaTypeOptions" />
          </NFormItem>
          <NFormItem :label="t('额度上限', 'Quota limit')" required>
            <NInputNumber
              :value="allocationQuotaLimit"
              :min="0"
              :precision="allocationQuotaType === 'usd' ? 8 : 0"
              @update:value="setAllocationQuotaLimit"
            />
          </NFormItem>
          <NFormItem :label="t('周期', 'Period')" required>
            <NSelect v-model:value="allocationPeriod" :options="periodOptions" />
          </NFormItem>
          <NFormItem :label="t('启用', 'Enabled')">
            <NSwitch v-model:value="allocationEnabled" />
          </NFormItem>
          <NFormItem :label="t('硬限制意图', 'Hard-limit intent')">
            <NSwitch v-model:value="allocationHardLimit" />
          </NFormItem>
        </div>
        <NFormItem :label="t('备注', 'Note')">
          <NInput v-model:value="allocationNote" type="textarea" :autosize="{ minRows: 2, maxRows: 4 }" />
        </NFormItem>
        <NAlert type="warning" :bordered="false" class="hard-limit-note">
          {{
            t(
              '硬限制意图只表示未来需要强制拦截；当前页面不会直接阻断 CLIProxyAPI 请求。',
              'Hard-limit intent marks policies that should be enforced later. This page does not block CLIProxyAPI requests yet.',
            )
          }}
        </NAlert>
        <div class="modal-actions">
          <NButton secondary @click="allocationEditorVisible = false">{{ t('取消', 'Cancel') }}</NButton>
          <NButton type="primary" :loading="isSavingAllocation" @click="saveAllocation">{{ t('保存', 'Save') }}</NButton>
        </div>
      </NForm>
    </NModal>
  </section>
</template>

<style scoped>
.account-allocations-page {
  display: grid;
  gap: 18px;
}

.enforcement-alert {
  margin-top: -6px;
}

.allocation-metrics {
  grid-template-columns: repeat(4, minmax(150px, 1fr));
}

.metric-card.is-red .metric-icon {
  color: #d92d20;
  background: rgb(217 45 32 / 10%);
}

.metric-card.is-amber .metric-icon {
  color: #b76e00;
  background: rgb(183 110 0 / 12%);
}

.metric-card.is-teal .metric-icon {
  color: var(--cpa-primary);
  background: var(--cpa-primary-wash);
}

.metric-card.is-blue .metric-icon {
  color: var(--cpa-accent-blue);
  background: rgb(46 117 255 / 10%);
}

.panel-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
}

.panel-heading h2 {
  margin: 0;
  color: var(--cpa-text-strong);
  font-size: 16px;
  font-weight: 780;
}

.panel-heading p {
  margin: 4px 0 0;
  color: var(--cpa-text-muted);
  font-size: 12px;
}

.allocation-table-panel {
  margin-bottom: 8px;
}

.usage-progress-cell {
  display: grid;
  gap: 6px;
}

.usage-progress-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  font-size: 12px;
}

.usage-progress-title strong {
  color: var(--cpa-text-strong);
}

.modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 12px;
}

.allocation-form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 12px;
}

.hard-limit-note {
  margin-top: -4px;
}

@media (max-width: 860px) {
  .allocation-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .allocation-form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
