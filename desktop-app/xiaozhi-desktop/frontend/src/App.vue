<script setup>
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import {
  GetServiceStatus,
  GetServiceHealth,
  GetLogs,
  Greet,
  GetSystemInfo,
  OpenManagerWeb
} from '../wailsjs/go/main/App'

const defaultServiceKeys = ['backend', 'manager', 'redis']
const serviceOrder = [...defaultServiceKeys, 'speech']
const serviceKeys = ref([...defaultServiceKeys])
const serviceMetaMap = {
  backend: {
    label: 'Backend 服务',
    description: '承载 LLM、ASR、TTS 等核心推理能力'
  },
  manager: {
    label: 'Manager 服务',
    description: '提供控制台 API 及静态资源服务'
  },
  redis: {
    label: 'Redis 服务',
    description: '缓存事件与消息队列的轻量依赖'
  },
  speech: {
    label: 'Speech 服务',
    description: '实时语音/VAD/TTS 推理入口'
  }
}

const serviceStatus = ref(buildStatusShape(serviceKeys.value))
const serviceHealth = ref({})
const message = ref('')
const loading = ref(false)
const systemInfo = ref({})
const selectedService = ref('backend')
const logs = ref('')
const showConsole = ref(false)
const consoleFrameKey = ref(0)
const managerConsoleURL = ref('')
const logsContainer = ref(null)
const lastStatusUpdated = ref('')
const lastLogUpdated = ref('')

const STATUS_POLL_INTERVAL = 5000
const LOG_POLL_INTERVAL = 3000
const statusIntervalSeconds = STATUS_POLL_INTERVAL / 1000
const logIntervalSeconds = LOG_POLL_INTERVAL / 1000

let statusTimer = null
let logTimer = null
let statusRefreshing = false
let logRequestToken = 0

const services = computed(() =>
  serviceKeys.value.map((key) => ({
    key,
    label: serviceMetaMap[key]?.label || `${key} 服务`,
    description: serviceMetaMap[key]?.description || '运行时托管服务'
  }))
)

function normalizeServiceKeys(keys) {
  const list = Array.isArray(keys) ? keys.filter(Boolean) : []
  const base = list.length ? list : defaultServiceKeys
  const seen = new Set()
  const ordered = []

  base.forEach((key) => {
    if (!seen.has(key)) {
      seen.add(key)
      ordered.push(key)
    }
  })

  ordered.sort((a, b) => {
    const ia = serviceOrder.indexOf(a)
    const ib = serviceOrder.indexOf(b)
    if (ia === -1 && ib === -1) {
      return a.localeCompare(b)
    }
    if (ia === -1) return 1
    if (ib === -1) return -1
    return ia - ib
  })

  return ordered
}

function buildStatusShape(keys, source = {}) {
  const shape = {}
  keys.forEach((key) => {
    shape[key] = Boolean(source?.[key])
  })
  return shape
}

function applyServiceKeys(keys, incomingStatus) {
  const nextKeys = normalizeServiceKeys(keys)
  const changed =
    nextKeys.length !== serviceKeys.value.length ||
    nextKeys.some((key, index) => key !== serviceKeys.value[index])

  if (changed) {
    serviceKeys.value = nextKeys
  }

  if (incomingStatus) {
    serviceStatus.value = buildStatusShape(nextKeys, incomingStatus)
  } else {
    serviceStatus.value = buildStatusShape(nextKeys, serviceStatus.value)
  }

  if (nextKeys.length && !nextKeys.includes(selectedService.value)) {
    selectedService.value = nextKeys[0]
  }
}

const allServicesRunning = computed(() =>
  Object.values(serviceStatus.value).every((status) => Boolean(status))
)

const autoStartHint = computed(() =>
  allServicesRunning.value
    ? '所有服务均已就绪，可直接进入控制台。'
    : '应用正在自动启动全部服务，请耐心等待...'
)

