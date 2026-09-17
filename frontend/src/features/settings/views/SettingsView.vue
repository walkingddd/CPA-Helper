<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import {
  NAlert,
  NButton,
  NDescriptions,
  NDescriptionsItem,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NSpace,
  NSwitch,
  NTag,
  useMessage,
} from 'naive-ui'
import { Activity, Database, Power, Server } from 'lucide-vue-next'

import {
  getCollectorStatus,
  getSettings,
  updateSettings,
} from '@/features/settings/api/settingsApi'
import { useI18n } from '@/shared/i18n'
import type { CollectorStatus, SettingsUpdatePayload } from '@/shared/types/api'
import { formatDateTime, formatInteger } from '@/shared/utils/format'

const message = useMessage()
const { errorText, serverText, t } = useI18n()
const isLoading = ref(false)
const isSaving = ref(false)
const collectorStatus = ref<CollectorStatus | null>(null)

const settingsForm = reactive({
  cliaproxy_url: 'http://127.0.0.1:8317',
  model_request_url: 'http://127.0.0.1:8317',
  management_key: '',
  collector_enabled: false,
  batch_size: 100,
  poll_interval_seconds: 2,
  retry_interval_seconds: 10,
  api_key_prefix: 'sk',
})

const apiKeyPrefixPreview = computed(() => `${(settingsForm.api_key_prefix || 'sk').trim() || 'sk'}-xxxxxxxx…`)

const remoteStatusType = computed(() => {
  if (collectorStatus.value?.remote_enabled === true) {
    return 'success'
  }
  if (collectorStatus.value?.remote_enabled === false) {
    return 'error'
  }
  return 'warning'
})

const remoteStatusText = computed(() => {
  if (collectorStatus.value?.remote_enabled === true) {
    return t('开启', 'On')
  }
  if (collectorStatus.value?.remote_enabled === false) {
    return t('关闭', 'Off')
  }
  return t('未知', 'Unknown')
})

const collectorEnabledText = computed(() => (collectorStatus.value?.enabled ? t('开启', 'On') : t('关闭', 'Off')))
const collectorRunningText = computed(() => (collectorStatus.value?.running ? t('运行中', 'Running') : t('空闲', 'Idle')))

async function refresh() {
  isLoading.value = true
  try {
    const [settings, status] = await Promise.all([
      getSettings(),
      getCollectorStatus(),
    ])
    settingsForm.cliaproxy_url = settings.cliaproxy_url
    settingsForm.model_request_url = settings.model_request_url
    settingsForm.management_key = settings.management_key
    settingsForm.collector_enabled = settings.collector_enabled
    settingsForm.batch_size = settings.batch_size
    settingsForm.poll_interval_seconds = settings.poll_interval_seconds
    settingsForm.retry_interval_seconds = settings.retry_interval_seconds
    settingsForm.api_key_prefix = settings.api_key_prefix || 'sk'
    collectorStatus.value = status
  } catch (error) {
    message.error(errorText(error, '加载设置失败', 'Failed to load settings'))
  } finally {
    isLoading.value = false
  }
}

async function saveSettings() {
  isSaving.value = true
  try {
    const payload: SettingsUpdatePayload = {
      cliaproxy_url: settingsForm.cliaproxy_url,
      model_request_url: settingsForm.model_request_url,
      management_key: settingsForm.management_key,
      collector_enabled: settingsForm.collector_enabled,
      batch_size: settingsForm.batch_size,
      poll_interval_seconds: settingsForm.poll_interval_seconds,
      retry_interval_seconds: settingsForm.retry_interval_seconds,
      api_key_prefix: settingsForm.api_key_prefix,
    }
    const saved = await updateSettings(payload)
    settingsForm.management_key = saved.management_key
    settingsForm.api_key_prefix = saved.api_key_prefix || 'sk'
    message.success(t('设置已保存', 'Settings saved'))
    await refresh()
  } catch (error) {
    message.error(errorText(error, '保存设置失败', 'Failed to save settings'))
  } finally {
    isSaving.value = false
  }
}

onMounted(refresh)
</script>

