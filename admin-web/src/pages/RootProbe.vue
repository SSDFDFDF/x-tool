<script setup lang="ts">
import EndpointProbePage from '../components/EndpointProbePage.vue'
</script>

<template>
  <EndpointProbePage
    title="根路由探活"
    description="在管理台内直接请求服务根路由 `/`，用于确认实例是否可达，以及默认探活响应是否正常。"
    endpoint="/"
    note="该页面请求的是服务真实根路由，不是管理后台的 hash 路由。"
  />
</template>
