<script setup lang="ts">
import EndpointProbePage from '../components/EndpointProbePage.vue'
</script>

<template>
  <EndpointProbePage
    title="模型 API 调试"
    description="在管理台内直接请求 `/v1/models`，用于检查当前代理对外暴露的模型列表，以及带客户端 Bearer Token 时的返回结果。"
    endpoint="/v1/models"
    :requires-bearer-token="true"
    token-placeholder="输入客户端 Bearer Token 后重试..."
    note="页面只在当前会话内暂存输入的 Bearer Token，刷新后不会保留。"
  />
</template>
