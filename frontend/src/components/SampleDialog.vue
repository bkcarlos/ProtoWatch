<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElAlert, ElButton, ElDescriptions, ElDescriptionsItem, ElDialog, ElTabPane, ElTabs } from 'element-plus'
import { CopyDocument } from '@element-plus/icons-vue'
import type { SampleDetail } from '../types'
import { bytes } from '../format'
const props = defineProps<{ detail: SampleDetail | null }>()
const emit = defineEmits<{ close: []; error: [message: string] }>()
const encoding = ref('hex')
const copied = ref(false)
watch(() => props.detail, () => { copied.value = false; encoding.value = 'hex' })
watch(encoding, () => { copied.value = false })
const payload = computed(() => encoding.value === 'hex' ? props.detail?.hex || '' : props.detail?.payload_base64 || '')
const fields = computed(() => {
  const s = props.detail?.sample
  return s ? { Topic: s.topic || '未知', 'Writer GUID': s.writer, '序列号': s.sequence, '捕获时间': s.time, '包编号': s.packet, '源地址': s.source, '载荷大小': bytes(s.size) + (s.truncated ? '（只保留前 16 KiB）' : ''), '编码': s.encoding } : {}
})
async function copy() {
  try { await navigator.clipboard.writeText(payload.value); copied.value = true }
  catch { emit('error', '此连接不支持剪贴板，请手动选择并复制载荷文本。') }
}
</script>
<template>
  <ElDialog :model-value="Boolean(detail)" title="样本详情" class="sample-dialog" width="820px" align-center destroy-on-close @update:model-value="value => { if (!value) emit('close') }">
    <div id="detail">
      <ElDescriptions :column="1" border size="small"><ElDescriptionsItem v-for="(value, key) in fields" :key="key" :label="String(key)"><span class="mono">{{ value }}</span></ElDescriptionsItem></ElDescriptions>
      <ElAlert title="原始序列化载荷 · 当前版本不进行业务字段解码" type="info" :closable="false" show-icon class="payload-note" />
      <ElTabs v-model="encoding"><ElTabPane label="十六进制" name="hex" /><ElTabPane label="Base64" name="base64" /></ElTabs>
      <pre id="hex" class="payload">{{ payload || '空载荷' }}</pre>
    </div>
    <template #footer><ElButton @click="emit('close')">关闭</ElButton><ElButton type="primary" :icon="CopyDocument" @click="copy">{{ copied ? '已复制' : '复制载荷' }}</ElButton></template>
  </ElDialog>
</template>
