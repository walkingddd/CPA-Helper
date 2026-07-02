<script setup lang="ts">
import { computed, h, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import {
  NAlert,
  NButton,
  NDataTable,
  NForm,
  NFormItem,
  NIcon,
  NInput,
  NInputNumber,
  NSelect,
  NSpace,
  NSwitch,
  NTag,
  useMessage,
  type DataTableColumns,
} from 'naive-ui'
import {
  Activity,
  Copy,
  ExternalLink,
  Gauge,
  LogIn,
  PauseCircle,
  ShieldAlert,
  ShieldCheck,
  Trash2,
  Users,
} from 'lucide-vue-next'

import {
  clearCodexKeeperLogs,
  getCodexKeeperSettings,
  getCodexKeeperOAuthStatus,
  getCodexKeeperStatus,
  listCodexKeeperAccounts,
  previewCodexKeeperSchedule,
  runCodexKeeperOnce,
  startCodexKeeper,
  startCodexKeeperOAuth,
  stopCodexKeeper,
  submitCodexKeeperOAuthCallback,
  updateCodexKeeperSettings,
} from '@/features/codex-keeper/api/codexKeeperApi'
import type {
  CodexKeeperAccount,
  CodexKeeperOAuthProvider,
  CodexKeeperPriorityRule,
  CodexKeeperSettingsUpdatePayload,
  CodexKeeperStatus,
} from '@/shared/types/api'
import { useI18n } from '@/shared/i18n'
import { copyToClipboard } from '@/shared/utils/clipboard'
import { formatDateTime, formatInteger } from '@/shared/utils/format'

type LogTone = 'danger' | 'debug' | 'default' | 'info' | 'warning'

interface ParsedLogLine {
  component: string
  key: string
  level: string
  message: string
  raw: string
  time: string
  tone: LogTone
}

const message = useMessage()
const { errorText, keeperStatusText, serverText, t } = useI18n()
const isLoading = ref(false)
const isSaving = ref(false)
const isActing = ref(false)
const status = ref<CodexKeeperStatus | null>(null)
const accounts = ref<CodexKeeperAccount[]>([])
const priorityRules = ref<CodexKeeperPriorityRule[]>([])
const nextRunTimes = ref<string[]>([])
const schedulePreviewError = ref('')
const logBodyRef = ref<HTMLElement | null>(null)
const shouldFollowLatestLog = ref(true)
let statusTimer: number | undefined
let schedulePreviewTimer: number | undefined
let oauthStatusTimer: number | undefined

const oauthProviderOptions: Array<{ label: string; value: CodexKeeperOAuthProvider }> = [
  { label: 'Codex / OpenAI', value: 'codex' },
  { label: 'Claude', value: 'anthropic' },
  { label: 'Gemini CLI', value: 'gemini' },
  { label: 'Antigravity', value: 'antigravity' },
  { label: 'Kimi', value: 'kimi' },
]

const oauthProvider = ref<CodexKeeperOAuthProvider>('codex')
const oauthProjectID = ref('')
const oauthCallbackURL = ref('')
const oauthFlow = ref<Awaited<ReturnType<typeof startCodexKeeperOAuth>> | null>(null)
const oauthStatus = ref<Awaited<ReturnType<typeof getCodexKeeperOAuthStatus>> | null>(null)
const isStartingOAuth = ref(false)
const isCheckingOAuth = ref(false)
const isSubmittingOAuthCallback = ref(false)

const conditionalRefreshIntervalOptions = computed(() => [
  { label: t('关闭', 'Off'), value: 0 },
  { label: t('5 秒', '5 seconds'), value: 5 },
  { label: t('10 秒', '10 seconds'), value: 10 },
  { label: t('30 秒', '30 seconds'), value: 30 },
  { label: t('60 秒', '60 seconds'), value: 60 },
])

const form = reactive({
  schedule_cron: '*/30 * * * *',
  quota_threshold: 100,
  usage_timeout_seconds: 30,
  cpa_timeout_seconds: 30,
  max_retries: 2,
  worker_threads: 8,
  conditional_refresh_interval_seconds: 30,
  account_refresh_cache_minutes: 10,
  dry_run: true,
  enable_credential_websockets: false,
  auto_start_daemon: false,
})

const accountTotalCount = computed(() => accounts.value.length)
const enabledAccountCount = computed(() => accounts.value.filter((account) => !account.disabled).length)
const disabledAccountCount = computed(() => accounts.value.filter((account) => account.disabled).length)
const unauthorizedErrorAccountCount = computed(
  () => accounts.value.filter((account) => account.last_status_code === 401).length,
)
const quotaExhaustedAccountCount = computed(
  () => accounts.value.filter(isQuotaExhaustedAccount).length,
)
const isRunning = computed(() => status.value?.running === true)
const runningModes = computed(() => new Set(status.value?.running_modes ?? []))
const isDaemonRunning = computed(() => status.value?.daemon_running === true)
const isRunOnceBlocked = computed(
  () => runningModes.value.has('once') || runningModes.value.has('daemon'),
)
const statusLogs = computed(() =>
  Array.isArray(status.value?.logs) ? status.value.logs : [],
)
const parsedLogLines = computed(() =>
  statusLogs.value
    .map((line, index) => parseLogLine(line, index))
    .filter((line): line is ParsedLogLine => line !== null),
)
const logText = computed(() => parsedLogLines.value.map((line) => line.raw).join('\n'))
const displayedLogLines = computed(() =>
  [...parsedLogLines.value].reverse(),
)
const displayedPriorityRules = computed(() =>
  [...priorityRules.value].sort((left, right) => {
    const priorityDiff = Number(right.priority) - Number(left.priority)
    if (priorityDiff !== 0) {
      return priorityDiff
    }
    return left.account_type.localeCompare(right.account_type)
  }),
)
const stateType = computed(() => {
  if (isRunning.value || isDaemonRunning.value) {
    return 'success'
  }
  if (status.value?.state === 'error' || status.value?.state === 'failed') {
    return 'error'
  }
  if (status.value?.state === 'stopping') {
    return 'warning'
  }
  return 'default'
})
const statusDetailText = computed(() => {
  const detail = status.value?.detail
  if (isDaemonRunning.value && !isRunning.value) {
    return t('自动巡检已开启', 'Automatic inspection is enabled')
  }
  if (!detail) {
    return t('未运行', 'Not running')
  }
  return keeperStatusText(detail)
})
const statusFootnoteText = computed(() =>
  isDaemonRunning.value ? t('等待 Cron 调度', 'Waiting for Cron schedule') : t('后台自动巡检', 'Background automatic inspection'),
)

const oauthProviderNeedsProjectID = computed(() => oauthProvider.value === 'gemini')
const oauthStatusText = computed(() => {
  if (oauthStatus.value?.completed) {
    return t('OAuth 登录已完成，auth file 已由 CLIProxyAPI 写入。', 'OAuth sign-in is complete. CLIProxyAPI has written the auth file.')
  }
  if (oauthStatus.value?.status) {
    return t(`当前状态：${oauthStatus.value.status}`, `Current status: ${oauthStatus.value.status}`)
  }
  if (oauthFlow.value?.state) {
    return t('等待浏览器完成授权。', 'Waiting for browser authorization.')
  }
  return ''
})

watch(logText, () => {
  if (shouldFollowLatestLog.value) {
    scrollLogToTop()
  }
})

function applySettings(nextSettings: Awaited<ReturnType<typeof getCodexKeeperSettings>>) {
  form.schedule_cron = nextSettings.schedule_cron
  form.quota_threshold = nextSettings.quota_threshold
  form.usage_timeout_seconds = nextSettings.usage_timeout_seconds
  form.cpa_timeout_seconds = nextSettings.cpa_timeout_seconds
  form.max_retries = nextSettings.max_retries
  form.worker_threads = nextSettings.worker_threads
  form.conditional_refresh_interval_seconds = nextSettings.conditional_refresh_interval_seconds
  form.account_refresh_cache_minutes = nextSettings.account_refresh_cache_minutes
  form.dry_run = nextSettings.dry_run
  form.enable_credential_websockets = nextSettings.enable_credential_websockets
  form.auto_start_daemon = nextSettings.auto_start_daemon
  nextRunTimes.value = nextSettings.next_run_times
  schedulePreviewError.value = ''
  priorityRules.value = nextSettings.priority_rules.map((rule) => ({ ...rule }))
}

function isQuotaExhaustedAccount(account: CodexKeeperAccount): boolean {
  return !account.disabled && (account.priority ?? 0) === -1
}

async function loadAll() {
  isLoading.value = true
  try {
    const [settings, nextStatus, accountResponse] = await Promise.all([
      getCodexKeeperSettings(),
      getCodexKeeperStatus(),
      listCodexKeeperAccounts(),
    ])
    applySettings(settings)
    status.value = nextStatus
    accounts.value = accountResponse.items
  } catch (error) {
    message.error(errorText(error, '加载账号巡检失败', 'Failed to load account inspection'))
  } finally {
    isLoading.value = false
  }
}

async function loadStatus() {
  try {
    const [nextStatus, accountResponse] = await Promise.all([
      getCodexKeeperStatus(),
      listCodexKeeperAccounts(),
    ])
    status.value = nextStatus
    accounts.value = accountResponse.items
  } catch {
    return
  }
}

function normalizedRules(): CodexKeeperPriorityRule[] {
  const seen = new Set<string>()
  return priorityRules.value
    .map((rule) => ({
      account_type: rule.account_type.trim().toLowerCase(),
      priority: Number(rule.priority),
    }))
    .filter((rule) => {
      if (!rule.account_type || seen.has(rule.account_type)) {
        return false
      }
      seen.add(rule.account_type)
      return rule.priority >= 0 && rule.priority <= 20
    })
}

async function saveSettings() {
  const rules = normalizedRules()
  if (rules.length !== priorityRules.value.length) {
    message.error(t('账号类型不可为空或重复，优先级必须在 0 ~ 20', 'Account types cannot be empty or duplicated, and priorities must be 0-20'))
    return
  }
  isSaving.value = true
  try {
    const payload: CodexKeeperSettingsUpdatePayload = {
      ...form,
      priority_rules: rules,
    }
    const saved = await updateCodexKeeperSettings(payload)
    applySettings(saved)
    message.success(t('巡检配置已保存', 'Inspection settings saved'))
  } catch (error) {
    message.error(errorText(error, '保存巡检配置失败', 'Failed to save inspection settings'))
  } finally {
    isSaving.value = false
  }
}

async function loadSchedulePreview() {
  const scheduleCron = form.schedule_cron.trim()
  if (!scheduleCron) {
    nextRunTimes.value = []
    schedulePreviewError.value = t('请填写 Cron 表达式', 'Enter a Cron expression')
    return
  }
  try {
    const preview = await previewCodexKeeperSchedule({ schedule_cron: scheduleCron })
    if (form.schedule_cron.trim() !== scheduleCron) {
      return
    }
    nextRunTimes.value = preview.next_run_times
    schedulePreviewError.value = ''
  } catch (error) {
    if (form.schedule_cron.trim() !== scheduleCron) {
      return
    }
    nextRunTimes.value = []
    schedulePreviewError.value = errorText(
      error,
      'Cron 表达式无效，请使用 5 段格式',
      'Invalid Cron expression. Use the 5-field format',
    )
  }
}

function queueSchedulePreview() {
  if (schedulePreviewTimer !== undefined) {
    window.clearTimeout(schedulePreviewTimer)
  }
  schedulePreviewTimer = window.setTimeout(() => {
    void loadSchedulePreview()
  }, 350)
}

async function runAction(action: () => Promise<void>, successText: string) {
  isActing.value = true
  try {
    await action()
    message.success(successText)
    await loadStatus()
  } catch (error) {
    message.error(errorText(error, '操作失败', 'Operation failed'))
  } finally {
    isActing.value = false
  }
}

async function startOAuthLogin() {
  isStartingOAuth.value = true
  oauthStatus.value = null
  try {
    const payload = {
      provider: oauthProvider.value,
      project_id: oauthProviderNeedsProjectID.value ? oauthProjectID.value.trim() : undefined,
    }
    const flow = await startCodexKeeperOAuth(payload)
    oauthFlow.value = flow
    oauthCallbackURL.value = ''
    const popup = window.open(flow.url, '_blank', 'noopener,noreferrer')
    if (!popup) {
      message.info(t('OAuth 链接已生成，请手动打开授权页。', 'OAuth link generated. Open the authorization page manually.'))
    } else {
      message.success(t('已打开 OAuth 授权页', 'OAuth authorization page opened'))
    }
    startOAuthStatusPolling()
  } catch (error) {
    message.error(errorText(error, '创建 OAuth 登录失败', 'Failed to create OAuth sign-in'))
  } finally {
    isStartingOAuth.value = false
  }
}

function openOAuthURL() {
  if (!oauthFlow.value?.url) {
    return
  }
  window.open(oauthFlow.value.url, '_blank', 'noopener,noreferrer')
}

function startOAuthStatusPolling() {
  stopOAuthStatusPolling()
  if (!oauthFlow.value?.state) {
    return
  }
  void checkOAuthStatus({ silent: true })
  oauthStatusTimer = window.setInterval(() => {
    void checkOAuthStatus({ silent: true })
  }, 3000)
}

function stopOAuthStatusPolling() {
  if (oauthStatusTimer !== undefined) {
    window.clearInterval(oauthStatusTimer)
    oauthStatusTimer = undefined
  }
}

async function checkOAuthStatus(options: { silent?: boolean } = {}) {
  const state = oauthFlow.value?.state
  if (!state) {
    if (!options.silent) {
      message.info(t('当前没有可查询的 OAuth state', 'There is no OAuth state to check yet'))
    }
    return
  }
  isCheckingOAuth.value = true
  try {
    const nextStatus = await getCodexKeeperOAuthStatus(state)
    oauthStatus.value = nextStatus
    if (nextStatus.completed) {
      stopOAuthStatusPolling()
      await loadStatus()
      message.success(t('OAuth 登录已完成', 'OAuth sign-in completed'))
    } else if (!options.silent) {
      message.info(oauthStatusText.value || t('OAuth 登录仍在等待完成', 'OAuth sign-in is still pending'))
    }
  } catch (error) {
    if (!options.silent) {
      message.error(errorText(error, '查询 OAuth 状态失败', 'Failed to check OAuth status'))
    }
  } finally {
    isCheckingOAuth.value = false
  }
}

async function submitOAuthCallback() {
  if (!oauthCallbackURL.value.trim()) {
    message.error(t('请粘贴 OAuth callback URL', 'Paste the OAuth callback URL'))
    return
  }
  isSubmittingOAuthCallback.value = true
  try {
    const result = await submitCodexKeeperOAuthCallback({
      provider: oauthFlow.value?.provider ?? oauthProvider.value,
      redirect_url: oauthCallbackURL.value.trim(),
    })
    oauthStatus.value = {
      state: oauthFlow.value?.state ?? '',
      status: result.status || 'ok',
      completed: true,
      raw: result.raw,
    }
    stopOAuthStatusPolling()
    await loadStatus()
    message.success(t('OAuth callback 已提交', 'OAuth callback submitted'))
  } catch (error) {
    message.error(errorText(error, '提交 OAuth callback 失败', 'Failed to submit OAuth callback'))
  } finally {
    isSubmittingOAuthCallback.value = false
  }
}

function addRule() {
  priorityRules.value.push({ account_type: '', priority: 0 })
}

function removeRule(rule: CodexKeeperPriorityRule) {
  const index = priorityRules.value.indexOf(rule)
  if (index >= 0) {
    priorityRules.value.splice(index, 1)
  }
}

function logTone(level: string): LogTone {
  const normalizedLevel = level.trim().toUpperCase()
  if (normalizedLevel === 'INFO') {
    return 'info'
  }
  if (normalizedLevel === 'WARNING' || normalizedLevel === 'WARN') {
    return 'warning'
  }
  if (normalizedLevel === 'ERROR' || normalizedLevel === 'CRITICAL' || normalizedLevel === 'FATAL') {
    return 'danger'
  }
  if (normalizedLevel === 'DEBUG') {
    return 'debug'
  }
  return 'default'
}

function parseSlogFields(line: string): Record<string, string> | null {
  const fields: Record<string, string> = {}
  let cursor = 0
  while (cursor < line.length) {
    while (line[cursor] === ' ') {
      cursor += 1
    }
    if (cursor >= line.length) {
      break
    }
    const keyStart = cursor
    while (cursor < line.length && line[cursor] !== '=' && line[cursor] !== ' ') {
      cursor += 1
    }
    if (cursor >= line.length || line[cursor] !== '=') {
      return null
    }
    const key = line.slice(keyStart, cursor)
    cursor += 1

    let value = ''
    if (line[cursor] === '"') {
      cursor += 1
      let escaped = false
      while (cursor < line.length) {
        const char = line[cursor]
        cursor += 1
        if (escaped) {
          value += char
          escaped = false
          continue
        }
        if (char === '\\') {
          escaped = true
          continue
        }
        if (char === '"') {
          break
        }
        value += char
      }
    } else {
      const valueStart = cursor
      while (cursor < line.length && line[cursor] !== ' ') {
        cursor += 1
      }
      value = line.slice(valueStart, cursor)
    }
    fields[key] = value
  }
  return fields.time && fields.level && fields.msg ? fields : null
}

function parseLogLine(line: string, index: number): ParsedLogLine | null {
  const fields = parseSlogFields(line)
  if (!fields) {
    return null
  }
  const time = fields.time
  const level = fields.level
  const messageText = fields.msg
  if (!time || !level || !messageText) {
    return null
  }
  const component = fields.component ?? '-'
  const extraFields = Object.entries(fields)
    .filter(([key]) => !['time', 'level', 'component', 'msg'].includes(key))
    .map(([key, value]) => `${key}=${value}`)
  const logMessage = [messageText, ...extraFields].filter(Boolean).join(' ')
  return {
    component,
    key: `${index}-${line}`,
    level,
    message: logMessage,
    raw: line,
    time,
    tone: logTone(level),
  }
}

function isNearLogTop(element: HTMLElement) {
  return element.scrollTop <= 48
}

function handleLogScroll(event: Event) {
  const target = event.currentTarget
  if (target instanceof HTMLElement) {
    shouldFollowLatestLog.value = isNearLogTop(target)
  }
}

function scrollLogToTop() {
  void nextTick(() => {
    const logBody = logBodyRef.value
    if (logBody && shouldFollowLatestLog.value) {
      logBody.scrollTop = 0
    }
  })
}

async function copyLogText() {
  if (!logText.value) {
    message.info(t('暂无日志可复制', 'No logs to copy'))
    return
  }
  try {
    await copyToClipboard(logText.value)
    message.success(t('维护日志已复制', 'Maintenance logs copied'))
  } catch (error) {
    message.error(errorText(error, '复制失败', 'Copy failed'))
  }
}

const priorityColumns = computed<DataTableColumns<CodexKeeperPriorityRule>>(() => [
  {
    title: t('账号类型', 'Account Type'),
    key: 'account_type',
    minWidth: 132,
    render: (row) =>
      h(NInput, {
        size: 'small',
        value: row.account_type,
        placeholder: t('例如 pro_20x', 'For example, pro_20x'),
        onUpdateValue: (value: string) => {
          row.account_type = value
        },
      }),
  },
  {
    title: t('优先级', 'Priority'),
    key: 'priority',
    width: 112,
    render: (row) =>
      h(NInputNumber, {
        size: 'small',
        value: row.priority,
        min: 0,
        max: 20,
        onUpdateValue: (value: number | null) => {
          row.priority = value ?? 0
        },
      }),
  },
  {
    title: '',
    key: 'actions',
    width: 58,
    render: (row) =>
      h(
        NButton,
        { size: 'tiny', quaternary: true, type: 'error', onClick: () => removeRule(row) },
        { default: () => t('移除', 'Remove') },
      ),
  },
])

onMounted(() => {
  void loadAll()
  statusTimer = window.setInterval(() => {
    void loadStatus()
  }, 3000)
})

watch(() => form.schedule_cron, queueSchedulePreview)

onBeforeUnmount(() => {
  if (statusTimer !== undefined) {
    window.clearInterval(statusTimer)
  }
  if (schedulePreviewTimer !== undefined) {
    window.clearTimeout(schedulePreviewTimer)
  }
  stopOAuthStatusPolling()
})
</script>

<template>
  <section class="page inspection-page">
    <div class="page-header">
      <div>
        <h1 class="page-title">{{ t('巡检设置', 'Inspection Settings') }}</h1>
        <p class="page-subtitle">{{ t('维护 Codex auth file 的健康状态和调度优先级', 'Maintain Codex auth file health and scheduling priorities') }}</p>
      </div>
      <NSpace>
        <NButton secondary :loading="isLoading" @click="loadAll">{{ t('重新加载', 'Reload') }}</NButton>
        <NButton type="primary" :loading="isSaving" :disabled="isRunning" @click="saveSettings">
          {{ t('保存配置', 'Save Settings') }}
        </NButton>
      </NSpace>
    </div>

    <div class="metric-grid">
      <div class="metric-card inspection-status-card">
        <div class="metric-icon" aria-hidden="true">
          <Activity :size="20" :stroke-width="2.2" />
        </div>
        <div class="metric-label">{{ t('运行状态', 'Run Status') }}</div>
        <div class="metric-value inspection-status-value" :title="statusDetailText">
          <NTag class="inspection-status-tag" :type="stateType" size="small" :bordered="false">
            {{ statusDetailText }}
          </NTag>
        </div>
        <div class="metric-footnote">{{ statusFootnoteText }}</div>
      </div>
      <div class="metric-card">
        <div class="metric-icon" aria-hidden="true">
          <Users :size="20" :stroke-width="2.2" />
        </div>
        <div class="metric-label">{{ t('账号总数', 'Total Accounts') }}</div>
        <div class="metric-value">{{ formatInteger(accountTotalCount) }}</div>
        <div class="metric-footnote">{{ t('全部 auth file', 'All auth files') }}</div>
      </div>
      <div class="metric-card is-green">
        <div class="metric-icon" aria-hidden="true">
          <ShieldCheck :size="20" :stroke-width="2.2" />
        </div>
        <div class="metric-label">{{ t('启用中', 'Enabled') }}</div>
        <div class="metric-value">{{ formatInteger(enabledAccountCount) }}</div>
        <div class="metric-footnote">{{ t('可参与调度', 'Available for scheduling') }}</div>
      </div>
      <div class="metric-card is-warning">
        <div class="metric-icon" aria-hidden="true">
          <PauseCircle :size="20" :stroke-width="2.2" />
        </div>
        <div class="metric-label">{{ t('已禁用', 'Disabled') }}</div>
        <div class="metric-value">{{ formatInteger(disabledAccountCount) }}</div>
        <div class="metric-footnote">{{ t('停用账号', 'Inactive accounts') }}</div>
      </div>
      <div class="metric-card is-danger">
        <div class="metric-icon" aria-hidden="true">
          <ShieldAlert :size="20" :stroke-width="2.2" />
        </div>
        <div class="metric-label">{{ t('401报错', '401 Errors') }}</div>
        <div class="metric-value">{{ formatInteger(unauthorizedErrorAccountCount) }}</div>
        <div class="metric-footnote">HTTP 401</div>
      </div>
      <div class="metric-card is-purple">
        <div class="metric-icon" aria-hidden="true">
          <Gauge :size="20" :stroke-width="2.2" />
        </div>
        <div class="metric-label">{{ t('额度耗尽', 'Quota Exhausted') }}</div>
        <div class="metric-value">{{ formatInteger(quotaExhaustedAccountCount) }}</div>
        <div class="metric-footnote">{{ t('临时降级', 'Temporary downgrade') }}</div>
      </div>
    </div>

    <section class="panel oauth-panel">
      <div class="panel-inner oauth-panel-inner">
        <div class="section-heading">
          <div>
            <h2 class="section-title">{{ t('OAuth 登录', 'OAuth Sign-in') }}</h2>
            <p class="section-hint">{{ t('通过 CLIProxyAPI 创建 provider 授权链接，完成后会写入新的 auth file。', 'Create a provider authorization link through CLIProxyAPI. The completed flow writes a new auth file.') }}</p>
          </div>
          <NSpace class="oauth-actions" size="small">
            <NButton secondary :disabled="!oauthFlow?.url" @click="openOAuthURL">
              <template #icon>
                <NIcon :component="ExternalLink" />
              </template>
              {{ t('打开授权页', 'Open Auth Page') }}
            </NButton>
            <NButton secondary :loading="isCheckingOAuth" :disabled="!oauthFlow?.state" @click="checkOAuthStatus()">
              {{ t('检查状态', 'Check Status') }}
            </NButton>
          </NSpace>
        </div>

        <div class="oauth-grid">
          <NFormItem :label="t('Provider', 'Provider')">
            <NSelect v-model:value="oauthProvider" :options="oauthProviderOptions" />
          </NFormItem>
          <NFormItem v-if="oauthProviderNeedsProjectID" :label="t('GCP Project ID', 'GCP Project ID')">
            <NInput v-model:value="oauthProjectID" placeholder="my-gcp-project" />
          </NFormItem>
          <NFormItem class="oauth-submit-item">
            <NButton type="primary" block :loading="isStartingOAuth" @click="startOAuthLogin">
              <template #icon>
                <NIcon :component="LogIn" />
              </template>
              {{ t('创建并打开 OAuth 登录', 'Create and Open OAuth Sign-in') }}
            </NButton>
          </NFormItem>
        </div>

        <NAlert v-if="oauthFlow" class="oauth-alert" type="info" :show-icon="false">
          <div class="oauth-status-line">
            <strong>{{ oauthFlow.label }}</strong>
            <span v-if="oauthFlow.state">state: {{ oauthFlow.state }}</span>
            <span v-if="oauthStatusText">{{ oauthStatusText }}</span>
          </div>
        </NAlert>

        <div class="oauth-callback-row">
          <NInput
            v-model:value="oauthCallbackURL"
            clearable
            :placeholder="t('如果浏览器没有自动回调，可粘贴 callback URL', 'Paste the callback URL if the browser did not complete the redirect automatically')"
          />
          <NButton secondary :loading="isSubmittingOAuthCallback" @click="submitOAuthCallback">
            {{ t('提交 callback', 'Submit Callback') }}
          </NButton>
        </div>
      </div>
    </section>

    <div class="grid-two inspection-settings-grid">
      <section class="panel inspection-config-panel">
        <div class="panel-inner config-panel-inner">
          <div class="section-heading">
            <h2 class="section-title">{{ t('巡检配置', 'Inspection Configuration') }}</h2>
            <NSpace class="config-actions" size="small">
              <NButton
                size="small"
                secondary
                :loading="isActing"
                :disabled="isRunOnceBlocked"
                @click="runAction(runCodexKeeperOnce, t('已开始执行一轮', 'Started one inspection run'))"
              >
                {{ t('执行一轮', 'Run Once') }}
              </NButton>
              <NButton
                size="small"
                type="primary"
                :loading="isActing"
                :disabled="isDaemonRunning"
                @click="runAction(startCodexKeeper, t('已开始自动巡检', 'Automatic inspection started'))"
              >
                {{ t('开始自动巡检', 'Start Automatic Inspection') }}
              </NButton>
              <NButton
                size="small"
                secondary
                type="warning"
                :loading="isActing"
                :disabled="!isDaemonRunning"
                @click="runAction(stopCodexKeeper, t('已请求停止', 'Stop requested'))"
              >
                {{ t('停止', 'Stop') }}
              </NButton>
            </NSpace>
          </div>
          <NForm class="config-form" :model="form" label-placement="top" size="small">
            <div class="config-sections">
              <section class="config-block">
                <h3 class="config-block-title">{{ t('调度', 'Schedule') }}</h3>
                <div class="schedule-grid">
                  <NFormItem :label="t('Cron 表达式', 'Cron Expression')">
                    <NInput v-model:value="form.schedule_cron" :placeholder="t('例如 */30 * * * *', 'For example, */30 * * * *')" />
                  </NFormItem>
                  <div class="schedule-preview">
                    <div class="preview-title">{{ t('后续 5 次调用', 'Next 5 Runs') }}</div>
                    <div v-if="schedulePreviewError" class="preview-error">
                      {{ schedulePreviewError }}
                    </div>
                    <div v-else-if="nextRunTimes.length" class="preview-grid">
                      <span v-for="time in nextRunTimes" :key="time" class="preview-time">
                        {{ formatDateTime(time) }}
                      </span>
                    </div>
                    <div v-else class="preview-muted">{{ t('填写 Cron 表达式后显示', 'Enter a Cron expression to preview') }}</div>
                  </div>
                </div>
                <div class="conditional-refresh-grid">
                  <NFormItem :label="t('按条件扫描间隔', 'Conditional Scan Interval')">
                    <NSelect
                      v-model:value="form.conditional_refresh_interval_seconds"
                      :options="conditionalRefreshIntervalOptions"
                    />
                  </NFormItem>
                  <NFormItem :label="t('账号刷新缓存（分钟）', 'Account Refresh Cache (minutes)')">
                    <NInputNumber
                      v-model:value="form.account_refresh_cache_minutes"
                      :min="1"
                      :precision="0"
                    />
                  </NFormItem>
                </div>
                <div class="conditional-refresh-help">
                  <p>
                    <strong>{{ t('按条件扫描间隔：', 'Conditional scan interval:') }}</strong>{{ t('后台自动巡检开启后，每隔多久检查一次是否有账号需要刷新；会查找缓存时间内有实际请求的账号、额度刷新时间已到的账号、检测异常账号，并同步本地记录与 CPA 当前账号列表的差异。', 'How often automatic inspection checks whether accounts need refreshing after it is enabled. It looks for accounts with actual requests during the cache window, expired quota refresh times, inspection errors, and differences between local records and the current CPA account list.') }}
                  </p>
                  <p>
                    <strong>{{ t('账号刷新缓存：', 'Account refresh cache:') }}</strong>{{ t('控制自动任务的防重复时间；同一账号在缓存时间内不会被自动巡检或按条件扫描重复刷新，手动刷新会绕过缓存但会更新缓存时间。', 'Controls duplicate prevention for automatic tasks. The same account will not be refreshed repeatedly by automatic inspection or conditional scans during the cache window. Manual refresh bypasses the cache but updates the cache time.') }}
                  </p>
                </div>
              </section>

              <section class="config-block">
                <h3 class="config-block-title">{{ t('执行参数', 'Execution Parameters') }}</h3>
                <div class="params-grid">
                  <NFormItem :label="t('额度阈值（%）', 'Quota Threshold (%)')">
                    <NInputNumber v-model:value="form.quota_threshold" :min="0" :max="100" />
                  </NFormItem>
                  <NFormItem :label="t('额度检测超时（秒）', 'Quota Check Timeout (seconds)')">
                    <NInputNumber v-model:value="form.usage_timeout_seconds" :min="1" />
                  </NFormItem>
                  <NFormItem :label="t('账号管理接口超时（秒）', 'Account API Timeout (seconds)')">
                    <NInputNumber v-model:value="form.cpa_timeout_seconds" :min="1" />
                  </NFormItem>
                  <NFormItem :label="t('失败重试次数', 'Failure Retries')">
                    <NInputNumber v-model:value="form.max_retries" :min="0" :max="5" />
                  </NFormItem>
                  <NFormItem :label="t('账号处理并发数', 'Account Processing Concurrency')">
                    <NInputNumber v-model:value="form.worker_threads" :min="1" :max="64" />
                  </NFormItem>
                </div>
                <div class="switch-row">
                  <NFormItem class="switch-form-item">
                    <div class="switch-setting">
                      <div class="switch-copy">
                        <span class="switch-title">{{ t('只检查不修改', 'Check Only') }}</span>
                        <p class="switch-help">{{ t('开启后只模拟处理，不会禁用账号或调整优先级。', 'When enabled, processing is simulated and accounts are not disabled or reprioritized.') }}</p>
                      </div>
                      <NSwitch v-model:value="form.dry_run" class="switch-control" />
                    </div>
                  </NFormItem>
                  <NFormItem class="switch-form-item">
                    <div class="switch-setting">
                      <div class="switch-copy">
                        <span class="switch-title">{{ t('启用凭证 WebSocket', 'Enable Credential WebSocket') }}</span>
                        <p class="switch-help">
                          {{ t('刷新时为每个 Codex 凭证写入 websockets=true，用于 Responses API 的 WebSocket 传输。', 'During refresh, write websockets=true to each Codex credential for Responses API WebSocket transport.') }}
                        </p>
                      </div>
                      <NSwitch
                        v-model:value="form.enable_credential_websockets"
                        class="switch-control"
                      />
                    </div>
                  </NFormItem>
                  <NFormItem class="switch-form-item">
                    <div class="switch-setting">
                      <div class="switch-copy">
                        <span class="switch-title">{{ t('启动后自动巡检', 'Auto Inspect on Startup') }}</span>
                        <p class="switch-help">{{ t('每次 CPA-Helper 启动后，自动按上面的计划检查账号。', 'Automatically inspect accounts using the schedule above whenever CPA-Helper starts.') }}</p>
                      </div>
                      <NSwitch v-model:value="form.auto_start_daemon" class="switch-control" />
                    </div>
                  </NFormItem>
                </div>
              </section>
            </div>
          </NForm>
          <section class="config-block runtime-block">
            <h3 class="config-block-title">{{ t('运行信息', 'Runtime Information') }}</h3>
            <div class="runtime-info-grid">
              <div class="runtime-stat">
                <span class="runtime-label">CLIProxyAPI</span>
                <strong class="runtime-value">
                  {{ status ? t('使用系统设置中的地址和管理密钥', 'Using the system settings URL and admin key') : t('等待加载', 'Waiting to load') }}
                </strong>
              </div>
              <div class="runtime-stat">
                <span class="runtime-label">{{ t('最近开始', 'Last Started') }}</span>
                <strong class="runtime-value">
                  {{ formatDateTime(status?.last_started_at ?? null) }}
                </strong>
              </div>
              <div class="runtime-stat">
                <span class="runtime-label">{{ t('最近完成', 'Last Finished') }}</span>
                <strong class="runtime-value">
                  {{ formatDateTime(status?.last_finished_at ?? null) }}
                </strong>
              </div>
            </div>
          </section>
        </div>
      </section>

      <section class="panel priority-rules-panel">
        <div class="panel-inner">
          <div class="section-heading">
            <h2 class="section-title">{{ t('账号类型优先级', 'Account Type Priorities') }}</h2>
            <NButton size="small" secondary @click="addRule">{{ t('新增规则', 'Add Rule') }}</NButton>
          </div>
          <p class="section-hint">
            {{ t('账号当前优先级超过 20 时视为手动优先，巡检不会覆盖；0 ~ 20 会按这里的账号类型规则维护。', 'Current account priorities above 20 are treated as manual priority and will not be overwritten. Priorities from 0 to 20 are maintained using the account type rules here.') }}
          </p>
          <NDataTable
            class="priority-table"
            size="small"
            :columns="priorityColumns"
            :data="displayedPriorityRules"
            :pagination="false"
            :scroll-x="320"
          />
        </div>
      </section>
    </div>

    <section class="panel log-panel">
      <div class="panel-inner log-panel-inner">
        <div class="section-heading">
          <h2 class="section-title">{{ t('维护日志', 'Maintenance Logs') }}</h2>
          <NSpace class="log-actions" size="small">
            <NButton secondary :disabled="!logText" @click="copyLogText">
              <template #icon>
                <NIcon :component="Copy" />
              </template>
              {{ t('复制日志', 'Copy Logs') }}
            </NButton>
            <NButton secondary :loading="isActing" @click="runAction(clearCodexKeeperLogs, t('日志已清空', 'Logs cleared'))">
              <template #icon>
                <NIcon :component="Trash2" />
              </template>
              {{ t('清空日志', 'Clear Logs') }}
            </NButton>
          </NSpace>
        </div>
        <div
          ref="logBodyRef"
          class="log-view"
          role="log"
          :aria-label="t('维护日志', 'Maintenance Logs')"
          @scroll="handleLogScroll"
        >
          <div v-if="displayedLogLines.length === 0" class="log-empty">{{ t('暂无日志', 'No logs') }}</div>
          <div v-else class="log-lines">
            <div
              v-for="line in displayedLogLines"
              :key="line.key"
              class="log-line"
              :class="`is-${line.tone}`"
              :title="serverText(line.message, '维护日志', 'Maintenance log')"
            >
              <time class="log-time">{{ line.time }}</time>
              <span class="log-level">{{ line.level }}</span>
              <span class="log-component">{{ line.component }}</span>
              <span class="log-message">{{ serverText(line.message, '维护日志', 'Maintenance log') }}</span>
            </div>
          </div>
        </div>
      </div>
    </section>
  </section>
</template>

<style scoped>
.section-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 8px;
}

