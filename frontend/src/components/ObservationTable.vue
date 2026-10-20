<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElAlert, ElButton, ElCard, ElInput, ElTable, ElTableColumn, ElTag } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import type { Observation, Tab, Topic } from '../types'
import { bytes, number, time, warnings } from '../format'
import EmptyState from './EmptyState.vue'

const props = defineProps<{ state: Observation | null; tab: Tab; selectedKey?: string }>()
const emit = defineEmits<{ select: [topic: Topic] }>()
const search = ref('')
watch(() => props.tab, () => { search.value = '' })
const titles: Record<Tab, [string, string]> = {
  topics: ['Topic 总览', '选择 Topic 筛选样本 · 点击列标题排序'],
  participants: ['参与者', '从 RTPS 头与 SPDP 公告识别 · 不等同于 ROS 节点'],
  endpoints: ['读写端点', '已观察端点的公告属性 · 不代表已经匹配'],
  diagnostics: ['观察提示', '只描述本观察点证据 · 不直接判断网络丢包或业务故障'],
}
const matches = (...values: string[]) => values.join(' ').toLowerCase().includes(search.value.trim().toLowerCase())
const topics = computed(() => (props.state?.topics || []).filter(t => matches(t.name, t.type)))
const participants = computed(() => (props.state?.participants || []).filter(p => matches(p.guid, p.name, p.source)))
const endpoints = computed(() => (props.state?.endpoints || []).filter(e => matches(e.guid, e.topic, e.type)))
const diagnostics = computed(() => Object.entries(props.state?.warnings || {}).filter(([key]) => matches(key, ...(warnings[key] || []))))
const count = computed(() => ({ topics: topics.value.length, participants: participants.value.length, endpoints: endpoints.value.length, diagnostics: diagnostics.value.length })[props.tab])
</script>

<template>
  <ElCard class="workspace" shadow="never">
    <template #header><div class="section-bar"><div><h2 id="tableTitle">{{ titles[tab][0] }} <ElTag size="small" type="info" round>{{ count }}</ElTag></h2><p>{{ titles[tab][1] }}</p></div><ElInput v-model="search" clearable :prefix-icon="Search" placeholder="搜索名称、类型或 GUID" aria-label="筛选列表" class="table-search" /></div></template>
    <div id="table">
      <ElTable v-if="tab === 'topics'" :data="topics" row-key="key" stripe max-height="420" :row-class-name="({ row }: { row: Topic }) => selectedKey === row.key ? 'selected-topic' : ''" @row-click="(row: Topic) => emit('select', row)">
        <ElTableColumn prop="name" label="Topic / 类型" min-width="330" sortable><template #default="{ row }"><ElButton class="topic-link" link type="primary" @click.stop="emit('select', row as Topic)">{{ row.name }}</ElButton><div class="sub mono">Domain {{ row.domain }} · {{ row.type || '类型未知' }}</div><div v-if="row.ros_name" class="sub">ROS 名称推测 {{ row.ros_name }}</div></template></ElTableColumn>
        <ElTableColumn label="发布 / 订阅" width="136"><template #default="{ row }"><ElTag size="small" type="success">W {{ row.writers }}</ElTag> <ElTag size="small" type="info">R {{ row.readers }}</ElTag></template></ElTableColumn>
        <ElTableColumn prop="samples" label="样本" width="100" sortable align="right" />
        <ElTableColumn prop="hz" label="频率 · 5 秒" width="132" sortable align="right"><template #default="{ row }">{{ row.hz.toFixed(1) }} Hz</template></ElTableColumn>
        <ElTableColumn prop="bytes_per_second" label="载荷速率" width="126" sortable align="right"><template #default="{ row }">{{ bytes(row.bytes_per_second) }}/s</template></ElTableColumn>
        <ElTableColumn prop="last_seen" label="最近出现" width="125" sortable><template #default="{ row }"><span class="mono">{{ time(row.last_seen) }}</span></template></ElTableColumn>
        <template #empty><EmptyState :title="search ? '没有符合条件的 Topic' : '尚未发现 Topic'" :description="search ? '换一个关键词，或清空搜索查看全部记录。' : state?.rtps_packets ? '已观察到 RTPS，可能缺少端点公告。' : '开始采集、导入 PCAP，或试用合成示例。'" /></template>
      </ElTable>
      <ElTable v-else-if="tab === 'participants'" :data="participants" row-key="guid" stripe max-height="420">
        <ElTableColumn prop="guid" label="GUID / 公告名称" min-width="310" sortable><template #default="{ row }"><span class="mono">{{ row.guid }}</span><div class="sub">{{ row.name || '未提供名称' }}</div></template></ElTableColumn>
        <ElTableColumn prop="vendor" label="实现标识" min-width="155" show-overflow-tooltip />
        <ElTableColumn prop="source" label="源地址" min-width="180" show-overflow-tooltip />
        <ElTableColumn prop="domain" label="Domain" width="100" sortable><template #default="{ row }">{{ row.domain ?? '未知' }}</template></ElTableColumn>
        <ElTableColumn label="发现依据" width="135"><template #default="{ row }"><ElTag size="small" :type="row.discovery_observed ? 'success' : 'info'">{{ row.discovery_observed ? 'SPDP 公告' : '仅 RTPS 头' }}</ElTag></template></ElTableColumn>
        <ElTableColumn prop="last_seen" label="最近出现" width="125" sortable><template #default="{ row }">{{ time(row.last_seen) }}</template></ElTableColumn>
        <template #empty><EmptyState title="没有参与者记录" description="等待 SPDP 公告或可识别的 RTPS 数据。" /></template>
      </ElTable>
      <ElTable v-else-if="tab === 'endpoints'" :data="endpoints" row-key="guid" stripe max-height="420">
        <ElTableColumn prop="guid" label="端点 GUID" min-width="300" show-overflow-tooltip />
        <ElTableColumn prop="kind" label="方向" width="105" sortable><template #default="{ row }"><ElTag :type="row.kind === 'writer' ? 'success' : 'info'" size="small">{{ row.kind }}</ElTag></template></ElTableColumn>
        <ElTableColumn prop="topic" label="Topic" min-width="250" sortable show-overflow-tooltip />
        <ElTableColumn prop="reliability" label="Reliability" width="130" />
        <ElTableColumn prop="durability" label="Durability" width="140" />
        <ElTableColumn prop="samples" label="样本" width="100" sortable />
        <template #empty><EmptyState title="没有端点记录" description="SEDP 公告可以提供 Topic、类型和部分 QoS 信息。" /></template>
      </ElTable>
      <div v-else class="diagnostics">
        <ElAlert v-for="[key, value] in diagnostics" :key="key" :title="`${warnings[key]?.[0] || key} · ${number(value)} 次`" :description="warnings[key]?.[1] || '未支持的协议特性'" type="warning" show-icon :closable="false" />
        <EmptyState v-if="!diagnostics.length" title="暂无协议或采集提示" description="没有提示不代表已观察到完整网络，也不代表所有订阅端已收到数据。" />
      </div>
    </div>
    <div class="table-foot"><span>{{ count }} 条记录</span><span>类型名称相同不代表结构已验证</span></div>
  </ElCard>
</template>
