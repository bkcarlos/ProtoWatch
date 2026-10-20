<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { ElAlert, ElButton, ElCard, ElDescriptions, ElDescriptionsItem, ElDialog, ElInput, ElPagination, ElSelect, ElOption, ElTable, ElTableColumn, ElTag, ElTabs, ElTabPane } from 'element-plus'
import { Search, Upload, VideoPause, VideoPlay, CopyDocument } from '@element-plus/icons-vue'
import type { SomeIPState, SomeIPTab, SomeIPService, SomeIPSample, SomeIPDetail } from '../types'
import { hexID, endpointText, serviceStatuses, someipWarnings } from '../someip'
import { bytes, time, warnings } from '../format'
import EmptyState from './EmptyState.vue'

const props = defineProps<{ state: SomeIPState; tab: SomeIPTab; paused: boolean; busy: boolean; connected: boolean; commonWarnings: Record<string, number>; getDetail: (id: number) => Promise<SomeIPDetail> }>()
const emit = defineEmits<{ pause: []; mapping: [file?: File]; demoMapping: []; clearMapping: []; error: [message: string] }>()
const search = ref('')
const filterKey = ref('')
const provider = ref('')
const transport = ref('')
const page = ref(1)
const pageSize = ref(10)
const mappingFile = ref<HTMLInputElement>()
const detail = ref<SomeIPDetail | null>(null)
const encoding = ref('hex')
const copied = ref(false)
const filter = computed(() => props.state.services.find(s => s.key === filterKey.value))
const match = (...values: unknown[]) => values.join(' ').toLowerCase().includes(search.value.trim().toLowerCase())
const serviceIndex = computed(() => new Map(props.state.services.map(s => [s.key, s])))
const providerOf = (key: string) => serviceIndex.value.get(key)?.provider || '未知'
const inProvider = (key: string) => !provider.value || serviceIndex.value.get(key)?.provider === provider.value
const providers = computed(() => {
  const groups = new Map<string, { address: string; services: number; messages: number }>()
  for (const s of props.state.services) {
    const group = groups.get(s.provider) || { address: s.provider, services: 0, messages: 0 }
    group.services++; group.messages += s.messages
    groups.set(s.provider, group)
  }
  return [...groups.values()].sort((a, b) => a.address.localeCompare(b.address, undefined, { numeric: true }))
})
const services = computed(() => props.state.services.filter(s => inProvider(s.key) && match(s.name, hexID(s.service_id), hexID(s.instance_id), s.provider, endpointText(s.endpoints))))
const members = computed(() => props.state.members.filter(m => inProvider(m.service_key) && match(m.name, hexID(m.service_id), hexID(m.method_id), m.kind, providerOf(m.service_key))))
const discovery = computed(() => props.state.discovery.filter(e => match(e.kind, e.service_name, hexID(e.service_id), hexID(e.instance_id), e.eventgroup_name, hexID(e.eventgroup_id), e.source)))
const messages = computed(() => props.state.samples.filter(s => inProvider(s.service_key) && (!transport.value || s.transport === transport.value) && (!filterKey.value || s.service_key === filterKey.value)))
const visibleMessages = computed(() => messages.value.slice((page.value - 1) * pageSize.value, page.value * pageSize.value))
const diagnostics = computed(() => [...Object.entries(props.state.warnings).map(([key, count]) => ({ key, count, text: someipWarnings[key] || key })), ...Object.entries(props.commonWarnings).map(([key, count]) => ({ key: `network:${key}`, count, text: warnings[key]?.[0] || key }))].filter(d => match(d.text, d.key)))
const title = computed(() => ({ services: '服务实例', members: '方法与事件', discovery: '服务发现记录', diagnostics: '观察提示' })[props.tab])
const payload = computed(() => encoding.value === 'hex' ? detail.value?.hex || '' : detail.value?.payload_base64 || '')
watch(() => props.tab, () => { search.value = '' })
watch(provider, () => { filterKey.value = ''; page.value = 1 })
watch([filterKey, pageSize, transport], () => { page.value = 1 })
watch(() => messages.value.length, n => { page.value = Math.min(page.value, Math.max(1, Math.ceil(n / pageSize.value))) })
watch(encoding, () => { copied.value = false })
let requestID = 0
onUnmounted(() => { requestID++ })
async function inspect(id: number) {
  const version = ++requestID
  try { const result = await props.getDetail(id); if (version === requestID) { detail.value = result; encoding.value = 'hex'; copied.value = false } }
  catch (error) { emit('error', error instanceof Error ? error.message : String(error)) }
}
function closeDetail() { detail.value = null; requestID++ }
function mappingInput(event: Event) { const input = event.target as HTMLInputElement; emit('mapping', input.files?.[0]); input.value = '' }
async function copy() { try { await navigator.clipboard.writeText(payload.value); copied.value = true } catch { emit('error', '无法访问剪贴板，请选择载荷文本手动复制。') } }
</script>

