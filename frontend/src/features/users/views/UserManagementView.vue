<script setup lang="ts">
import type { Component } from 'vue'
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
  NSpace,
  NSwitch,
  NTag,
  NTooltip,
  useMessage,
  type DataTableColumns,
} from 'naive-ui'
import { CircleDollarSign, KeyRound, ShieldCheck, UserRound } from 'lucide-vue-next'

import ApiKeyCreateModal from '@/features/api-keys/components/ApiKeyCreateModal.vue'
import {
  createUserApiKey,
  createUser,
  disableUser,
  enableUser,
  listUsers,
  updateUser,
  updateUserQuota,
} from '@/features/users/api/usersApi'
import { useI18n } from '@/shared/i18n'
import type { ApiKeyCreatePayload, UserSummary } from '@/shared/types/api'
import { copyToClipboard } from '@/shared/utils/clipboard'
import { formatCompact, formatDateTime, formatInteger, formatUsd } from '@/shared/utils/format'

const message = useMessage()
const { errorText, t } = useI18n()
const isLoading = ref(false)
const isSavingUser = ref(false)
const users = ref<UserSummary[]>([])
const editorVisible = ref(false)
const editingUserId = ref<number | null>(null)
const userAccount = ref('')
const userPassword = ref('')
const isUserAdmin = ref(false)
const userNickname = ref('')
const quotaUnlimited = ref(true)
const quotaLifetimeUsd = ref(0)
const quotaMonthlyUsd = ref(0)
const apiKeyCreatorVisible = ref(false)
const apiKeyTarget = ref<UserSummary | null>(null)
const isCreatingApiKey = ref(false)
const createdApiKey = ref<string | null>(null)
const createdApiKeyUserLabel = ref('')
const isEditingFirstUser = computed(() => editingUserId.value === 1)
const apiKeyTargetLabel = computed(() => (apiKeyTarget.value ? apiKeyUserLabel(apiKeyTarget.value) : ''))

interface UserMetricCard {
  key: string
  label: string
  value: string
  footnote: string
  tone: 'teal' | 'blue' | 'purple' | 'green'
  icon: Component
}

const userMetrics = computed<UserMetricCard[]>(() => {
  const activeUsers = users.value.filter((user) => user.disabled_at === null).length
  const adminUsers = users.value.filter((user) => user.is_admin).length
  const boundKeys = users.value.reduce((total, user) => total + user.key_count, 0)
  const todayCost = users.value.reduce((total, user) => total + user.today_estimated_cost_usd, 0)
  return [
    {
      key: 'active',
      label: t('启用用户', 'Active users'),
      value: formatInteger(activeUsers),
      footnote: t(`共 ${formatInteger(users.value.length)} 个账号`, `${formatInteger(users.value.length)} accounts total`),
      tone: 'teal',
      icon: UserRound,
    },
    {
      key: 'admins',
      label: t('管理员', 'Admins'),
      value: formatInteger(adminUsers),
      footnote: t('拥有管理权限', 'Have admin access'),
      tone: 'purple',
      icon: ShieldCheck,
    },
    {
      key: 'keys',
      label: t('绑定 Key', 'Bound keys'),
      value: formatInteger(boundKeys),
      footnote: t('当前用户集合', 'Current users'),
      tone: 'blue',
      icon: KeyRound,
    },
    {
      key: 'cost',
      label: t('今日费用', 'Today cost'),
      value: formatUsd(todayCost),
      footnote: t('按现价估算', 'Estimated at current prices'),
      tone: 'green',
      icon: CircleDollarSign,
    },
  ]
})

function userLabel(row: UserSummary): string {
  return row.nickname.trim() || row.username.trim() || t('未知用户', 'Unknown user')
}

function apiKeyUserLabel(row: UserSummary): string {
  const label = userLabel(row)
  return label === row.username ? row.username : `${label} (${row.username})`
}

