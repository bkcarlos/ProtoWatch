import { computed, ref, shallowRef } from 'vue'
import { bytes } from '../format'
import type { InterfacesResponse, NetworkInterface, Observation, SampleDetail, SomeIPDetail, ServerPCAPFile, ServerPCAPFiles } from '../types'

const message = (error: unknown) => error instanceof Error ? error.message : String(error)

// Explicit lifecycle keeps polling testable without mounting a component.
export function createWatchClient(fetcher: typeof fetch = fetch) {
  const state = shallowRef<Observation | null>(null)
  const interfaces = ref<NetworkInterface[]>([])
  const selectedInterface = ref('')
  const liveSupported = ref(false)
  const connected = ref(false)
  const notice = ref('')
  const paused = ref(false)
  const busy = ref(false)
  const uploading = ref(false)
  const serverFiles = shallowRef<ServerPCAPFile[]>([])
  const serverDirectory = ref('')
  const selectedServerFile = ref('')
  const filesLoading = ref(false)
  const filesError = ref('')
  const filesTruncated = ref(false)
  const maxImportBytes = ref(4 * 1024 ** 3)
  let uploadController: AbortController | undefined
  const running = computed(() => ['running', 'importing'].includes(state.value?.status || ''))
  let disposed = false
  let started = false
  let generation = 0
  let timer: ReturnType<typeof setTimeout> | undefined
  let pollRequest: AbortController | undefined
  const requests = new Set<AbortController>()

  async function request(path: string, init: RequestInit = {}, controller = new AbortController()) {
    if (disposed) throw new Error('观察页面已经关闭')
    requests.add(controller)
    const headers = new Headers(init.headers)
    try {
      const response = await fetcher(path, { ...init, headers, signal: controller.signal })
      if (!response.ok) {
        const body = await response.json().catch(() => ({ error: response.statusText })) as { error?: string }
        throw new Error(body.error || `请求失败 (${response.status})`)
      }
      return response
    } finally { requests.delete(controller) }
  }

  async function refresh(force = false) {
    if (disposed || (!force && ((paused.value || busy.value) && !uploading.value)) || pollRequest) return
    const currentGeneration = generation
    const controller = new AbortController()
    pollRequest = controller
    try {
      const response = await request('/api/state', {}, controller)
      const next = await response.json() as Observation
      if (!disposed && generation === currentGeneration) {
        state.value = next
        connected.value = true
        if (next.error) notice.value = next.error
      }
    } catch (error) {
      if (!disposed && generation === currentGeneration && !controller.signal.aborted) {
        connected.value = false
        notice.value = message(error)
      }
    } finally { if (pollRequest === controller) pollRequest = undefined }
  }

  function invalidatePoll() {
    generation++
    pollRequest?.abort()
    pollRequest = undefined
  }

  async function loadInterfaces() {
    try {
      const data = await (await request('/api/interfaces')).json() as InterfacesResponse
      if (disposed) return
      interfaces.value = data.interfaces
      liveSupported.value = data.live_supported
      if (Number.isSafeInteger(data.max_import_bytes) && data.max_import_bytes > 0) maxImportBytes.value = data.max_import_bytes
      if (!data.interfaces.some(i => i.name === selectedInterface.value && i.up && !i.loopback)) {
        selectedInterface.value = data.interfaces.find(i => i.up && !i.loopback)?.name || ''
      }
    } catch (error) { if (!disposed) notice.value = message(error) }
  }

  async function loadServerFiles() {
    if (disposed || filesLoading.value) return
    filesLoading.value = true
    filesError.value = ''
    try {
      const data = await (await request('/api/files')).json() as ServerPCAPFiles
      if (disposed) return
      serverFiles.value = data.files
      serverDirectory.value = data.directory
      filesTruncated.value = data.truncated
      if (!data.files.some(file => file.name === selectedServerFile.value)) selectedServerFile.value = ''
    } catch (error) { if (!disposed) filesError.value = message(error) }
    finally { filesLoading.value = false }
  }

  async function openServerFile() {
    if (!selectedServerFile.value || running.value) return
    await action(async () => {
      await request('/api/files/open', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: selectedServerFile.value }),
      })
      notice.value = '文件已交由服务端后台解析；刷新或关闭网页不会中断，解析期间可点击“停止解析”结束。'
    })
  }

  async function action(operation: () => Promise<unknown>) {
    if (busy.value || disposed) return
    busy.value = true
    invalidatePoll()
    notice.value = ''
    try { await operation() } catch (error) { if (!disposed) notice.value = message(error) }
    finally { busy.value = false; invalidatePoll(); await refresh(true) }
  }

  async function upload(file?: File) {
    if (!file || busy.value || running.value) return
    if (file.size > maxImportBytes.value) { notice.value = `文件超过当前上传上限 ${bytes(maxImportBytes.value)}；可调整 --max-import-mib 或在设备上使用 --read`; return }
    await action(async () => {
      notice.value = `正在上传并逐包解析 ${file.name}…`
      const controller = new AbortController()
      uploadController = controller
      uploading.value = true
      try {
        await request(`/api/import?name=${encodeURIComponent(file.name)}`, { method: 'POST', body: file }, controller)
        notice.value = '导入完成。离线频率以抓包结束时刻为基准；开始新会话会替换当前结果。'
      } catch (error) {
        if (controller.signal.aborted) throw new Error('导入已取消；已解析的数据保留为部分结果。')
        throw error
      } finally { uploading.value = false; uploadController = undefined }
    })
  }

  async function demo(protocol: 'dds' | 'someip' = 'dds') {
    if (busy.value || running.value) return
    try {
      const response = await request(protocol === 'dds' ? '/demo.pcap' : '/mixed-demo.pcap')
      await upload(new File([await response.blob()], protocol === 'dds' ? 'DDS 演示（合成）.pcap' : 'DDS + SOMEIP 混合演示（合成）.pcap'))
    } catch (error) { notice.value = message(error) }
  }

  const startCapture = () => action(() => request('/api/capture/start', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ interface: selectedInterface.value }),
  }))
  const stopCapture = () => action(() => request('/api/capture/stop', { method: 'POST' }))
  const sampleDetail = async (id: number) => await (await request(`/api/samples/${id}`)).json() as SampleDetail
  const someipDetail = async (id: number) => await (await request(`/api/someip/samples/${id}`)).json() as SomeIPDetail
  const updateMapping = (data: Blob | string) => action(async () => {
    await request('/api/someip/config', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: data })
    notice.value = '名称映射已更新；仅保存在当前程序内存中。需要重启后使用，请通过 --someip-map 加载文件。'
  })
  async function importMapping(file?: File) {
    if (!file) return
    if (file.size > 1024 * 1024) { notice.value = '映射文件不能超过 1 MiB'; return }
    await updateMapping(file)
  }
  async function demoMapping() {
    try { await updateMapping(await (await request('/someip-names.json')).blob()) }
    catch (error) { notice.value = message(error) }
  }
  const exportSnapshot = () => action(async () => {
    const blob = await (await request('/api/export')).blob()
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url; link.download = 'proto-watch-snapshot.json'
    link.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  })

  async function tick() {
    await refresh()
    if (!disposed) timer = setTimeout(tick, 1000)
  }
  function startPolling() {
    if (started || disposed) return
    started = true
    void loadInterfaces()
    void loadServerFiles()
    void tick()
  }
  function dispose() {
    disposed = true
    invalidatePoll()
    clearTimeout(timer)
    requests.forEach(controller => controller.abort())
    requests.clear()
  }
  function togglePause() { paused.value = !paused.value; if (!paused.value) void refresh() }

  return { serverFiles, serverDirectory, selectedServerFile, filesLoading, filesError, filesTruncated, loadServerFiles, openServerFile, state, interfaces, selectedInterface, liveSupported, connected, uploading, maxImportBytes, cancelUpload: () => uploadController?.abort(),
    notice, paused, busy, running, refresh, upload, demo, startCapture, stopCapture,
    sampleDetail, someipDetail, importMapping, demoMapping, updateMapping, exportSnapshot, startPolling, dispose, togglePause }
}
