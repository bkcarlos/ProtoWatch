<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElButton, ElCard, ElPagination, ElTable, ElTableColumn, ElTag } from 'element-plus'
import { VideoPause, VideoPlay } from '@element-plus/icons-vue'
import type { Sample, Topic } from '../types'
import { bytes, short, time } from '../format'
import EmptyState from './EmptyState.vue'
const props = defineProps<{ samples: Sample[]; filter: Topic | null; paused: boolean; hasTopics: boolean }>()
const emit = defineEmits<{ select: [id: number]; clear: []; pause: [] }>()
const page = ref(1)
const pageSize = ref(10)
const rows = computed(() => props.samples.filter(sample => !props.filter || sample.topic_key === props.filter.key))
const visibleRows = computed(() => rows.value.slice((page.value - 1) * pageSize.value, page.value * pageSize.value))
watch([() => props.filter?.key, pageSize], () => { page.value = 1 })
watch(() => rows.value.length, total => { page.value = Math.min(page.value, Math.max(1, Math.ceil(total / pageSize.value))) })
</script>
<template>
  <ElCard class="workspace samples-section" shadow="never">
    <template #header><div class="section-bar"><div><h2>最近样本 <ElTag id="sampleFilter" size="small" :closable="Boolean(filter)" @close="emit('clear')">{{ filter?.name || '全部 Topic' }}</ElTag></h2><p>保留最近 256 条 · 载荷预览最多 16 KiB · 点击样本查看原始字节</p></div><ElButton :type="paused ? 'primary' : 'default'" :icon="paused ? VideoPlay : VideoPause" @click="emit('pause')">{{ paused ? '恢复刷新' : '暂停刷新' }}</ElButton></div></template>
    <ElTable id="samples" :data="visibleRows" row-key="id" stripe @row-click="(row: Sample) => emit('select', row.id)">
      <ElTableColumn prop="time" label="捕获时间" width="112"><template #default="{ row }"><span class="mono">{{ time(row.time) }}</span></template></ElTableColumn>
      <ElTableColumn prop="topic" label="Topic" min-width="240" show-overflow-tooltip><template #default="{ row }">{{ row.topic || '未知 Topic' }}</template></ElTableColumn>
      <ElTableColumn prop="writer" label="Writer GUID" min-width="185"><template #default="{ row }"><span class="mono" :title="row.writer">{{ short(row.writer) }}</span></template></ElTableColumn>
      <ElTableColumn prop="sequence" label="序列号" width="90" align="right" />
      <ElTableColumn label="载荷大小" width="132"><template #default="{ row }">{{ bytes(row.size) }}<ElTag v-if="row.truncated" size="small" type="warning">预览截断</ElTag></template></ElTableColumn>
      <ElTableColumn label="编码" width="115"><template #default="{ row }"><ElTag size="small" type="info">{{ row.encoding }}</ElTag></template></ElTableColumn>
      <ElTableColumn label="操作" width="90" fixed="right"><template #default="{ row }"><ElButton link type="primary" :aria-label="`查看样本 ${row.id}`" @click.stop="emit('select', row.id)">详情</ElButton></template></ElTableColumn>
      <template #empty><EmptyState title="暂无用户样本" :description="hasTopics ? '已发现实体，当前尚未捕获此范围的用户数据。' : '收到样本后可检查序列号、编码及原始载荷。'" /></template>
    </ElTable>
    <div class="table-foot"><span>{{ paused ? '界面刷新已暂停，后台采集不受影响' : '每秒刷新 · 可暂停后检查样本' }}</span><ElPagination v-model:current-page="page" v-model:page-size="pageSize" :page-sizes="[10, 20, 50]" :total="rows.length" layout="total, sizes, prev, pager, next" background size="small" /></div>
  </ElCard>
</template>
