<script setup lang="ts">
import { ElButton, ElSelect, ElOption } from 'element-plus'
import { FolderOpened, Refresh } from '@element-plus/icons-vue'
import type { ServerPCAPFile } from '../types'
import { bytes } from '../format'
defineProps<{files: ServerPCAPFile[]; directory: string; loading: boolean; error: string; truncated: boolean; disabled: boolean}>()
const selected = defineModel<string>({required:true})
defineEmits<{refresh: []; open: []}>()
</script>

<template>
  <section class="server-file-picker" aria-label="服务端离线文件">
    <div class="server-file-heading"><strong>服务端 PCAP</strong><span class="mono">{{ directory || '正在获取目录…' }}</span></div>
    <div class="server-file-controls">
      <ElSelect v-model="selected" filterable clearable :disabled="disabled || loading" aria-label="服务端 PCAP 文件" placeholder="选择服务所在设备上的抓包文件" class="server-file-select" no-data-text="目录中暂无 PCAP 文件">
        <ElOption v-for="file in files" :key="file.name" :value="file.name" :label="`${file.name} · ${bytes(file.size)}`" />
      </ElSelect>
      <ElButton type="primary" :icon="FolderOpened" :disabled="disabled || loading || !selected" @click="$emit('open')">加载文件</ElButton>
      <ElButton :icon="Refresh" :loading="loading" @click="$emit('refresh')">刷新目录</ElButton>
    </div>
    <p v-if="error" class="server-file-error">{{ error }}</p>
    <p v-else-if="truncated">目录较大，仅列出部分文件；可用 --pcap-dir 指向更具体的目录。</p>
    <p v-else>直接读取服务端目录，无需上传、无总大小限制。{{ !loading && !files.length ? '请将 .pcap / .cap 文件放入此目录后刷新。' : '支持搜索文件名，后台解析可随时停止。' }}</p>
  </section>
</template>
