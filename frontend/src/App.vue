<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { createWatchClient } from './composables/useWatch'
import { number, statuses, time, bytes } from './format'
import ServerFilePicker from './components/ServerFilePicker.vue'
import type { SampleDetail, Tab, Topic, Protocol, SomeIPTab } from './types'
import ObservationTable from './components/ObservationTable.vue'
import SampleTable from './components/SampleTable.vue'
import SampleDialog from './components/SampleDialog.vue'
import SomeIPPanel from './components/SomeIPPanel.vue'
import { ElAlert, ElBreadcrumb, ElBreadcrumbItem, ElButton, ElCard, ElConfigProvider, ElIcon, ElMenu, ElMenuItem, ElOption, ElRadioGroup, ElRadioButton, ElSelect, ElStatistic, ElTag, ElTooltip } from 'element-plus'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import { Connection, DataLine, Download, Monitor, Upload, VideoPause, VideoPlay, Warning, Share } from '@element-plus/icons-vue'

const client = createWatchClient()
const { state, interfaces, selectedInterface, liveSupported, connected, notice, paused, busy, running, uploading, maxImportBytes } = client
const tab = ref<Tab>('topics')
const protocol = ref<Protocol>('dds')
const someipTab = ref<SomeIPTab>('services')
const activeTab = computed(() => protocol.value === 'dds' ? tab.value : someipTab.value)
function selectTab(value: string) { if (protocol.value === 'dds') tab.value = value as Tab; else someipTab.value = value as SomeIPTab }
const filter = ref<Topic | null>(null)
const detail = ref<SampleDetail | null>(null)
const fileInput = ref<HTMLInputElement>()
const rate = computed(() => (state.value?.topics || []).reduce((total, topic) => total + topic.hz, 0))
const nav = computed(() => protocol.value === 'dds' ? [
  { id: 'topics' as const, icon: DataLine, label: 'Topics', count: state.value?.topics.length || 0 },
  { id: 'participants' as const, icon: Monitor, label: '参与者', count: state.value?.participants.length || 0 },
  { id: 'endpoints' as const, icon: Share, label: '读写端点', count: state.value?.endpoints.length || 0 },
  { id: 'diagnostics' as const, icon: Warning, label: '观察提示', count: Object.keys(state.value?.warnings || {}).length },
 ] : [
  { id: 'services', icon: Monitor, label: '服务实例', count: state.value?.someip.services.length || 0 },
  { id: 'members', icon: Share, label: '方法与事件', count: state.value?.someip.members.length || 0 },
  { id: 'discovery', icon: DataLine, label: '服务发现', count: state.value?.someip.discovery_count || 0 },
  { id: 'diagnostics', icon: Warning, label: '观察提示', count: Object.keys(state.value?.someip.warnings || {}).length + Object.keys(state.value?.warnings || {}).length },
])
const endpointCount = (kind: string) => state.value?.endpoints.filter(endpoint => endpoint.kind === kind).length || 0
const sourceMeta = computed(() => state.value?.mode === 'file' ? state.value.status === 'importing' ? '离线文件 · 后端正在逐包读取' : state.value.status === 'complete' ? '离线文件 · 统计截至抓包结束' : '离线文件 · 当前为部分解析结果' : state.value?.mode === 'live' ? '实时接口 · 只呈现此接口可见的数据' : '选择设备网卡，或导入已有的 PCAP 抓包')
let detailRequest = 0
watch(protocol, () => { detail.value = null; detailRequest++ })
watch(() => state.value?.started, () => { filter.value = null; detail.value = null; detailRequest++ })
async function openSample(id: number) {
  const request = ++detailRequest
  try { const result = await client.sampleDetail(id); if (request === detailRequest) detail.value = result }
  catch (error) { notice.value = error instanceof Error ? error.message : String(error) }
}
async function uploadInput(event: Event) {
  const input = event.target as HTMLInputElement
  await client.upload(input.files?.[0]); input.value = ''
}
function drop(event: DragEvent) { event.preventDefault(); void client.upload(event.dataTransfer?.files[0]) }
const preventDrag = (event: DragEvent) => event.preventDefault()
onMounted(() => { client.startPolling(); window.addEventListener('dragover', preventDrag); window.addEventListener('drop', drop) })
onUnmounted(() => { detailRequest++; client.dispose(); window.removeEventListener('dragover', preventDrag); window.removeEventListener('drop', drop) })
</script>