<template>
  <section class="page">
    <div class="page-header">
      <div>
        <h1 class="page-title">{{ t('系统设置', 'System Settings') }}</h1>
        <p class="page-subtitle">{{ t('集中管理采集配置', 'Manage collection settings in one place') }}</p>
      </div>
      <NSpace>
        <NButton secondary :loading="isLoading" @click="refresh">{{ t('刷新', 'Refresh') }}</NButton>
        <NButton type="primary" :loading="isSaving" @click="saveSettings">{{ t('保存设置', 'Save settings') }}</NButton>
      </NSpace>
    </div>

    <div class="metric-grid settings-metrics">
      <div class="metric-card" :class="collectorStatus?.enabled ? 'is-green' : 'is-orange'">
        <div class="metric-icon" aria-hidden="true">
          <Power :size="20" :stroke-width="2.2" />
        </div>
        <div class="metric-label">{{ t('本地采集', 'Local collection') }}</div>
        <div class="metric-value">{{ collectorEnabledText }}</div>
        <div class="metric-footnote">{{ t('系统开关', 'System switch') }}</div>
      </div>
      <div class="metric-card" :class="collectorStatus?.running ? 'is-teal' : 'is-blue'">
        <div class="metric-icon" aria-hidden="true">
          <Activity :size="20" :stroke-width="2.2" />
        </div>
        <div class="metric-label">{{ t('运行状态', 'Run status') }}</div>
        <div class="metric-value">{{ collectorRunningText }}</div>
        <div class="metric-footnote">{{ t('采集进程', 'Collector process') }}</div>
      </div>
      <div class="metric-card" :class="remoteStatusType === 'success' ? 'is-green' : 'is-purple'">
        <div class="metric-icon" aria-hidden="true">
          <Server :size="20" :stroke-width="2.2" />
        </div>
        <div class="metric-label">{{ t('远端开关', 'Remote switch') }}</div>
        <div class="metric-value">{{ remoteStatusText }}</div>
        <div class="metric-footnote">CLIProxyAPI</div>
      </div>
      <div class="metric-card is-blue">
        <div class="metric-icon" aria-hidden="true">
          <Database :size="20" :stroke-width="2.2" />
        </div>
        <div class="metric-label">{{ t('累计写入', 'Records written') }}</div>
        <div class="metric-value">{{ formatInteger(collectorStatus?.records_collected ?? 0) }}</div>
        <div class="metric-footnote">{{ t('本地记录', 'Local records') }}</div>
      </div>
    </div>

    <div class="grid-two">
      <section class="panel">
        <div class="panel-inner">
          <h2 class="section-title">{{ t('采集配置', 'Collection Settings') }}</h2>
          <NForm :model="settingsForm" label-placement="top">
            <div class="form-grid">
              <div class="field-stack">
                <div class="field-label">{{ t('CLIProxyAPI 地址', 'CLIProxyAPI URL') }}</div>
                <NInput v-model:value="settingsForm.cliaproxy_url" />
                <div class="form-help">{{ t('用于采集队列、API Key 同步和管理接口。', 'Used for collection queues, API key sync, and management APIs.') }}</div>
              </div>
              <div class="field-stack">
                <div class="field-label">{{ t('模型请求地址（例如：填写CPA外网地址）', 'Model request URL (for example, CPA public URL)') }}</div>
                <NInput
                  v-model:value="settingsForm.model_request_url"
                  :placeholder="t('例如：http://192.168.26.50:8317', 'Example: http://192.168.26.50:8317')"
                />
                <div class="form-help">{{ t('仅用于 API 密钥页「请求测试」生成 URL 和示例。', 'Only used to generate URLs and examples for request tests on the API keys page.') }}</div>
              </div>
              <NFormItem :label="t('管理密钥', 'Management key')">
                <NInput
                  v-model:value="settingsForm.management_key"
                  type="password"
                  show-password-on="mousedown"
                  :placeholder="t('请输入 CLIProxyAPI 管理密钥', 'Enter the CLIProxyAPI management key')"
                />
              </NFormItem>
              <NFormItem :label="t('开启本地采集', 'Enable local collection')">
                <NSwitch v-model:value="settingsForm.collector_enabled" />
              </NFormItem>
              <NFormItem :label="t('批量读取数', 'Batch size')">
                <NInputNumber v-model:value="settingsForm.batch_size" :min="1" :max="1000" />
              </NFormItem>
              <NFormItem :label="t('轮询间隔（秒）', 'Poll interval (seconds)')">
                <NInputNumber v-model:value="settingsForm.poll_interval_seconds" :min="0.2" />
              </NFormItem>
              <NFormItem :label="t('重试间隔（秒）', 'Retry interval (seconds)')">
                <NInputNumber v-model:value="settingsForm.retry_interval_seconds" :min="1" />
              </NFormItem>
            </div>
          </NForm>
        </div>
      </section>

      <section class="panel">
        <div class="panel-inner">
          <h2 class="section-title">{{ t('采集状态', 'Collection Status') }}</h2>
          <NDescriptions label-placement="left" :column="1" size="small" bordered>
            <NDescriptionsItem :label="t('本地采集', 'Local collection')">
              <NTag :type="collectorStatus?.enabled ? 'success' : 'default'" size="small">
                {{ collectorStatus?.enabled ? t('开启', 'On') : t('关闭', 'Off') }}
              </NTag>
            </NDescriptionsItem>
            <NDescriptionsItem :label="t('运行状态', 'Run status')">
              <NTag :type="collectorStatus?.running ? 'success' : 'default'" size="small">
                {{ collectorStatus?.running ? t('运行中', 'Running') : t('空闲', 'Idle') }}
              </NTag>
            </NDescriptionsItem>
            <NDescriptionsItem :label="t('远端开关', 'Remote switch')">
              <NTag :type="remoteStatusType" size="small">
                {{ remoteStatusText }}
              </NTag>
            </NDescriptionsItem>
            <NDescriptionsItem :label="t('累计写入', 'Records written')">
              {{ formatInteger(collectorStatus?.records_collected ?? 0) }}
            </NDescriptionsItem>
            <NDescriptionsItem :label="t('最后轮询', 'Last poll')">
              {{ formatDateTime(collectorStatus?.last_poll_at ?? null) }}
            </NDescriptionsItem>
            <NDescriptionsItem :label="t('最后成功', 'Last success')">
              {{ formatDateTime(collectorStatus?.last_success_at ?? null) }}
            </NDescriptionsItem>
          </NDescriptions>
          <NAlert
            v-if="collectorStatus?.last_error"
            type="warning"
            :bordered="false"
            class="status-alert"
          >
            {{ serverText(collectorStatus.last_error, '采集异常', 'Collector error') }}
          </NAlert>
        </div>
      </section>
    </div>

    <section class="panel">
      <div class="panel-inner">
        <h2 class="section-title">{{ t('API KEY 设置', 'API Key Settings') }}</h2>
        <NForm :model="settingsForm" label-placement="top">
          <div class="form-grid">
            <div class="field-stack">
              <div class="field-label">{{ t('API KEY 前缀', 'API key prefix') }}</div>
              <NInput
                v-model:value="settingsForm.api_key_prefix"
                :placeholder="t('留空使用默认前缀 sk', 'Leave blank to use the default prefix sk')"
                :maxlength="32"
                show-count
              />
              <div class="form-help">
                {{ t(`新生成的 API KEY 形如 ${apiKeyPrefixPreview}。只允许字母、数字、- 和 _，不能以 - 开头或结尾；只影响之后新建的 KEY，已有 KEY 不变。`, `New API keys look like ${apiKeyPrefixPreview}. Letters, digits, - and _ only; must not start or end with -. Only affects keys created from now on; existing keys are unchanged.`) }}
              </div>
            </div>
          </div>
        </NForm>
      </div>
    </section>
  </section>
</template>

<style scoped>
.section-title {
  margin: 0 0 12px;
}

.settings-metrics {
  grid-template-columns: repeat(4, minmax(150px, 1fr));
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 18px 12px;
}

.field-stack {
  display: grid;
  gap: 6px;
  width: 100%;
  min-width: 0;
  align-content: start;
}

.field-label {
  color: var(--cpa-text);
  font-size: 14px;
  line-height: 1.35;
}

.form-help {
  margin: 0;
  padding-left: 8px;
  border-left: 2px solid color-mix(in srgb, var(--cpa-primary) 42%, transparent);
  color: var(--cpa-text);
  font-size: 13px;
  font-weight: 600;
  line-height: 1.45;
}

.status-alert {
  margin-top: 10px;
}

@media (max-width: 900px) {
  .settings-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .form-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 560px) {
  .settings-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