const detailRows = (serviceKey) => {
  const rows = []
  const health = serviceHealth.value[serviceKey] || {}

  rows.push({
    label: '运行状态',
    value: serviceStatus.value[serviceKey] ? '运行中' : '启动中'
  })

  if (health.uptime) {
    rows.push({ label: '运行时长', value: health.uptime })
  }

  if (serviceKey === 'backend' && health.redis_target) {
    rows.push({ label: 'Redis', value: health.redis_target })
  }

  if (serviceKey === 'manager' && managerConsoleURL.value) {
    rows.push({ label: '控制台', value: managerConsoleURL.value })
  }

  if (serviceKey === 'redis' && health.address) {
    rows.push({ label: '地址', value: health.address })
  }

  if (serviceKey === 'speech' && health.address) {
    rows.push({ label: '地址', value: health.address })
  }

  return rows
}

const fetchServiceStatus = async () => {
  try {
    const status = await GetServiceStatus()
    const keys = Object.keys(status || {})
    applyServiceKeys(keys.length ? keys : serviceKeys.value, status || {})
  } catch (error) {
    console.error('获取服务状态失败:', error)
  }
}

const fetchServiceHealth = async () => {
  try {
    const health = await GetServiceHealth()
    const keys = Object.keys(health || {})
    if (keys.length) {
      applyServiceKeys(keys)
    }
    serviceHealth.value = health || {}
    lastStatusUpdated.value = new Date().toLocaleTimeString()
  } catch (error) {
    console.error('获取服务健康状态失败:', error)
  }
}

const refreshStatus = async () => {
  if (statusRefreshing) {
    return
  }
  statusRefreshing = true
  try {
    await Promise.all([fetchServiceStatus(), fetchServiceHealth()])
  } finally {
    statusRefreshing = false
  }
}

const fetchLogs = async (serviceName) => {
  if (!serviceName) {
    logs.value = '请选择要查看的服务日志'
    return
  }
  const token = ++logRequestToken
  try {
    const logContent = await GetLogs(serviceName)
    if (token !== logRequestToken) {
      return
    }
    logs.value = logContent
    lastLogUpdated.value = new Date().toLocaleTimeString()
  } catch (error) {
    if (token !== logRequestToken) {
      return
    }
    logs.value = `获取日志失败: ${error}`
    lastLogUpdated.value = new Date().toLocaleTimeString()
  }
}

const sendGreeting = async () => {
  try {
    const result = await Greet('用户')
    message.value = result
  } catch (error) {
    message.value = `问候失败: ${error}`
  }
}

const fetchSystemInfo = async () => {
  try {
    const info = await GetSystemInfo()
    systemInfo.value = info
    managerConsoleURL.value = info.manager_console_url || info.manager_url || ''
    if (info.services) {
      const keys = Object.keys(info.services)
      if (keys.length) {
        applyServiceKeys(keys)
      }
    }
  } catch (error) {
    console.error('获取系统信息失败:', error)
  }
}

const refreshAll = async () => {
  loading.value = true
  try {
    await Promise.all([refreshStatus(), fetchSystemInfo()])
  } catch (error) {
    message.value = `刷新失败: ${error}`
  } finally {
    loading.value = false
  }
}

const openManagerConsoleInBrowser = async () => {
  loading.value = true
  try {
    const result = await OpenManagerWeb()
    message.value = result
  } catch (error) {
    message.value = `打开管理控制台失败: ${error}`
  } finally {
    loading.value = false
  }
}

const openConsoleInsideApp = () => {
  if (!managerConsoleURL.value) {
    message.value = '未能解析管理控制台地址'
    return
  }
  showConsole.value = true
}

const exitConsoleView = () => {
  showConsole.value = false
}

const reloadConsole = () => {
  consoleFrameKey.value += 1
}

const startStatusPolling = () => {
  stopStatusPolling()
  statusTimer = setInterval(() => {
    refreshStatus().catch((error) => {
      console.error('自动刷新服务状态失败:', error)
    })
  }, STATUS_POLL_INTERVAL)
}

const stopStatusPolling = () => {
  if (statusTimer) {
    clearInterval(statusTimer)
    statusTimer = null
  }
}

const startLogPolling = () => {
  stopLogPolling()
  if (!selectedService.value) {
    return
  }
  fetchLogs(selectedService.value)
  logTimer = setInterval(() => {
    fetchLogs(selectedService.value)
  }, LOG_POLL_INTERVAL)
}