<template>
  <ElConfigProvider :locale="zhCn">
    <div class="app-shell">
      <aside class="sidebar">
        <a class="brand" href="/"><ElIcon :size="26"><Connection /></ElIcon><span>ProtoWatch<small>网络观察工作台</small></span></a>
        <div class="nav-label">观察工作空间</div>
        <ElMenu :default-active="activeTab" @select="selectTab">
          <ElMenuItem v-for="item in nav" :key="item.id" :index="item.id">
            <ElIcon><component :is="item.icon" /></ElIcon><span>{{ item.label }}</span><span class="nav-count">{{ item.count }}</span>
          </ElMenuItem>
        </ElMenu>
        <div class="sidebar-bottom"><ElTag type="info" effect="plain" size="small">被动观察模式</ElTag><p>仅呈现捕获范围内的流量<br>不创建 DDS 参与者</p><small>MVP 0.2 · Linux ARM64</small></div>
      </aside>
      <div class="main-area">
        <header class="topbar"><ElBreadcrumb separator="/"><ElBreadcrumbItem>工作空间</ElBreadcrumbItem><ElBreadcrumbItem>{{ protocol === 'dds' ? 'DDS' : 'SOME/IP' }} 网络观察</ElBreadcrumbItem></ElBreadcrumb><div class="protocol-controls"><ElRadioGroup v-model="protocol" size="small" aria-label="协议选择"><ElRadioButton value="dds">DDS</ElRadioButton><ElRadioButton value="someip">SOME/IP</ElRadioButton></ElRadioGroup><ElTag :type="connected ? 'success' : 'danger'" effect="light" round><span class="status-dot" />{{ connected ? '设备已连接' : '连接不可用' }}</ElTag></div></header>
        <main>
          <section class="page-heading"><div><h1>{{ protocol === 'dds' ? 'DDS' : 'SOME/IP' }} 网络观察</h1><p>{{ protocol === 'dds' ? '发现参与者，定位 Topic，检查捕获的数据样本。' : '发现服务实例，观察请求、响应与事件通知。' }}</p></div><ElButton :icon="Download" :disabled="busy || !connected" @click="client.exportSnapshot">导出快照</ElButton></section>
          <ElCard class="source-panel" shadow="never">
            <div class="source-top"><div class="source-title"><div class="source-symbol"><ElIcon :size="24"><Connection /></ElIcon></div><div><strong>{{ state?.source || '创建观察会话' }}</strong><p>{{ sourceMeta }}</p></div></div><ElTag :type="state?.status === 'error' ? 'danger' : state?.status === 'running' ? 'success' : 'info'">{{ busy ? '正在处理' : state?.mode === 'file' && state.status === 'stopped' ? '解析已停止' : statuses[state?.status || 'idle'] }}</ElTag></div>
            <div class="source-controls">
              <div class="capture-controls"><label for="capture-interface">采集网卡</label><ElSelect id="capture-interface" v-model="selectedInterface" filterable placeholder="选择网卡" aria-label="采集网卡" :disabled="running || busy" class="interface-select"><ElOption v-for="item in interfaces" :key="item.name" :value="item.name" :label="`${item.name} · ${item.addresses[0] || '无地址'}`" :disabled="!item.up || item.loopback" /></ElSelect>
              <ElTooltip :disabled="liveSupported || running" content="实时采集需要在 Linux 设备上运行"><span><ElButton v-if="!running" type="primary" :icon="VideoPlay" :disabled="!liveSupported || !selectedInterface || busy || !connected" @click="client.startCapture">开始观察</ElButton><ElButton v-else-if="!uploading" type="danger" plain :icon="VideoPause" :disabled="busy" @click="client.stopCapture">{{ state?.mode === 'file' ? '停止解析' : '停止采集' }}</ElButton></span></ElTooltip></div>
              <div class="import-controls"><ElButton v-if="uploading" type="danger" plain @click="client.cancelUpload">取消导入</ElButton><ElButton :icon="Upload" :disabled="running || busy || !connected" @click="fileInput?.click()">浏览器上传</ElButton><input ref="fileInput" type="file" accept=".pcap,.cap" hidden @change="uploadInput"><ElButton type="primary" plain :loading="busy" :disabled="running || !connected" @click="client.demo(protocol)">{{ protocol === 'dds' ? '试用合成示例' : '试用混合示例' }}</ElButton></div>
            </div>
            <ServerFilePicker :files="client.serverFiles.value" :directory="client.serverDirectory.value" :loading="client.filesLoading.value" :error="client.filesError.value" :truncated="client.filesTruncated.value" :disabled="running || busy || !connected" v-model="client.selectedServerFile.value" @refresh="client.loadServerFiles" @open="client.openServerFile" />
            <div class="source-help">{{ liveSupported ? '单网卡被动采集 · 需要 CAP_NET_RAW 权限' : '当前主机支持离线分析；实时采集请在 Linux 设备上运行。' }}<span>支持拖入 PCAP · 上传上限 {{ bytes(maxImportBytes) }}</span></div>
            <p v-if="state?.mode === 'file' && (state.import_bytes > 0 || state.status === 'importing')" class="import-progress" role="status">已读取 {{ bytes(state.import_bytes || 0) }}{{ state.import_total > 0 ? ` / ${bytes(state.import_total)}` : '' }} · 已解析 {{ number(state.packets) }} 包{{ state.status === 'importing' ? ' · 逐包解析中' : state.status !== 'complete' ? ' · 部分结果' : '' }}</p>
          </ElCard>
          <ElAlert v-if="notice" class="notice" :title="notice" :type="!connected || state?.error ? 'error' : 'info'" show-icon closable @close="notice = ''" />
          <section v-if="protocol === 'dds'" class="metrics">
            <ElCard shadow="never"><ElStatistic title="已观察参与者" :value="state?.participants.length || 0" /><p>以 GUID 区分，不等同于进程</p></ElCard>
            <ElCard shadow="never"><ElStatistic title="发现的 Topics" :value="state?.topics.length || 0" /><p>{{ endpointCount('writer') }} 个发布端 <span>／</span> {{ endpointCount('reader') }} 个订阅端</p></ElCard>
            <ElCard shadow="never"><ElStatistic title="用户样本" :value="state?.sample_count || 0" /><p>最近 5 秒 <b>{{ rate.toFixed(1) }}</b> 样本 / 秒</p></ElCard>
            <ElCard shadow="never"><ElStatistic title="捕获的 RTPS 包" :value="state?.rtps_packets || 0" /><p>内核丢弃 {{ state?.kernel_drops == null ? '不可用' : number(state.kernel_drops) }}</p></ElCard>
          </section>
          <section v-else class="metrics">
            <ElCard shadow="never"><ElStatistic title="服务记录" :value="state?.someip.services.length || 0" /><p>包含有效公告与实例未知的服务</p></ElCard>
            <ElCard shadow="never"><ElStatistic title="用户消息" :value="state?.someip.message_count || 0" /><p>请求 {{ state?.someip.requests || 0 }} / 响应 {{ state?.someip.responses || 0 }}</p></ElCard>
            <ElCard shadow="never"><ElStatistic title="事件通知" :value="state?.someip.notifications || 0" /><p>错误消息 {{ state?.someip.errors || 0 }}</p></ElCard>
            <ElCard shadow="never"><ElStatistic title="发现条目" :value="state?.someip.discovery_count || 0" /><p>SOME/IP 相关载荷包 {{ state?.someip.packets || 0 }}（含 TCP 重传）</p></ElCard>
          </section>
          <template v-if="protocol === 'dds'">
          <ObservationTable :state="state" :tab="tab" :selected-key="filter?.key" @select="filter = $event" />
          <SampleTable :key="state?.started" :samples="state?.samples || []" :filter="filter" :paused="paused" :has-topics="Boolean(state?.topics.length)" @select="openSample" @clear="filter = null" @pause="client.togglePause" />
          </template>
          <SomeIPPanel v-else-if="state" :key="state.started" :state="state.someip" :tab="someipTab" :busy="busy" :connected="connected" :paused="paused" :common-warnings="state.warnings" :get-detail="client.someipDetail" @pause="client.togglePause" @mapping="client.importMapping" @demo-mapping="client.demoMapping" @clear-mapping="client.updateMapping('{}')" @error="notice = $event" />
          <footer><span>ProtoWatch · 本地数据，本地观察</span><span>{{ state?.packets ? `最后捕获 ${time(state.observed)} · 总计 ${number(state.packets)} 包` : '等待输入' }}</span></footer>
        </main>
      </div>
      <SampleDialog :detail="detail" @close="detail = null" @error="notice = $event" />
    </div>
  </ElConfigProvider>
</template>