function quotaBalanceValue(row: UserSummary, bucket: 'monthly' | 'lifetime'): string {
  if (row.quota.unlimited) {
    return t('无限制', 'Unlimited')
  }

  const value = bucket === 'monthly' ? row.quota.monthly_remaining_usd : row.quota.lifetime_remaining_usd
  return formatUsd(value)
}

function quotaBalanceClass(row: UserSummary): string {
  if (row.quota.paused || !row.quota.can_create_keys) {
    return 'is-error'
  }
  if (row.quota.unpriced_records > 0) {
    return 'is-warning'
  }
  return row.quota.unlimited ? 'is-unlimited' : 'is-normal'
}

function quotaDetail(row: UserSummary): string | null {
  if (row.quota.paused) {
    return t('Key 已因余额暂停', 'Keys paused due to balance')
  }
  if (row.quota.sync_error) {
    return t('同步异常', 'Sync error')
  }
  if (row.quota.unpriced_records > 0) {
    return t(`未定价 ${formatInteger(row.quota.unpriced_records)} 条`, `${formatInteger(row.quota.unpriced_records)} unpriced`)
  }
  return null
}

function todayRequestDetail(row: UserSummary): string {
  if (!row.today_records) {
    return t(`累计 ${formatInteger(row.records)}`, `${formatInteger(row.records)} total`)
  }
  const failed = row.today_failed_records
    ? t(`失败 ${formatInteger(row.today_failed_records)}`, `${formatInteger(row.today_failed_records)} failed`)
    : t('无失败', 'No failures')
  const rate = Math.round((row.today_success_records / row.today_records) * 100)
  return `${rate}% · ${failed}`
}

function todayCostDetail(row: UserSummary): string {
  if (!row.today_records) {
    return t('今日无请求', 'No requests today')
  }
  if (row.today_unpriced_records > 0) {
    return t(`未计价 ${formatInteger(row.today_unpriced_records)}`, `${formatInteger(row.today_unpriced_records)} unpriced`)
  }
  return t('已计价', 'Priced')
}

function lastModelLabel(row: UserSummary): string {
  return row.last_model ?? '-'
}

function lastProviderLabel(row: UserSummary): string {
  return row.last_provider ?? t('未知服务商', 'Unknown provider')
}

function setQuotaLifetimeUsd(value: number | null) {
  quotaLifetimeUsd.value = value ?? 0
}

function setQuotaMonthlyUsd(value: number | null) {
  quotaMonthlyUsd.value = value ?? 0
}

function resetEditor() {
  editingUserId.value = null
  userAccount.value = ''
  userPassword.value = ''
  isUserAdmin.value = false
  userNickname.value = ''
  quotaUnlimited.value = true
  quotaLifetimeUsd.value = 0
  quotaMonthlyUsd.value = 0
}

function openCreateUser() {
  resetEditor()
  userPassword.value = 'password'
  editorVisible.value = true
}

function editUser(row: UserSummary) {
  editingUserId.value = row.id
  userAccount.value = row.username
  userPassword.value = ''
  isUserAdmin.value = row.id === 1 ? true : row.is_admin
  userNickname.value = row.nickname
  quotaUnlimited.value = row.quota.unlimited
  quotaLifetimeUsd.value = row.quota.lifetime_quota_usd ?? 0
  quotaMonthlyUsd.value = row.quota.monthly_quota_usd ?? 0
  editorVisible.value = true
}

async function refresh() {
  isLoading.value = true
  try {
    users.value = await listUsers()
  } catch (error) {
    message.error(errorText(error, '加载用户列表失败', 'Failed to load users'))
  } finally {
    isLoading.value = false
  }
}

function isUserDisabled(row: UserSummary): boolean {
  return row.disabled_at !== null
}

function apiKeyCreationBlockedReason(row: UserSummary): string | null {
  if (isUserDisabled(row)) {
    return t('该用户已禁用，无法创建 API 密钥', 'This user is disabled and cannot create API keys')
  }
  if (!row.quota.can_create_keys) {
    return t('该用户额度不足，无法创建 API 密钥', 'This user has insufficient quota to create API keys')
  }
  return null
}