const stopLogPolling = () => {
  if (logTimer) {
    clearInterval(logTimer)
    logTimer = null
  }
}

watch(selectedService, () => {
  startLogPolling()
})

watch(logs, () => {
  nextTick(() => {
    if (logsContainer.value) {
      logsContainer.value.scrollTop = logsContainer.value.scrollHeight
    }
  })
})

onMounted(() => {
  refreshAll().catch((error) => {
    console.error('初始化状态刷新失败:', error)
  })
  sendGreeting()

  startStatusPolling()
  startLogPolling()
})

onUnmounted(() => {
  stopStatusPolling()
  stopLogPolling()
})
</script>

<template>
  <div class="app" v-if="!showConsole">
    <header class="header">
      <div>
        <h1>小智桌面端</h1>
        <p>开箱即用的一体化运行时，自动启动所有本地服务。</p>
        <p class="integration-info">{{ autoStartHint }}</p>
      </div>
      <div class="header-meta">
        <div class="meta-item">
          <span>当前版本</span>
          <strong>{{ systemInfo.version || '1.0.0' }}</strong>
        </div>
        <div class="meta-item">
          <span>Go / Wails</span>
          <strong>{{ systemInfo.go_version }} / {{ systemInfo.wails_version }}</strong>
        </div>
        <div class="meta-item" v-if="systemInfo.build_time && systemInfo.build_time !== 'unknown'">
          <span>编译时间</span>
          <strong>{{ systemInfo.build_time }}</strong>
        </div>
      </div>
    </header>

    <main class="main">
      <section class="status-section">
        <div class="section-head">
          <div>
            <h2>服务总览</h2>
            <p>核心服务会在应用启动后自动运行。</p>
          </div>
          <div class="auto-refresh-indicator">
            <span>状态每 {{ statusIntervalSeconds }} 秒自动刷新</span>
            <span v-if="lastStatusUpdated">上次更新 {{ lastStatusUpdated }}</span>
          </div>
        </div>
        <div class="service-grid">
          <div
            v-for="service in services"
            :key="service.key"
            class="service-card"
            :class="{ active: serviceStatus[service.key] }"
          >
            <div class="service-card-header">
              <h3>{{ service.label }}</h3>
              <span class="badge" :class="{ online: serviceStatus[service.key] }">
                {{ serviceStatus[service.key] ? '运行中' : '启动中' }}
              </span>
            </div>
            <p class="service-desc">{{ service.description }}</p>
            <div class="health-info">
              <div
                class="health-item"
                v-for="row in detailRows(service.key)"
                :key="row.label"
              >
                <span class="label">{{ row.label }}</span>
                <span class="value">{{ row.value || '检测中' }}</span>
              </div>
            </div>
          </div>
        </div>
      </section>

      <section class="console-entry">
        <div>
          <h2>进入 manager-console</h2>
          <p>自动启动完成后，可直接在桌面应用内使用 React 控制台。</p>
        </div>
        <div class="entry-actions">
          <button class="btn btn-primary" @click="openConsoleInsideApp" :disabled="!managerConsoleURL">
            在应用内打开
          </button>
          <button class="btn btn-secondary" @click="openManagerConsoleInBrowser" :disabled="loading">
            浏览器打开
          </button>
        </div>
      </section>

      <section class="logs-section">
        <div class="section-head">
          <h2>服务日志</h2>
          <div class="logs-controls">
            <select v-model="selectedService" class="service-select">
              <option v-for="service in services" :key="service.key" :value="service.key">
                {{ service.label }}
              </option>
            </select>
            <div class="log-refresh-indicator">
              <span>日志每 {{ logIntervalSeconds }} 秒追踪</span>
              <span v-if="lastLogUpdated">更新于 {{ lastLogUpdated }}</span>
            </div>
          </div>
        </div>
        <div class="logs-content" ref="logsContainer">
          <pre>{{ logs || '日志实时刷新中...' }}</pre>
        </div>
      </section>

      <section class="message-section">
        <h2>系统消息</h2>
        <div class="message-box">
          <p v-if="message">{{ message }}</p>
          <p v-else>后台自动维护所有服务，您可以随时进入控制台。</p>
        </div>
      </section>

      <section class="system-info-section">
        <h2>系统信息</h2>
        <div class="system-info-grid">
          <div class="info-card">
            <h4>运行环境</h4>
            <p><strong>应用:</strong> {{ systemInfo.app_name }}</p>
            <p><strong>集成:</strong> {{ systemInfo.integration }}</p>
            <p><strong>工作目录:</strong> {{ systemInfo.work_dir }}</p>
            <p v-if="systemInfo.build_time && systemInfo.build_time !== 'unknown'">
              <strong>编译时间:</strong> {{ systemInfo.build_time }}
            </p>
          </div>
          <div class="info-card">
            <h4>服务开关</h4>
            <div
              class="service-config"
              v-for="(enabled, service) in systemInfo.services"
              :key="service"
            >
              <span class="service-name">{{ serviceMetaMap[service]?.label || service }}</span>
              <span class="service-status" :class="{ enabled }">
                {{ enabled ? '已启用' : '已禁用' }}
              </span>
            </div>
          </div>
        </div>
      </section>
    </main>
  </div>

  <div class="console-view" v-else>
    <div class="console-toolbar">
      <div>
        <h2>manager-console</h2>
        <p>所有服务已在后台运行，专注于管理控制台即可。</p>
      </div>
      <div class="toolbar-actions">
        <button class="btn btn-secondary" @click="reloadConsole">
          刷新视图
        </button>
        <button class="btn btn-secondary" @click="openManagerConsoleInBrowser" :disabled="loading">
          浏览器打开
        </button>
        <button class="btn btn-stop" @click="exitConsoleView">
          返回服务面板
        </button>
      </div>
    </div>
    <iframe
      class="console-iframe"
      :key="consoleFrameKey"
      :src="managerConsoleURL"
      title="manager-console"
    ></iframe>
  </div>