.section-title {
  margin: 0;
  color: var(--cpa-text);
  font-size: 15px;
}

.section-hint {
  margin: -2px 0 10px;
  color: var(--cpa-text-muted);
  font-size: 12px;
  line-height: 1.5;
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

.inspection-config-panel .panel-inner {
  padding: 12px 14px 14px;
}

.inspection-settings-grid {
  align-items: start;
}

.oauth-panel-inner {
  display: grid;
  gap: 10px;
}

.oauth-panel .section-heading {
  margin-bottom: 0;
}

.oauth-actions {
  flex-shrink: 0;
}

.oauth-grid {
  display: grid;
  grid-template-columns: minmax(180px, 240px) minmax(220px, 1fr) minmax(220px, 260px);
  gap: 10px;
  align-items: end;
}

.oauth-grid :deep(.n-form-item) {
  margin: 0;
}

.oauth-grid :deep(.n-form-item-feedback-wrapper) {
  min-height: 0;
}

.oauth-submit-item {
  align-self: end;
}

.oauth-alert {
  border-radius: var(--cpa-radius-sm);
}

.oauth-status-line {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 12px;
  align-items: center;
  color: var(--cpa-text);
  font-size: 12px;
  line-height: 1.5;
}

.oauth-status-line span {
  min-width: 0;
  overflow-wrap: anywhere;
}

.oauth-callback-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
  align-items: center;
}