function openApiKeyCreator(row: UserSummary) {
  const blockedReason = apiKeyCreationBlockedReason(row)
  if (blockedReason) {
    message.error(blockedReason)
    return
  }
  apiKeyTarget.value = row
  apiKeyCreatorVisible.value = true
}

async function createApiKeyForUser(payload: ApiKeyCreatePayload) {
  const target = apiKeyTarget.value
  if (!target || isCreatingApiKey.value) {
    return
  }
  const blockedReason = apiKeyCreationBlockedReason(target)
  if (blockedReason) {
    message.error(blockedReason)
    return
  }
  isCreatingApiKey.value = true
  try {
    const created = await createUserApiKey(target.id, payload)
    createdApiKey.value = created.api_key ?? payload.api_key ?? null
    createdApiKeyUserLabel.value = apiKeyUserLabel(target)
    apiKeyCreatorVisible.value = false
    message.success(t('API 密钥已创建并同步到 CPA', 'API key created and synced to CPA'))
    await refresh()
  } catch (error) {
    message.error(errorText(error, '创建 API 密钥失败', 'Failed to create API key'))
  } finally {
    isCreatingApiKey.value = false
  }
}

async function copyCreatedApiKey() {
  if (!createdApiKey.value) {
    return
  }
  try {
    await copyToClipboard(createdApiKey.value)
    message.success(t('API 密钥已复制', 'API key copied'))
  } catch (error) {
    message.error(errorText(error, '复制失败', 'Copy failed'))
  }
}

function closeCreatedApiKey() {
  createdApiKey.value = null
  createdApiKeyUserLabel.value = ''
}

function renderApiKeyCreator(row: UserSummary) {
  const blockedReason = apiKeyCreationBlockedReason(row)
  const button = h(
    NButton,
    {
      size: 'small',
      quaternary: true,
      type: 'primary',
      disabled: Boolean(blockedReason),
      onClick: () => openApiKeyCreator(row),
    },
    { default: () => t('创建 Key', 'Create key') },
  )
  if (!blockedReason) {
    return button
  }
  return h(
    NTooltip,
    { placement: 'top' },
    {
      trigger: () => h('span', { class: 'disabled-action-trigger' }, [button]),
      default: () => blockedReason,
    },
  )
}

async function disableUserRow(row: UserSummary) {
  try {
    await disableUser(row.id)
    message.success(t('用户已禁用', 'User disabled'))
    await refresh()
  } catch (error) {
    message.error(errorText(error, '禁用用户失败', 'Failed to disable user'))
  }
}

async function enableUserRow(row: UserSummary) {
  try {
    await enableUser(row.id)
    message.success(t('用户已启用', 'User enabled'))
    await refresh()
  } catch (error) {
    message.error(errorText(error, '启用用户失败', 'Failed to enable user'))
  }
}

async function saveUser() {
  const nickname = userNickname.value.trim()
  if (!nickname) {
    message.error(t('用户昵称不能为空', 'User nickname is required'))
    return
  }
  const username = userAccount.value.trim()
  if (!username) {
    message.error(t('账号不能为空', 'Account is required'))
    return
  }
  const isEditing = editingUserId.value !== null
  const password = userPassword.value.trim()
  if (!isEditing && !password) {
    message.error(t('密码不能为空', 'Password is required'))
    return
  }
  isSavingUser.value = true
  try {
    const payload = {
      username,
      password: password || undefined,
      is_admin: isEditingFirstUser.value ? true : isUserAdmin.value,
      nickname,
    }
    const saved =
      editingUserId.value !== null
        ? await updateUser(editingUserId.value, payload)
        : await createUser(payload)
    await updateUserQuota(saved.id, {
      lifetime_quota_usd: quotaUnlimited.value ? null : quotaLifetimeUsd.value,
      monthly_quota_usd: quotaUnlimited.value ? null : quotaMonthlyUsd.value,
    })
    message.success(isEditing ? t('用户已保存', 'User saved') : t('用户已创建', 'User created'))
    editorVisible.value = false
    resetEditor()
    await refresh()
  } catch (error) {
    message.error(errorText(error, '保存用户失败', 'Failed to save user'))
  } finally {
    isSavingUser.value = false
  }
}