</template>

<style>
* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

body {
  font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
}

.app,
.console-view {
  min-height: 100vh;
  background: #0f172a;
  color: #f8fafc;
}

.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 2.5rem 3rem 1.5rem;
  background: linear-gradient(135deg, rgba(15, 23, 42, 0.95), rgba(30, 64, 175, 0.85));
}

.header h1 {
  font-size: 2.5rem;
  margin-bottom: 0.5rem;
}

.header p {
  font-size: 1rem;
  opacity: 0.85;
}

.header-meta {
  display: flex;
  gap: 1.5rem;
}

.meta-item {
  text-align: right;
}

.meta-item span {
  display: block;
  font-size: 0.85rem;
  opacity: 0.75;
}

.meta-item strong {
  font-size: 1.4rem;
}

.integration-info {
  margin-top: 0.5rem;
  color: #4ade80;
  font-weight: 600;
}

.main {
  padding: 2rem 3rem 3rem;
}

.section-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 1rem;
}

.auto-refresh-indicator {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 0.25rem;
  font-size: 0.85rem;
  color: rgba(248, 250, 252, 0.75);
}

.auto-refresh-indicator span:last-child {
  color: rgba(148, 163, 184, 0.9);
  font-size: 0.8rem;
}

.section-head h2 {
  font-size: 1.5rem;
  margin-bottom: 0.3rem;
}

.service-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 1.5rem;
}

.service-card {
  background: rgba(15, 23, 42, 0.65);
  border-radius: 1rem;
  padding: 1.5rem;
  border: 1px solid rgba(255, 255, 255, 0.08);
  backdrop-filter: blur(6px);
}

.service-card.active {
  border-color: #22c55e;
  box-shadow: 0 8px 30px rgba(34, 197, 94, 0.15);
}

.service-card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 0.5rem;
}

.service-desc {
  font-size: 0.9rem;
  opacity: 0.75;
  min-height: 48px;
}

.badge {
  padding: 0.2rem 0.8rem;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.15);
  font-size: 0.85rem;
}

.badge.online {
  background: rgba(34, 197, 94, 0.15);
  color: #4ade80;
}

.health-info {
  margin-top: 1rem;
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}