.config-panel-inner {
  display: grid;
  gap: 10px;
}

.config-actions {
  flex-shrink: 0;
}

.config-form {
  min-width: 0;
}

.config-form :deep(.n-form-item) {
  margin: 0;
}

.config-form :deep(.n-form-item-label) {
  padding: 0 0 4px;
  color: var(--cpa-text);
  font-size: 12px;
}

.config-form :deep(.n-form-item-feedback-wrapper) {
  min-height: 0;
}

.config-form :deep(.n-form-item-blank) {
  min-height: 30px;
}

.switch-form-item :deep(.n-form-item-blank) {
  min-height: 0;
}

.config-form :deep(.n-input-number),
.config-form :deep(.n-input) {
  width: 100%;
}

.config-sections {
  display: grid;
  gap: 10px;
}

.config-block {
  min-width: 0;
  padding: 10px 12px;
  border: 1px solid var(--cpa-border);
  border-radius: 6px;
  background: var(--cpa-surface-muted);
}

.config-block-title {
  margin: 0 0 8px;
  color: var(--cpa-text);
  font-size: 12px;
  font-weight: 700;
  line-height: 1.2;
}

.schedule-grid {
  display: grid;
  grid-template-columns: minmax(220px, 0.82fr) minmax(0, 1fr);
  gap: 10px;
  align-items: end;
}