const columns = computed<DataTableColumns<UserSummary>>(() => [
  {
    title: t('用户昵称', 'User nickname'),
    key: 'nickname',
    width: 120,
    render: (row) => userLabel(row),
  },
  {
    title: t('账号', 'Account'),
    key: 'username',
    width: 130,
    render: (row) => row.username,
  },
  {
    title: t('角色', 'Role'),
    key: 'is_admin',
    width: 90,
    render: (row) => (row.is_admin ? t('管理员', 'Admin') : t('普通用户', 'Standard user')),
  },
  {
    title: t('状态', 'Status'),
    key: 'disabled_at',
    width: 90,
    render: (row) =>
      h(
        NTag,
        {
          size: 'small',
          type: isUserDisabled(row) ? 'warning' : 'success',
          bordered: false,
        },
        { default: () => (isUserDisabled(row) ? t('已禁用', 'Disabled') : t('启用中', 'Enabled')) },
      ),
  },
  {
    title: t('余额', 'Balance'),
    key: 'quota',
    width: 210,
    render: (row) => {
      const detail = quotaDetail(row)
      return h('div', { class: ['metric-stack', 'quota-balance-stack'] }, [
        h('div', { class: ['quota-balance-row', 'is-monthly', quotaBalanceClass(row)] }, [
          h('span', { class: 'quota-balance-label' }, t('每月余额：', 'Monthly: ')),
          h('strong', { class: 'quota-balance-value' }, quotaBalanceValue(row, 'monthly')),
        ]),
        h('div', { class: ['quota-balance-row', 'is-lifetime', quotaBalanceClass(row)] }, [
          h('span', { class: 'quota-balance-label' }, t('不限时余额：', 'Lifetime: ')),
          h('strong', { class: 'quota-balance-value' }, quotaBalanceValue(row, 'lifetime')),
        ]),
        ...(detail
          ? [
              h(
                'span',
                { class: ['metric-muted', 'quota-balance-detail', { 'is-error': row.quota.sync_error || row.quota.paused }] },
                detail,
              ),
            ]
          : []),
      ])
    },
  },
  {
    title: t('API KEY 数量', 'API keys'),
    key: 'key_count',
    width: 95,
    render: (row) => t(`${formatInteger(row.key_count)} 个`, `${formatInteger(row.key_count)} keys`),
  },
  {
    title: t('今日请求', 'Today requests'),
    key: 'today_records',
    width: 140,
    render: (row) =>
      h('div', { class: 'metric-stack' }, [
        h('span', { class: 'metric-primary' }, formatInteger(row.today_records)),
        h('span', { class: 'metric-muted' }, todayRequestDetail(row)),
      ]),
  },
  {
    title: t('今日输入', 'Today input'),
    key: 'today_input_tokens',
    width: 120,
    render: (row) => formatCompact(row.today_input_tokens),
  },
  {
    title: t('今日输出', 'Today output'),
    key: 'today_output_tokens',
    width: 120,
    render: (row) => formatCompact(row.today_output_tokens),
  },
  {
    title: t('今日缓存', 'Today cache'),
    key: 'today_cached_tokens',
    width: 120,
    render: (row) => formatCompact(row.today_cached_tokens),
  },
  {
    title: t('今日总 Token', 'Today total tokens'),
    key: 'today_total_tokens',
    width: 145,
    render: (row) => formatCompact(row.today_total_tokens),
  },
  {
    title: t('今日费用', 'Today cost'),
    key: 'today_estimated_cost_usd',
    width: 150,
    render: (row) =>
      h('div', { class: 'metric-stack' }, [
        h('span', { class: 'metric-primary' }, formatUsd(row.today_estimated_cost_usd)),
        h(
          'span',
          { class: ['metric-muted', { 'is-error': row.today_unpriced_records > 0 }] },
          todayCostDetail(row),
        ),
      ]),
  },
  {
    title: t('最近模型', 'Recent model'),
    key: 'last_model',
    width: 160,
    render: (row) =>
      h('div', { class: 'metric-stack' }, [
        h('span', { class: 'model-value' }, lastModelLabel(row)),
        h('span', { class: 'metric-muted' }, lastProviderLabel(row)),
      ]),
  },
  {
    title: t('最近使用', 'Last used'),
    key: 'last_seen_at',
    width: 150,
    render: (row) => formatDateTime(row.last_seen_at),
  },
  {
    title: '',
    key: 'actions',
    width: 190,
    fixed: 'right',
    render: (row) =>
      h(
        NSpace,
        { size: 4 },
        {
          default: () => [
            row.is_admin ? null : renderApiKeyCreator(row),
            h(
              NButton,
              { size: 'small', quaternary: true, onClick: () => editUser(row) },
              { default: () => t('编辑', 'Edit') },
            ),
            row.id === 1
              ? null
              : isUserDisabled(row)
                ? h(
                    NPopconfirm,
                    { onPositiveClick: () => enableUserRow(row) },
                    {
                      trigger: () =>
                        h(
                          NButton,
                          { size: 'small', quaternary: true, type: 'primary' },
                          { default: () => t('启用', 'Enable') },
                        ),
                      default: () => t(`启用用户 ${userLabel(row)} 并恢复其 API KEY？`, `Enable user ${userLabel(row)} and restore their API keys?`),
                    },
                  )
                : h(
                    NPopconfirm,
                    { onPositiveClick: () => disableUserRow(row) },
                    {
                      trigger: () =>
                        h(
                          NButton,
                          { size: 'small', quaternary: true, type: 'warning' },
                          { default: () => t('禁用', 'Disable') },
                        ),
                      default: () => t(`禁用用户 ${userLabel(row)} 并从 CPA 移除其 API KEY？`, `Disable user ${userLabel(row)} and remove their API keys from CPA?`),
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
  <section class="page">
    <div class="page-header">
      <div>
        <h1 class="page-title">{{ t('用户管理', 'User Management') }}</h1>
        <p class="page-subtitle">{{ t('管理用户昵称、登录账号、密码和角色', 'Manage user nicknames, sign-in accounts, passwords, and roles') }}</p>
      </div>
      <NSpace>
        <NButton secondary :loading="isLoading" @click="refresh">{{ t('刷新', 'Refresh') }}</NButton>
        <NButton type="primary" @click="openCreateUser">{{ t('增加用户', 'Add user') }}</NButton>
      </NSpace>
    </div>

    <div class="metric-grid user-metrics">
      <div v-for="metric in userMetrics" :key="metric.key" class="metric-card" :class="`is-${metric.tone}`">
        <div class="metric-icon" aria-hidden="true">
          <component :is="metric.icon" :size="20" :stroke-width="2.2" />
        </div>
        <div class="metric-label">{{ metric.label }}</div>
        <div class="metric-value">{{ metric.value }}</div>
        <div class="metric-footnote">{{ metric.footnote }}</div>
      </div>
    </div>

    <section class="panel table-panel">
      <div v-if="createdApiKey" class="created-key-box">
        <div class="created-key-main">
          <div class="created-key-title">
            {{ t(`${createdApiKeyUserLabel} 的新密钥`, `New key for ${createdApiKeyUserLabel}`) }}
          </div>
          <div class="created-key-value">{{ createdApiKey }}</div>
        </div>
        <NSpace>
          <NButton secondary @click="copyCreatedApiKey">{{ t('复制', 'Copy') }}</NButton>
          <NButton tertiary @click="closeCreatedApiKey">{{ t('关闭', 'Close') }}</NButton>
        </NSpace>
      </div>

      <NDataTable
        size="small"
        :loading="isLoading"
        :columns="columns"
        :data="users"
        :pagination="{ pageSize: 12 }"
        table-layout="fixed"
        :scroll-x="2000"
      />
    </section>

    <ApiKeyCreateModal
      v-model:show="apiKeyCreatorVisible"
      :title="t('为用户创建 API 密钥', 'Create API key for user')"
      :target-label="apiKeyTargetLabel"
      :loading="isCreatingApiKey"
      @submit="createApiKeyForUser"
    />

    <NModal
      v-model:show="editorVisible"
      preset="card"
      :mask-closable="false"
      :closable="false"
      :title="editingUserId ? t('编辑用户', 'Edit user') : t('增加用户', 'Add user')"
      :style="{ width: 'min(520px, calc(100vw - 32px))' }"
    >
      <NAlert v-if="editingUserId === null" type="warning" :bordered="false" class="user-editor-warning">
        {{ t('账号一旦创建，不允许删除，只允许禁用，请谨慎操作。', 'Accounts cannot be deleted after creation. They can only be disabled, so proceed carefully.') }}
      </NAlert>

      <NForm label-placement="top">
        <NFormItem :label="t('用户昵称', 'User nickname')" required>
          <NInput
            v-model:value="userNickname"
            :placeholder="t('例如：研发用户', 'Example: Engineering user')"
            @keyup.enter="saveUser"
          />
        </NFormItem>
        <NFormItem :label="t('账号', 'Account')" required>
          <NInput
            v-model:value="userAccount"
            autocomplete="username"
            :disabled="editingUserId !== null"
            :placeholder="t('例如：user001', 'Example: user001')"
            @keyup.enter="saveUser"
          />
        </NFormItem>
        <NFormItem :label="t('密码', 'Password')" :required="editingUserId === null">
          <NInput
            v-model:value="userPassword"
            type="password"
            show-password-on="mousedown"
            autocomplete="new-password"
            :placeholder="editingUserId ? t('留空不修改密码', 'Leave blank to keep the current password') : t('请输入登录密码', 'Enter a sign-in password')"
            @keyup.enter="saveUser"
          />
        </NFormItem>
        <NFormItem :label="t('是否设为管理员', 'Set as admin')">
          <NSwitch v-model:value="isUserAdmin" :disabled="isEditingFirstUser" />
        </NFormItem>
        <NFormItem :label="t('余额设置', 'Balance settings')">
          <div class="quota-unlimited-row">
            <div>
              <div class="quota-unlimited-title">{{ t('不限制余额', 'Unlimited balance') }}</div>
              <div class="quota-unlimited-desc">{{ t('开启后不扣余额，也不会因余额暂停 API Key。', 'When enabled, balances are not deducted and API keys are not paused due to balance.') }}</div>
            </div>
            <NSwitch v-model:value="quotaUnlimited" />
          </div>
        </NFormItem>
        <div class="form-grid quota-editor-grid">
          <NFormItem :label="t('不限时余额 USD', 'Lifetime balance USD')">
            <NInputNumber
              :value="quotaLifetimeUsd"
              :disabled="quotaUnlimited"
              :min="0"
              :precision="8"
              placeholder="0"
              @update:value="setQuotaLifetimeUsd"
            />
          </NFormItem>
          <NFormItem :label="t('每月余额 USD', 'Monthly balance USD')">
            <NInputNumber
              :value="quotaMonthlyUsd"
              :disabled="quotaUnlimited"
              :min="0"
              :precision="8"
              placeholder="0"
              @update:value="setQuotaMonthlyUsd"
            />
          </NFormItem>
        </div>
        <NAlert type="info" :bordered="false" class="quota-editor-hint">
          {{ t('关闭不限制后，扣费顺序：先扣每月余额，不足部分再扣不限时余额；两者都无剩余时暂停该用户的 API Key。', 'After unlimited balance is disabled, charges are deducted from monthly balance first, then lifetime balance. If neither has remaining balance, the user API keys are paused.') }}
        </NAlert>
        <div class="user-editor-actions">
          <NButton secondary @click="editorVisible = false">{{ t('取消', 'Cancel') }}</NButton>
          <NButton type="primary" :loading="isSavingUser" @click="saveUser">
            {{ editingUserId ? t('保存', 'Save') : t('创建', 'Create') }}
          </NButton>
        </div>
      </NForm>
    </NModal>
  </section>
</template>

<style scoped>
.user-metrics {
  grid-template-columns: repeat(4, minmax(150px, 1fr));
}

.user-editor-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.user-editor-warning {
  margin-bottom: 12px;
}

.created-key-box {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  min-width: 0;
  margin-bottom: 14px;
  padding: 16px;
  border: 1px solid var(--cpa-border);
  border-radius: var(--cpa-radius);
  background:
    linear-gradient(135deg, rgb(0 154 168 / 10%), rgb(29 141 255 / 7%)),
    var(--cpa-primary-wash);
  box-shadow: var(--cpa-shadow-hairline);
}

.created-key-main {
  min-width: 0;
}

.created-key-title {
  margin-bottom: 4px;
  font-weight: 700;
}

.created-key-value {
  overflow-wrap: anywhere;
  font-family: Consolas, 'SFMono-Regular', 'Microsoft YaHei UI', monospace;
  font-size: 13px;
}

:global(.disabled-action-trigger) {
  display: inline-flex;
}

.quota-editor-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px 12px;
}

.quota-editor-hint {
  margin: -2px 0 12px;
}

.quota-unlimited-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  gap: 16px;
  min-width: 0;
  padding: 10px 12px;
  border: 1px solid var(--cpa-border);
  border-radius: var(--cpa-radius);
  background: var(--cpa-surface);
}

.quota-unlimited-title {
  color: var(--cpa-text-strong);
  font-size: 13px;
  font-weight: 760;
}

.quota-unlimited-desc {
  margin-top: 2px;
  color: var(--cpa-muted);
  font-size: 12px;
  line-height: 1.35;
}

:global(.metric-stack) {
  display: grid;
  gap: 2px;
  min-width: 0;
  line-height: 1.28;
}

:global(.quota-balance-stack) {
  gap: 3px;
}

:global(.quota-balance-row) {
  display: inline-flex;
  align-items: center;
  min-width: 0;
  width: fit-content;
  max-width: 100%;
  padding: 2px 7px;
  overflow: hidden;
  border-radius: var(--cpa-radius-sm);
  font-size: 12px;
  line-height: 1.35;
  white-space: nowrap;
}

:global(.quota-balance-row.is-monthly.is-normal) {
  background: var(--cpa-success-weak);
  color: var(--cpa-success);
}

:global(.quota-balance-row.is-lifetime.is-normal) {
  background: var(--cpa-primary-weak);
  color: var(--cpa-primary);
}

:global(.quota-balance-row.is-unlimited) {
  background: var(--cpa-primary-wash);
  color: var(--cpa-primary);
}

:global(.quota-balance-row.is-warning) {
  background: var(--cpa-warning-weak);
  color: var(--cpa-warning);
}

:global(.quota-balance-row.is-error) {
  background: var(--cpa-danger-weak);
  color: var(--cpa-danger);
}

:global(.quota-balance-label),
:global(.quota-balance-value) {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

:global(.quota-balance-label) {
  flex: 0 0 auto;
  font-weight: 600;
}

:global(.quota-balance-value) {
  font-weight: 760;
}

:global(.metric-primary) {
  font-weight: 600;
}

:global(.metric-muted) {
  min-width: 0;
  overflow: hidden;
  color: var(--cpa-muted);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:global(.metric-muted.is-error) {
  color: var(--cpa-danger);
}

:global(.model-value) {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 900px) {
  .user-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 560px) {
  .user-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .quota-editor-grid {
    grid-template-columns: 1fr;
  }

  .created-key-box {
    align-items: stretch;
    flex-direction: column;
  }
}
</style>
