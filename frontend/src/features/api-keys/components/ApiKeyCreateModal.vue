<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  NAlert,
  NButton,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NRadioButton,
  NRadioGroup,
  useMessage,
} from 'naive-ui'

import { useI18n } from '@/shared/i18n'
import type { ApiKeyCreatePayload } from '@/shared/types/api'

const props = withDefaults(
  defineProps<{
    show: boolean
    title: string
    loading: boolean
    targetLabel?: string
  }>(),
  {
    targetLabel: '',
  },
)

const emit = defineEmits<{
  'update:show': [show: boolean]
  submit: [payload: ApiKeyCreatePayload]
}>()

const message = useMessage()
const { t } = useI18n()
const description = ref('VSCode')
const creationMode = ref<'automatic' | 'custom'>('automatic')
const customApiKey = ref('')

const visible = computed({
  get: () => props.show,
  set: (show: boolean) => {
    if (!show && props.loading) {
      return
    }
    emit('update:show', show)
  },
})

watch(
  () => props.show,
  () => {
    description.value = 'VSCode'
    creationMode.value = 'automatic'
    customApiKey.value = ''
  },
)

function submit() {
  if (props.loading) {
    return
  }
  const normalizedDescription = description.value.trim()
  if (!normalizedDescription) {
    message.error(t('API KEY 描述不能为空', 'API key description is required'))
    return
  }
  if (creationMode.value === 'custom' && !customApiKey.value) {
    message.error(t('自定义 API KEY 不能为空', 'Custom API key is required'))
    return
  }
  const payload: ApiKeyCreatePayload = { description: normalizedDescription }
  if (creationMode.value === 'custom') {
    payload.api_key = customApiKey.value
  }
  emit('submit', payload)
}
</script>

<template>
  <NModal
    v-model:show="visible"
    preset="card"
    :mask-closable="false"
    :close-on-esc="!loading"
    :closable="false"
    :title="title"
    :style="{ width: 'min(520px, calc(100vw - 32px))' }"
  >
    <NAlert v-if="targetLabel" type="info" :bordered="false" class="target-alert">
      {{ t(`正在为 ${targetLabel} 创建 API 密钥。`, `Creating an API key for ${targetLabel}.`) }}
    </NAlert>

    <NForm label-placement="top">
      <NFormItem :label="t('API KEY 描述', 'API key description')" required>
        <NInput
          v-model:value="description"
          :disabled="loading"
          :maxlength="240"
          :placeholder="t('例如：VSCode', 'Example: VSCode')"
          @keyup.enter="submit"
        />
      </NFormItem>

      <NFormItem :label="t('密钥生成方式', 'Key creation mode')">
        <NRadioGroup v-model:value="creationMode" :disabled="loading">
          <NRadioButton value="automatic">{{ t('自动生成', 'Generate automatically') }}</NRadioButton>
          <NRadioButton value="custom">{{ t('自定义密钥', 'Custom key') }}</NRadioButton>
        </NRadioGroup>
      </NFormItem>

      <NFormItem
        v-if="creationMode === 'custom'"
        :label="t('自定义 API KEY', 'Custom API key')"
        required
      >
        <NInput
          v-model:value="customApiKey"
          type="password"
          show-password-on="mousedown"
          autocomplete="new-password"
          :disabled="loading"
          :maxlength="400"
          :placeholder="t('输入需要保留的完整密钥', 'Enter the full key to preserve')"
          @keyup.enter="submit"
        />
      </NFormItem>

      <NAlert v-if="creationMode === 'custom'" type="warning" :bordered="false" class="custom-key-hint">
        {{ t('密钥内容会原样使用，请确认没有多余空格，并使用足够安全且唯一的值。', 'The key is used exactly as entered. Ensure it has no extra spaces and is sufficiently secure and unique.') }}
      </NAlert>

      <div class="modal-actions">
        <NButton secondary :disabled="loading" @click="visible = false">{{ t('取消', 'Cancel') }}</NButton>
        <NButton type="primary" :loading="loading" :disabled="loading" @click="submit">
          {{ t('创建', 'Create') }}
        </NButton>
      </div>
    </NForm>
  </NModal>
</template>

<style scoped>
.target-alert {
  margin-bottom: 12px;
}

.custom-key-hint {
  margin: -2px 0 12px;
}

.modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
