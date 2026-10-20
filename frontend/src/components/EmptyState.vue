<script setup lang="ts">
import { ElEmpty } from 'element-plus'
defineProps<{ title: string; description: string }>()
</script>
<template>
  <ElEmpty :description="title" :image-size="68"><p class="empty-description">{{ description }}</p></ElEmpty>
</template>
