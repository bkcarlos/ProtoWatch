<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { ElAlert, ElButton, ElCheckbox, ElDescriptions, ElDescriptionsItem, ElDialog, ElLink, ElProgress, ElTag } from 'element-plus'
import { bytes } from '../format'
import { Refresh } from '@element-plus/icons-vue'
interface UpdateStatus { current: string; platform: string; latest?: string; url?: string; phase: string; available: boolean; error?: string; backup?: string; downloaded: number; total: number }
const open = ref(false)
const status = ref<UpdateStatus>()
const error = ref('')
const confirmed = ref(false)
const requestBusy = ref(false)
const installing = ref(false)
const originalVersion = ref('')
const restartWait = ref(0)
const finished = ref(false)
const working = computed(() => requestBusy.value || installing.value || ['checking', 'downloading', 'installing', 'restarting'].includes(status.value?.phase || ''))
const phaseLabel = computed(() => ({checking:'正在检查 GitHub Release…', downloading:'正在下载并校验安装包…', installing:'正在备份并替换程序…', restarting:'正在重启，等待服务重新连接…'}[status.value?.phase || ''] || ''))
async function api(path: string, body?: object) {
  const response = await fetch(`/api/update${path}`, { method: body ? 'POST' : 'GET', headers: body ? { 'Content-Type': 'application/json' } : undefined, body: body ? JSON.stringify(body) : undefined, signal: AbortSignal.timeout(25000) })
  const data = await response.json()
  if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`)
  return data as UpdateStatus
}
async function poll() {
  try {
    const value = await api('')
    status.value = value
    if (!installing.value && ['downloading', 'installing', 'restarting'].includes(value.phase)) {
      installing.value = true; originalVersion.value = value.current; restartWait.value = Date.now()
    }
    if (installing.value && value.current !== originalVersion.value) { installing.value = false; finished.value = true; error.value = ''; return }
    if (value.phase === 'error') { installing.value = false; error.value = value.error || '更新失败' }
    if (installing.value && Date.now() - restartWait.value > 1860000) { installing.value = false; error.value = '更新等待超时，请检查服务进程后刷新页面。' }
  } catch (e) {
    if (!installing.value) error.value = e instanceof Error ? e.message : String(e)
    else if (Date.now() - restartWait.value > 1860000) { installing.value = false; error.value = '服务未恢复连接，请检查设备上的进程和日志。' }
  }
}
async function check() {
  error.value = ''; confirmed.value = false; requestBusy.value = true
  try { status.value = await api('/check', {}) } catch (e) { error.value = e instanceof Error ? e.message : String(e); await poll() }
  finally { requestBusy.value = false }
}
async function install() {
  if (!status.value?.latest || !confirmed.value) return
  requestBusy.value = true; error.value = ''; originalVersion.value = status.value.current; finished.value = false
  try { status.value = await api('/install', { version: status.value.latest }); installing.value = true; restartWait.value = Date.now() }
  catch (e) { error.value = e instanceof Error ? e.message : String(e) }
  finally { requestBusy.value = false }
}
let timer: ReturnType<typeof setTimeout> | undefined
let disposed = false
async function tick() { if (open.value || installing.value || !status.value) await poll(); if (!disposed) timer = setTimeout(tick, 2000) }
onMounted(() => { void tick() })
onUnmounted(() => { disposed = true; clearTimeout(timer) })
function reload() { window.location.reload() }
</script>

<template>
  <div class="update-entry">
    <small v-if="status">{{ status.current }} · {{ status.platform }}</small>
    <ElButton text :icon="Refresh" @click="open = true">版本更新</ElButton>
  </div>
  <ElDialog v-model="open" title="版本更新" width="560px" class="update-dialog" append-to-body :close-on-click-modal="!working" :close-on-press-escape="!working">
    <p class="update-intro">从 GitHub 官方仓库下载稳定版本，自动匹配当前设备平台。</p>
    <ElDescriptions :column="1" border>
      <ElDescriptionsItem label="当前版本">{{ status?.current || '正在读取…' }}</ElDescriptionsItem>
      <ElDescriptionsItem label="运行平台">{{ status?.platform || '—' }}</ElDescriptionsItem>
      <ElDescriptionsItem label="最新稳定版"><ElLink v-if="status?.url" :href="status.url" target="_blank" rel="noopener noreferrer" type="primary">{{ status.latest }}</ElLink><span v-else>尚未检查</span><ElTag v-if="status?.latest && !status.available" class="update-tag" type="success" size="small">无更高版本</ElTag></ElDescriptionsItem>
    </ElDescriptions>
    <ElAlert v-if="error" class="update-note" :title="error" type="error" :closable="false" show-icon />
    <ElAlert v-if="finished" class="update-note" title="更新完成，服务已重新连接。刷新页面以加载新版界面。" type="success" :closable="false" show-icon />
    <ElAlert v-else-if="phaseLabel || installing" class="update-note" :title="phaseLabel || '等待服务重新连接…'" type="info" :closable="false" show-icon />
    <div v-if="status?.phase === 'downloading' && status.total > 0" class="update-note">
      <ElProgress :percentage="Math.min(100, Math.floor(status.downloaded / status.total * 100))" />
      <small>{{ bytes(status.downloaded) }} / {{ bytes(status.total) }}</small>
    </div>
    <div class="update-note update-details">
      <p>安装前校验 SHA-256，并在程序目录保留旧二进制备份。</p>
      <p>重启沿用命令行启动参数；当前抓包记录、网页中修改的采集设置和临时 SOME/IP 映射不会保留。需要的数据请先导出。</p>
      <p>设备需能访问 GitHub，且运行用户对程序目录有写权限。</p>
    </div>
    <ElCheckbox v-if="status?.available && !finished" v-model="confirmed" :disabled="working">已保存需要的数据，允许安装并重启服务</ElCheckbox>
    <template #footer>
      <ElButton :loading="requestBusy && !installing" :disabled="working || finished" @click="check">检查更新</ElButton>
      <ElButton v-if="finished" type="primary" @click="reload">刷新页面</ElButton>
      <ElButton v-else type="primary" :disabled="!status?.available || !confirmed || working" :loading="installing" @click="install">安装并重启</ElButton>
    </template>
  </ElDialog>
</template>

<style scoped>
.update-entry { display:flex; flex-direction:column; align-items:flex-start; gap:8px; margin-top:12px; }
.update-entry small { color:var(--el-text-color-secondary); }
.update-entry .el-button { margin-left:-12px; color:#bdcfe3; }
:global(.update-dialog) { max-width:calc(100vw - 32px); }
:global(.update-dialog .el-dialog__body) { max-height:65vh; overflow-y:auto; }
.update-intro { margin:0 0 18px; color:var(--el-text-color-secondary); line-height:1.6; }
.update-note { margin-top:16px; }
.update-details { font-size:13px; line-height:1.7; color:var(--el-text-color-secondary); }
.update-details p { margin:8px 0; }
.update-tag { margin-left:12px; }
</style>
