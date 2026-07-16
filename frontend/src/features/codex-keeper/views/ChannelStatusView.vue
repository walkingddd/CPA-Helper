<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  NButton,
  NCard,
  NEmpty,
  NGrid,
  NGi,
  NProgress,
  NSpace,
  NSpin,
  NTag,
  useMessage,
} from 'naive-ui'
import { Activity, CheckCircle2, Clock3, RadioTower, RefreshCw, ShieldAlert, Zap } from 'lucide-vue-next'

import { getChannelStatus } from '@/features/codex-keeper/api/codexKeeperApi'
import type { ChannelStatusItem, ChannelStatusResponse, ChannelStatusState } from '@/shared/types/api'
import { useI18n } from '@/shared/i18n'
import { formatDateTime, formatInteger } from '@/shared/utils/format'

const message = useMessage()
const { errorText, t } = useI18n()
const isLoading = ref(false)
const response = ref<ChannelStatusResponse | null>(null)

const items = computed(() => response.value?.items ?? [])
const healthyCount = computed(() => items.value.filter((item) => item.status === 'healthy').length)
const availableCount = computed(
  () =>
    items.value.filter((item) => !['disabled', 'unauthorized', 'quota_exhausted', 'error'].includes(item.status))
      .length,
)
const problemCount = computed(() => Math.max(0, items.value.length - availableCount.value))

async function load() {
  isLoading.value = true
  try {
    response.value = await getChannelStatus()
  } catch (error) {
    message.error(errorText(error, t('Failed to load channel status', 'Failed to load channel status')))
  } finally {
    isLoading.value = false
  }
}

function statusTagType(status: ChannelStatusState) {
  if (status === 'healthy' || status === 'checked') {
    return 'success'
  }
  if (status === 'disabled' || status === 'quota_exhausted') {
    return 'warning'
  }
  if (status === 'unauthorized' || status === 'error') {
    return 'error'
  }
  return 'default'
}

function quotaPercent(value: number | null): number {
  if (value === null || !Number.isFinite(value)) {
    return 0
  }
  return Math.max(0, Math.min(100, value))
}

function quotaStatus(value: number | null) {
  if (value === null) {
    return 'default'
  }
  if (value >= 100) {
    return 'error'
  }
  if (value >= 80) {
    return 'warning'
  }
  return 'success'
}

function quotaText(value: number | null): string {
  return value === null ? '-' : `${formatInteger(value)}%`
}

function accountTypeText(item: ChannelStatusItem): string {
  return item.account_type || t('Uncategorized', 'Uncategorized')
}

function checkedText(item: ChannelStatusItem): string {
  if (item.last_healthy_at) {
    return t('Healthy at', 'Healthy at') + ' ' + formatDateTime(item.last_healthy_at)
  }
  if (item.last_checked_at) {
    return t('Checked at', 'Checked at') + ' ' + formatDateTime(item.last_checked_at)
  }
  return t('Not checked', 'Not checked')
}

function resetText(value: string | null): string {
  return value ? formatDateTime(value, { includeSecond: false }) : '-'
}

onMounted(() => {
  void load()
})
</script>