.health-item {
  display: flex;
  justify-content: space-between;
  font-size: 0.85rem;
  padding-bottom: 0.2rem;
  border-bottom: 1px dashed rgba(255, 255, 255, 0.1);
}

.health-item:last-child {
  border-bottom: none;
}

.health-item .label {
  opacity: 0.7;
}

.console-entry {
  margin: 2rem 0;
  padding: 1.5rem;
  border-radius: 1rem;
  background: rgba(30, 64, 175, 0.25);
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.entry-actions {
  display: flex;
  gap: 1rem;
}

.logs-section,
.message-section,
.system-info-section {
  margin-bottom: 2rem;
}

.logs-controls {
  display: flex;
  gap: 1rem;
  align-items: center;
  justify-content: space-between;
}

.log-refresh-indicator {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 0.25rem;
  font-size: 0.85rem;
  color: rgba(248, 250, 252, 0.75);
}

.log-refresh-indicator span:last-child {
  color: rgba(148, 163, 184, 0.9);
  font-size: 0.8rem;
}

.service-select {
  padding: 0.4rem 0.8rem;
  border-radius: 0.5rem;
  border: none;
}

.logs-content {
  margin-top: 1rem;
  background: rgba(15, 23, 42, 0.8);
  border-radius: 1rem;
  padding: 1rem;
  min-height: 200px;
  max-height: 320px;
  overflow-y: auto;
  border: 1px solid rgba(255, 255, 255, 0.08);
}

.logs-content pre {
  white-space: pre-wrap;
  word-break: break-all;
  font-family: 'JetBrains Mono', Consolas, monospace;
  margin: 0;
}

.message-box {
  background: rgba(15, 23, 42, 0.8);
  border-radius: 1rem;
  padding: 1rem;
  border: 1px solid rgba(255, 255, 255, 0.08);
  min-height: 80px;
}

.system-info-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 1.5rem;
}

.info-card {
  background: rgba(15, 23, 42, 0.7);
  border-radius: 1rem;
  padding: 1.2rem;
  border: 1px solid rgba(255, 255, 255, 0.08);
}

.service-config {
  display: flex;
  justify-content: space-between;
  margin-bottom: 0.5rem;
}

.service-status {
  padding: 0.1rem 0.6rem;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.1);
}

.service-status.enabled {
  background: rgba(34, 197, 94, 0.15);
  color: #4ade80;
}

.btn {
  border: none;
  border-radius: 0.8rem;
  padding: 0.6rem 1.4rem;
  font-size: 0.95rem;
  cursor: pointer;
  color: #f8fafc;
  transition: opacity 0.2s ease, transform 0.2s ease;
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.btn-primary {
  background: #2563eb;
}

.btn-secondary {
  background: rgba(148, 163, 184, 0.2);
}

.btn-refresh {
  background: rgba(148, 163, 184, 0.2);
}

.btn-logs {
  background: rgba(14, 165, 233, 0.2);
}

.btn-stop {
  background: rgba(239, 68, 68, 0.85);
}

.btn:hover:not(:disabled) {
  opacity: 0.85;
  transform: translateY(-1px);
}

.console-view {
  display: flex;
  flex-direction: column;
}

.console-toolbar {
  padding: 1rem 1.5rem;
  background: rgba(15, 23, 42, 0.95);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
}

.toolbar-actions {
  display: flex;
  gap: 0.8rem;
}

.console-iframe {
  flex: 1;
  border: none;
  width: 100%;
  min-height: calc(100vh - 120px);
  background: #fff;
}

@media (max-width: 900px) {
  .header {
    flex-direction: column;
    align-items: flex-start;
    gap: 1rem;
  }

  .header-meta {
    width: 100%;
    justify-content: space-between;
  }

  .console-entry {
    flex-direction: column;
    align-items: flex-start;
    gap: 1rem;
  }

  .entry-actions {
    width: 100%;
    flex-direction: column;
  }

  .console-toolbar {
    flex-direction: column;
    align-items: flex-start;
  }

  .toolbar-actions {
    width: 100%;
    flex-wrap: wrap;
  }
}
</style>