.conditional-refresh-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(180px, 1fr));
  gap: 8px 10px;
  margin-top: 10px;
}

.conditional-refresh-help {
  display: grid;
  gap: 2px;
  margin: 2px 0 0;
  color: var(--cpa-text-muted);
  font-size: 12px;
  line-height: 1.45;
}

.conditional-refresh-help p {
  margin: 0;
}

.params-grid {
  display: grid;
  grid-template-columns: repeat(5, minmax(92px, 1fr));
  gap: 8px 10px;
}

.switch-row {
  display: grid;
  grid-template-columns: repeat(3, minmax(220px, 1fr));
  gap: 10px;
  margin-top: 10px;
}

.switch-form-item {
  min-width: 0;
}

.switch-setting {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  width: 100%;
  min-height: 72px;
  padding: 10px 12px;
  border: 1px solid var(--cpa-border);
  border-radius: 6px;
  background: var(--cpa-surface);
}

.switch-copy {
  display: grid;
  gap: 4px;
  min-width: 0;
}

.switch-title {
  color: var(--cpa-text);
  font-size: 13px;
  font-weight: 650;
  line-height: 1.25;
}

.switch-control {
  flex: 0 0 auto;
}

.switch-help {
  margin: 0;
  color: var(--cpa-text-muted);
  font-size: 12px;
  line-height: 1.35;
  text-wrap: pretty;
}