<template>
  <div class="someip-mapping-bar">
    <div><strong>SOME/IP · TCP / UDP</strong><p>名称来自用户映射；未匹配 SD 时实例显示未知。仅报文头匹配属于协议推测。</p></div>
    <div class="mapping-actions"><ElButton :icon="Upload" :disabled="busy || !connected" @click="mappingFile?.click()">导入名称映射</ElButton><ElButton :disabled="busy || !connected" @click="emit('demoMapping')">载入示例映射</ElButton><ElButton link type="primary" :disabled="busy || !connected" @click="emit('clearMapping')">清除映射</ElButton><a href="/api/someip/config" download="someip-names.json" class="mapping-download">导出映射</a><input ref="mappingFile" type="file" accept=".json" hidden @change="mappingInput"></div>
  </div>
  <div class="someip-provider-bar">
    <label for="someip-provider">服务提供方</label>
    <ElSelect id="someip-provider" v-model="provider" aria-label="服务提供方" placeholder="全部提供方" filterable class="provider-select">
      <ElOption label="全部提供方" value="" />
      <ElOption v-for="item in providers" :key="item.address" :value="item.address" :label="`${item.address} · ${item.services} 个服务记录 · ${item.messages} 条消息`" />
    </ElSelect>
    <ElButton v-if="provider" link type="primary" @click="provider = ''">清除提供方筛选</ElButton>
    <p>{{ tab === 'discovery' || tab === 'diagnostics' ? '发现记录与观察提示显示全会话；下方消息按提供方筛选。' : '联动筛选服务、方法 / 事件与消息；顶部统计为全会话。' }}</p>
  </div>
  <ElCard class="workspace" shadow="never">
    <template #header><div class="section-bar"><div><h2>{{ title }}</h2><p>{{ tab === 'discovery' ? '保留最近 256 条公告与订阅记录，不代表完整网络状态' : '服务 ID 与实例分别展示 · 点击服务名称筛选消息' }}</p></div><ElInput v-model="search" :prefix-icon="Search" clearable class="table-search" aria-label="SOME/IP 筛选列表" placeholder="搜索名称、ID 或地址" /></div></template>
    <div id="someip-table">
      <ElTable v-if="tab === 'services'" :data="services" row-key="key" stripe max-height="440" :row-class-name="({ row }: { row: SomeIPService }) => row.key === filterKey ? 'selected-topic' : ''" @row-click="(row: SomeIPService) => filterKey = row.key">
        <ElTableColumn label="服务 / 实例" min-width="230"><template #default="{ row }"><ElButton type="primary" link @click.stop="filterKey = row.key">{{ row.name || hexID(row.service_id) }}</ElButton><div class="sub mono">Service {{ hexID(row.service_id) }} · Instance {{ hexID(row.instance_id) }}</div><div class="sub">接口版本 {{ row.major }}.{{ row.minor ?? '未知' }}{{ row.name ? ' · 配置名称' : '' }}</div></template></ElTableColumn>
        <ElTableColumn label="服务端点" min-width="230"><template #default="{ row }"><span class="mono">{{ endpointText(row.endpoints) }}</span><div class="sub">提供方 {{ row.provider }}</div></template></ElTableColumn>
        <ElTableColumn label="发现状态" width="140"><template #default="{ row }"><ElTag size="small" :type="row.status === 'offered' ? 'success' : row.status === 'observed' ? 'info' : 'warning'">{{ serviceStatuses[row.status] }}</ElTag><div class="sub">{{ row.status === 'observed' ? '没有有效实例公告' : `公告 TTL ${row.ttl} 秒` }}</div></template></ElTableColumn>
        <ElTableColumn prop="messages" label="消息" width="90" sortable />
        <ElTableColumn prop="hz" label="频率" width="106" sortable><template #default="{ row }">{{ row.hz.toFixed(1) }} Hz</template></ElTableColumn>
        <ElTableColumn label="载荷速率" width="110"><template #default="{ row }">{{ bytes(row.bytes_per_second) }}/s</template></ElTableColumn>
        <template #empty><EmptyState title="没有匹配的服务" description="导入抓包或选择混合示例；缺少 SD 时仍会显示观察到的服务 ID。" /></template>
      </ElTable>
      <ElTable v-else-if="tab === 'members'" :data="members" row-key="key" stripe max-height="440">
        <ElTableColumn label="方法 / 事件" min-width="250"><template #default="{ row }"><strong>{{ row.name || hexID(row.method_id) }}</strong><div class="sub mono">Service {{ hexID(row.service_id) }} / Instance {{ hexID(row.instance_id) }} / {{ hexID(row.method_id) }}</div></template></ElTableColumn>
        <ElTableColumn label="服务提供方" min-width="155"><template #default="{ row }"><span class="mono">{{ providerOf(row.service_key) }}</span></template></ElTableColumn>
        <ElTableColumn prop="transport" label="传输" width="80" />
        <ElTableColumn label="类型" width="95"><template #default="{ row }"><ElTag size="small" :type="row.kind === 'event' ? 'success' : 'primary'">{{ row.kind === 'event' ? '事件' : '方法' }}</ElTag></template></ElTableColumn>
        <ElTableColumn prop="requests" label="请求" sortable width="100" /><ElTableColumn prop="responses" label="响应" sortable width="100" /><ElTableColumn prop="notifications" label="通知" sortable width="100" /><ElTableColumn prop="errors" label="错误" sortable width="100" /><ElTableColumn prop="messages" label="总消息" sortable min-width="100" />
        <template #empty><EmptyState title="没有方法或事件记录" description="收到非分段的 SOME/IP 用户消息后显示。" /></template>
      </ElTable>
      <ElTable v-else-if="tab === 'discovery'" :data="discovery" row-key="id" stripe max-height="440">
        <ElTableColumn label="时间 / 类型" min-width="220"><template #default="{ row }"><strong>{{ row.kind }}</strong><div class="sub">{{ time(row.time) }} · 包 {{ row.packet }}</div></template></ElTableColumn>
        <ElTableColumn label="服务 / 实例 / 事件组" min-width="245"><template #default="{ row }">{{ row.service_name || hexID(row.service_id) }}<div class="sub mono">{{ hexID(row.service_id) }} / {{ hexID(row.instance_id) }}</div><div v-if="row.eventgroup_id != null" class="sub">事件组 {{ hexID(row.eventgroup_id) }} {{ row.eventgroup_name }} · Counter {{ row.counter }}</div></template></ElTableColumn>
        <ElTableColumn prop="ttl" label="TTL 秒" width="85" /><ElTableColumn label="来源 / 目标" min-width="200"><template #default="{ row }"><span class="mono">{{ row.source }}<br>→ {{ row.destination }}</span></template></ElTableColumn>
        <ElTableColumn label="端点 / 标志" min-width="220"><template #default="{ row }"><span class="mono">{{ endpointText(row.endpoints) }}</span><div class="sub">Session {{ hexID(row.session_id) }} · Reboot {{ row.reboot ? 1 : 0 }} · Unicast {{ row.unicast ? 1 : 0 }}</div></template></ElTableColumn>
        <template #empty><EmptyState title="没有发现记录" description="等待 SOME/IP-SD Offer、Find 或事件组订阅报文。" /></template>
      </ElTable>
      <div v-else class="diagnostics"><ElAlert v-for="item in diagnostics" :key="item.key" :title="`${item.text} · ${item.count} 次`" type="warning" show-icon :closable="false" /><EmptyState v-if="!diagnostics.length" title="暂无观察提示" description="无提示不代表捕获范围完整。缺失字节不会拼接；SOME/IP-TP 和业务字段解码尚未支持。" /></div>
    </div>
    <div class="table-foot"><span>TCP / UDP 消息观察 · 不推断未捕获的调用结果</span><span>过期表示公告 TTL 到期，不直接判断设备离线</span></div>
  </ElCard>
  <div class="someip-transport-bar"><label for="someip-transport">消息传输协议</label><ElSelect id="someip-transport" v-model="transport" aria-label="消息传输协议" placeholder="全部传输" class="transport-select"><ElOption value="" label="全部传输" /><ElOption value="TCP" label="TCP" /><ElOption value="UDP" label="UDP" /></ElSelect><span>全会话：TCP {{ state.tcp_messages || 0 }} 条 · UDP {{ state.udp_messages || 0 }} 条</span></div>
  <ElCard class="workspace" shadow="never">
    <template #header><div class="section-bar"><div><h2>最近 SOME/IP 消息 <ElTag size="small" :closable="Boolean(filterKey)" @close="filterKey = ''">{{ filter ? `${filter.name || hexID(filter.service_id)} / ${hexID(filter.instance_id)} · ${filter.provider}` : provider || '全部服务' }}</ElTag></h2><p>最近 256 条 · TCP 重传不重复计数 · 原始载荷最多 16 KiB</p></div><ElButton :icon="paused ? VideoPlay : VideoPause" @click="emit('pause')">{{ paused ? '恢复刷新' : '暂停刷新' }}</ElButton></div></template>
    <ElTable id="someip-messages" :data="visibleMessages" stripe row-key="id" @row-click="(row: SomeIPSample) => inspect(row.id)">
      <ElTableColumn label="时间" width="105"><template #default="{ row }">{{ time(row.time) }}</template></ElTableColumn>
      <ElTableColumn label="服务 / 方法" min-width="225"><template #default="{ row }">{{ row.service_name || hexID(row.service_id) }} / {{ row.method_name || hexID(row.method_id) }}<div class="sub mono">{{ hexID(row.service_id) }} / {{ hexID(row.method_id) }} · Instance {{ hexID(row.instance_id) }}</div></template></ElTableColumn>
      <ElTableColumn prop="transport" label="传输" width="80" />
      <ElTableColumn label="消息类型" width="165"><template #default="{ row }"><ElTag size="small" :type="row.return_code || row.message_type === 'ERROR' ? 'danger' : row.message_type === 'NOTIFICATION' ? 'success' : 'info'">{{ row.message_type }}</ElTag></template></ElTableColumn>
      <ElTableColumn label="服务提供方" min-width="155"><template #default="{ row }"><span class="mono">{{ providerOf(row.service_key) }}</span></template></ElTableColumn>
      <ElTableColumn label="来源 → 目标" min-width="190"><template #default="{ row }"><span class="mono">{{ row.source }}<br>→ {{ row.destination }}</span></template></ElTableColumn>
      <ElTableColumn label="Session / 返回码" width="140"><template #default="{ row }"><span class="mono">{{ hexID(row.session_id) }} / {{ hexID(row.return_code, 2) }}</span></template></ElTableColumn>
      <ElTableColumn label="载荷" width="100"><template #default="{ row }">{{ bytes(row.size) }}{{ row.truncated ? ' 截断' : '' }}</template></ElTableColumn>
      <ElTableColumn label="操作" width="80" fixed="right"><template #default="{ row }"><ElButton type="primary" link :aria-label="`查看 SOME/IP 消息 ${row.id}`" @click.stop="inspect(row.id)">详情</ElButton></template></ElTableColumn>
      <template #empty><EmptyState title="暂无 SOME/IP 消息" description="SD 公告与 SOME/IP-TP 分段不计入完整用户消息。" /></template>
    </ElTable>
    <div class="table-foot"><span>{{ paused ? '界面已暂停，后台采集不受影响' : '每秒刷新 · 可暂停后检查' }}</span><ElPagination v-model:current-page="page" v-model:page-size="pageSize" :total="messages.length" :page-sizes="[10, 20, 50]" layout="total, sizes, prev, pager, next" size="small" background /></div>
  </ElCard>
  <ElDialog :model-value="Boolean(detail)" title="SOME/IP 消息详情" class="sample-dialog" width="820px" align-center destroy-on-close @update:model-value="value => { if (!value) closeDetail() }">
    <template v-if="detail"><ElDescriptions :column="1" size="small" border>
      <ElDescriptionsItem label="服务 / 实例">{{ detail.sample.service_name }} {{ hexID(detail.sample.service_id) }} / {{ hexID(detail.sample.instance_id) }}</ElDescriptionsItem>
      <ElDescriptionsItem label="服务提供方">{{ providerOf(detail.sample.service_key) }}{{ serviceIndex.get(detail.sample.service_key)?.instance_id == null ? '（按通信方向识别服务端，实例未知）' : '（已关联 SD 服务实例）' }}</ElDescriptionsItem>
      <ElDescriptionsItem label="方法 / 事件">{{ detail.sample.method_name }} {{ hexID(detail.sample.method_id) }}</ElDescriptionsItem>
      <ElDescriptionsItem label="Client / Session">{{ hexID(detail.sample.client_id) }} / {{ hexID(detail.sample.session_id) }}</ElDescriptionsItem>
      <ElDescriptionsItem label="类型 / 返回码">{{ detail.sample.message_type }} / {{ hexID(detail.sample.return_code, 2) }}</ElDescriptionsItem>
      <ElDescriptionsItem label="接口主版本">{{ detail.sample.interface_version }}</ElDescriptionsItem>
      <ElDescriptionsItem label="传输方式">{{ detail.sample.transport }}{{ detail.sample.transport === 'TCP' ? ' · 流重组后的完整消息，时间及包号对应完成重组的捕获包' : '' }}</ElDescriptionsItem>
      <ElDescriptionsItem label="通信方向">{{ detail.sample.source }} → {{ detail.sample.destination }}</ElDescriptionsItem>
      <ElDescriptionsItem label="捕获时间 / 包">{{ detail.sample.time }} / {{ detail.sample.packet }}</ElDescriptionsItem>
      <ElDescriptionsItem label="识别依据">{{ detail.sample.detection === 'header_heuristic' ? '报文头结构匹配（推测）' : '配置端口或有效 SD 端点' }}</ElDescriptionsItem>
      <ElDescriptionsItem label="载荷大小">{{ bytes(detail.sample.size) }}{{ detail.sample.truncated ? '（预览截断为 16 KiB）' : '' }}</ElDescriptionsItem>
    </ElDescriptions><ElAlert title="原始载荷预览，尚未按 ARXML 等类型描述解码业务字段。" type="info" show-icon :closable="false" class="payload-note" /><ElTabs v-model="encoding"><ElTabPane label="十六进制" name="hex" /><ElTabPane label="Base64" name="base64" /></ElTabs><pre id="someip-hex" class="payload">{{ payload || '空载荷' }}</pre></template>
    <template #footer><ElButton @click="closeDetail">关闭</ElButton><ElButton type="primary" :icon="CopyDocument" @click="copy">{{ copied ? '已复制' : '复制载荷' }}</ElButton></template>
  </ElDialog>
</template>