<template>
  <div class="channel-status-view">
    <div class="page-header">
      <div>
        <div class="eyebrow">
          <RadioTower :size="16" />
          {{ t('Channel Status', 'Channel Status') }}
        </div>
        <h1>{{ t('Available Channels', 'Available Channels') }}</h1>
        <p>
          {{
            t(
              'This page shows masked channel health and quota status without exposing real accounts or auth file names.',
              'This page shows masked channel health and quota status without exposing real accounts or auth file names.',
            )
          }}
        </p>
      </div>
      <NButton :loading="isLoading" type="primary" secondary @click="load">
        <template #icon>
          <RefreshCw :size="16" />
        </template>
        {{ t('Refresh', 'Refresh') }}
      </NButton>
    </div>

    <NGrid :cols="3" :x-gap="16" :y-gap="16" responsive="screen" class="summary-grid">
      <NGi>
        <NCard>
          <div class="summary-card">
            <RadioTower :size="24" />
            <div>
              <strong>{{ formatInteger(items.length) }}</strong>
              <span>{{ t('Total channels', 'Total channels') }}</span>
            </div>
          </div>
        </NCard>
      </NGi>
      <NGi>
        <NCard>
          <div class="summary-card success">
            <CheckCircle2 :size="24" />
            <div>
              <strong>{{ formatInteger(healthyCount) }}</strong>
              <span>{{ t('Healthy', 'Healthy') }}</span>
            </div>
          </div>
        </NCard>
      </NGi>
      <NGi>
        <NCard>
          <div class="summary-card warning">
            <ShieldAlert :size="24" />
            <div>
              <strong>{{ formatInteger(problemCount) }}</strong>
              <span>{{ t('Need attention', 'Need attention') }}</span>
            </div>
          </div>
        </NCard>
      </NGi>
    </NGrid>

    <div v-if="response?.refreshed_at" class="refreshed-at">
      <Clock3 :size="14" />
      {{ t('Updated at', 'Updated at') }} {{ formatDateTime(response.refreshed_at) }}
    </div>

    <NSpin :show="isLoading && items.length === 0">
      <NEmpty v-if="!isLoading && items.length === 0" :description="t('No channel status yet', 'No channel status yet')" />
      <div v-else class="channel-grid">
        <NCard v-for="item in items" :key="item.id" class="channel-card">
          <template #header>
            <div class="card-title">
              <span>{{ item.name }}</span>
              <NTag :type="statusTagType(item.status)" round>{{ item.status_label }}</NTag>
            </div>
          </template>
          <NSpace vertical :size="12">
            <div class="meta-row">
              <span>{{ accountTypeText(item) }}</span>
              <span v-if="item.email">{{ item.email }}</span>
            </div>
            <p class="status-detail">{{ item.status_detail }}</p>
            <div class="quota-block">
              <div class="quota-header">
                <span><Zap :size="14" /> {{ t('Short window', 'Short window') }}</span>
                <strong>{{ quotaText(item.primary_used_percent) }}</strong>
              </div>
              <NProgress
                type="line"
                :percentage="quotaPercent(item.primary_used_percent)"
                :status="quotaStatus(item.primary_used_percent)"
                :show-indicator="false"
              />
              <small>{{ t('Reset', 'Reset') }}: {{ resetText(item.primary_reset_at) }}</small>
            </div>
            <div class="quota-block">
              <div class="quota-header">
                <span><Activity :size="14" /> {{ t('Long window', 'Long window') }}</span>
                <strong>{{ quotaText(item.secondary_used_percent) }}</strong>
              </div>
              <NProgress
                type="line"
                :percentage="quotaPercent(item.secondary_used_percent)"
                :status="quotaStatus(item.secondary_used_percent)"
                :show-indicator="false"
              />
              <small>{{ t('Reset', 'Reset') }}: {{ resetText(item.secondary_reset_at) }}</small>
            </div>
            <div class="checked-text">{{ checkedText(item) }}</div>
          </NSpace>
        </NCard>
      </div>
    </NSpin>
  </div>
</template>

<style scoped>
.channel-status-view {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.page-header {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: flex-start;
}

.page-header h1 {
  margin: 4px 0 8px;
  font-size: 28px;
}

.page-header p {
  margin: 0;
  color: var(--text-muted, #6b7280);
  max-width: 760px;
}

.eyebrow,
.refreshed-at,
.quota-header span,
.summary-card,
.card-title {
  display: flex;
  align-items: center;
  gap: 8px;
}

.eyebrow {
  color: #2563eb;
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.summary-grid {
  margin-top: 4px;
}

.summary-card strong {
  display: block;
  font-size: 26px;
}

.summary-card span {
  color: var(--text-muted, #6b7280);
}

.summary-card.success {
  color: #16a34a;
}

.summary-card.warning {
  color: #d97706;
}

.refreshed-at {
  color: var(--text-muted, #6b7280);
  font-size: 13px;
}

.channel-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 16px;
}

.channel-card {
  min-height: 280px;
}

.card-title {
  justify-content: space-between;
}

.meta-row {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  color: var(--text-muted, #6b7280);
  font-size: 13px;
}

.status-detail {
  margin: 0;
  min-height: 40px;
  color: var(--text-muted, #6b7280);
}

.quota-block {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.quota-header {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  font-size: 13px;
}

.quota-block small,
.checked-text {
  color: var(--text-muted, #6b7280);
  font-size: 12px;
}

@media (max-width: 720px) {
  .page-header {
    flex-direction: column;
  }
}
</style>