.schedule-preview {
  min-width: 0;
  min-height: 54px;
  padding: 7px 10px;
  border: 1px solid var(--cpa-border);
  border-radius: 6px;
  background: var(--cpa-surface);
}

.preview-title {
  margin-bottom: 5px;
  color: var(--cpa-text);
  font-size: 12px;
  font-weight: 600;
}

.preview-grid {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 4px;
}

.preview-time {
  min-width: 0;
  padding: 3px 5px;
  border-radius: 4px;
  background: var(--cpa-surface-muted);
  color: var(--cpa-text);
  font-size: 12px;
  text-align: center;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.preview-muted,
.preview-error {
  color: var(--cpa-text-muted);
  font-size: 12px;
}

.preview-error {
  color: var(--cpa-danger);
}

.runtime-info-grid {
  display: grid;
  grid-template-columns: minmax(180px, 1.25fr) repeat(2, minmax(120px, 1fr));
  gap: 8px;
}

.runtime-stat {
  min-width: 0;
  padding: 8px 10px;
  border: 1px solid var(--cpa-border);
  border-radius: 6px;
  background: var(--cpa-surface);
}

.runtime-label {
  display: block;
  margin-bottom: 3px;
  color: var(--cpa-text-muted);
  font-size: 12px;
  line-height: 1.2;
}

.runtime-value {
  display: block;
  min-width: 0;
  overflow: hidden;
  color: var(--cpa-text);
  font-size: 12px;
  font-weight: 600;
  line-height: 1.35;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.priority-table {
  min-width: 0;
}

.priority-rules-panel {
  align-self: start;
}

.priority-rules-panel .panel-inner {
  padding: 16px 20px 18px;
}

.priority-rules-panel .section-heading {
  margin-bottom: 6px;
}

.priority-rules-panel .section-hint {
  margin-bottom: 12px;
}

.priority-table :deep(.n-data-table-wrapper),
.priority-table :deep(.n-data-table-base-table),
.priority-table :deep(.n-data-table-base-table-body) {
  min-width: 0;
}

.priority-table :deep(.n-data-table-th),
.priority-table :deep(.n-data-table-td) {
  padding: 6px 10px;
}

.priority-table :deep(.n-data-table-th) {
  font-size: 13px;
}

.priority-table :deep(.n-input),
.priority-table :deep(.n-input-number) {
  width: 100%;
}

.log-panel,
.log-panel-inner,
.log-view {
  min-height: 0;
}

.log-panel-inner {
  display: grid;
  gap: 10px;
}

.log-panel-inner .section-heading {
  margin-bottom: 0;
}

.log-actions {
  flex-shrink: 0;
}

.log-view {
  height: 520px;
  overflow: auto;
  border: 1px solid var(--cpa-border);
  border-radius: var(--cpa-radius-sm);
  background:
    linear-gradient(180deg, rgb(255 255 255 / 54%), rgb(255 255 255 / 18%)),
    var(--cpa-surface-muted);
  box-shadow: inset 0 1px 0 rgb(255 255 255 / 78%);
  scrollbar-gutter: stable;
  scrollbar-width: thin;
  scrollbar-color: color-mix(in srgb, var(--cpa-text-muted) 44%, transparent) transparent;
}

.log-view::-webkit-scrollbar {
  width: 14px;
  height: 14px;
}

.log-view::-webkit-scrollbar-track,
.log-view::-webkit-scrollbar-corner {
  background: transparent;
}

.log-view::-webkit-scrollbar-thumb {
  min-height: 48px;
  border: 5px solid transparent;
  border-radius: 999px;
  background: color-mix(in srgb, var(--cpa-text-muted) 44%, transparent);
  background-clip: content-box;
}

.log-view::-webkit-scrollbar-thumb:hover {
  background: color-mix(in srgb, var(--cpa-primary) 58%, var(--cpa-text-muted));
  background-clip: content-box;
}

:root.dark .log-view {
  background:
    linear-gradient(180deg, rgb(255 255 255 / 5%), rgb(255 255 255 / 1%)),
    var(--cpa-surface-muted);
  box-shadow: inset 0 1px 0 rgb(255 255 255 / 8%);
}

.log-lines {
  display: grid;
  min-width: 860px;
  padding: 8px;
}

.log-line {
  display: grid;
  grid-template-columns: 216px 68px minmax(112px, 148px) minmax(0, 1fr);
  gap: 10px;
  align-items: start;
  min-width: 0;
  padding: 7px 9px;
  border-bottom: 1px solid color-mix(in srgb, var(--cpa-border) 74%, transparent);
  color: var(--cpa-text);
  font-family: "Cascadia Mono", "SFMono-Regular", Consolas, monospace;
  font-size: 12px;
  line-height: 1.45;
}

.log-line:last-child {
  border-bottom: 0;
}

.log-line:hover {
  border-radius: var(--cpa-radius-sm);
  background: var(--cpa-primary-wash);
}

:root.dark .log-line:hover {
  background: color-mix(in srgb, var(--cpa-primary-wash) 70%, transparent);
}

.log-time,
.log-component,
.log-message {
  min-width: 0;
}

.log-time,
.log-component {
  overflow: hidden;
  color: var(--cpa-text-muted);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.log-level {
  display: inline-flex;
  width: fit-content;
  min-width: 48px;
  justify-content: center;
  padding: 1px 6px;
  border: 1px solid var(--cpa-border);
  border-radius: var(--cpa-radius-sm);
  background: var(--cpa-surface);
  color: var(--cpa-text-muted);
  font-size: 11px;
  font-weight: 760;
  line-height: 1.35;
}

.log-message {
  color: var(--cpa-text);
  overflow-wrap: anywhere;
}

.log-line.is-info .log-level {
  border-color: color-mix(in srgb, var(--cpa-primary) 24%, transparent);
  background: var(--cpa-primary-wash);
  color: var(--cpa-primary);
}

.log-line.is-warning .log-level {
  border-color: color-mix(in srgb, var(--cpa-warning) 28%, transparent);
  background: var(--cpa-warning-weak);
  color: var(--cpa-warning);
}

.log-line.is-danger .log-level {
  border-color: color-mix(in srgb, var(--cpa-danger) 28%, transparent);
  background: var(--cpa-danger-weak);
  color: var(--cpa-danger);
}

.log-line.is-debug .log-level {
  border-color: color-mix(in srgb, var(--cpa-accent-blue) 24%, transparent);
  background: var(--cpa-accent-blue-weak);
  color: var(--cpa-accent-blue);
}

.log-empty {
  display: grid;
  height: 100%;
  min-height: 180px;
  place-items: center;
  color: var(--cpa-text-muted);
  font-size: 13px;
}

@media (min-width: 861px) {
  .inspection-page {
    grid-template-rows: auto auto auto auto;
    min-height: 0;
  }
}

@media (max-width: 760px) {
  .section-heading {
    align-items: flex-start;
    flex-direction: column;
  }

  .schedule-grid,
  .conditional-refresh-grid,
  .oauth-grid,
  .oauth-callback-row,
  .runtime-info-grid {
    grid-template-columns: 1fr;
  }

  .params-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .preview-grid {
    grid-template-columns: 1fr;
  }

  .switch-row {
    grid-template-columns: 1fr;
  }

  .log-view {
    height: 420px;
  }

  .log-lines {
    min-width: 0;
  }

  .log-line {
    grid-template-columns: 142px 58px minmax(0, 1fr);
  }

  .log-message {
    grid-column: 1 / -1;
  }
}

@media (max-width: 520px) {
  .params-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
