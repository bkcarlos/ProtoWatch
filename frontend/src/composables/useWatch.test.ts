import { afterEach, describe, expect, it, vi } from 'vitest'
import { createWatchClient } from './useWatch'

const json = (data: unknown, status = 200) => new Response(JSON.stringify(data), { status })
const clients: ReturnType<typeof createWatchClient>[] = []
function client(fetcher: typeof fetch) {
  const result = createWatchClient(fetcher)
  clients.push(result)
  return result
}
afterEach(() => { clients.splice(0).forEach(c => c.dispose()); vi.useRealTimers() })

describe('observation requests', () => {
  it('recovers from a transient connection failure without credentials', async () => {
    const fetcher = vi.fn<typeof fetch>()
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
      .mockResolvedValueOnce(json({ status: 'idle', source: 'connected' }))
    const c = client(fetcher)
    await c.refresh()
    expect(c.connected.value).toBe(false)
    await c.refresh()
    expect(c.connected.value).toBe(true)
    expect(c.state.value?.source).toBe('connected')
    for (const [, init] of fetcher.mock.calls) {
      expect(new Headers(init?.headers).has('Authorization')).toBe(false)
    }
  })

  it('does not let a delayed poll replace the state after importing a new session', async () => {
    let resolveOld!: (response: Response) => void
    let oldSignal: AbortSignal | null | undefined
    const fetcher = vi.fn<typeof fetch>()
      .mockImplementationOnce((_path, init) => {
        oldSignal = init?.signal
        return new Promise(resolve => { resolveOld = resolve })
      })
      .mockResolvedValueOnce(json({ sample_count: 120 }))
      .mockResolvedValueOnce(json({ source: 'new.pcap', status: 'complete' }))
    const c = client(fetcher)
    const oldPoll = c.refresh()
    await c.upload(new File(['pcap'], 'new.pcap'))
    expect(oldSignal?.aborted).toBe(true)
    expect(c.state.value?.source).toBe('new.pcap')
    // Model a response that was already buffered when cancellation happened.
    resolveOld(json({ source: 'old.pcap', status: 'complete' }))
    await oldPoll
    expect(c.state.value?.source).toBe('new.pcap')
  })

  it('serializes polling, pauses refresh, and cancels requests on disposal', async () => {
    vi.useFakeTimers()
    let resolvePoll!: (response: Response) => void
    let signal: AbortSignal | null | undefined
    const fetcher = vi.fn<typeof fetch>(async (path, init) => {
      if (path === '/api/interfaces') return json({ interfaces: [], live_supported: false })
      if (path === '/api/files') return json({ directory: '/captures', files: [], truncated: false })
      signal = init?.signal
      return new Promise(resolve => { resolvePoll = resolve })
    })
    const c = client(fetcher)
    c.startPolling()
    await vi.advanceTimersByTimeAsync(5000)
    await c.refresh()
    expect(fetcher.mock.calls.filter(([path]) => path === '/api/state')).toHaveLength(1)
    c.togglePause()
    resolvePoll(json({ status: 'idle' }))
    await vi.advanceTimersByTimeAsync(3000)
    expect(fetcher.mock.calls.filter(([path]) => path === '/api/state')).toHaveLength(1)
    c.togglePause()
    expect(fetcher.mock.calls.filter(([path]) => path === '/api/state')).toHaveLength(2)
    c.dispose()
    expect(signal?.aborted).toBe(true)
    resolvePoll(json({ source: 'discarded' }))
    await vi.advanceTimersByTimeAsync(3000)
    expect(c.state.value?.source).toBeUndefined()
    expect(fetcher.mock.calls.filter(([path]) => path === '/api/state')).toHaveLength(2)
  })

  it('restores the controls and preserves the error when an import fails', async () => {
    const fetcher = vi.fn<typeof fetch>()
      .mockResolvedValueOnce(json({ error: '不是支持的 PCAP 文件' }, 400))
      .mockResolvedValueOnce(json({ status: 'idle' }))
    const c = client(fetcher)
    await c.upload(new File(['bad'], 'bad.pcap'))
    expect(c.busy.value).toBe(false)
    expect(c.notice.value).toBe('不是支持的 PCAP 文件')
    expect(c.connected.value).toBe(true)
  })
})

describe('SOME/IP API integration', () => {
  it('loads mixed capture for the SOME/IP demo and routes details separately', async () => {
    const fetcher = vi.fn<typeof fetch>()
      .mockResolvedValueOnce(new Response('pcap'))
      .mockResolvedValueOnce(json({}))
      .mockResolvedValueOnce(json({ status: 'complete' }))
      .mockResolvedValueOnce(json({ sample: { id: 2, service_id: 4660 }, hex: '', payload_base64: '' }))
    const c = client(fetcher)
    await c.demo('someip')
    expect(fetcher.mock.calls[0][0]).toBe('/mixed-demo.pcap')
    expect(String(fetcher.mock.calls[1][0])).toContain('/api/import?name=')
    expect((await c.someipDetail(2)).sample.service_id).toBe(4660)
    expect(fetcher.mock.calls[3][0]).toBe('/api/someip/samples/2')
  })
  it('uploads mappings as JSON and refreshes the current observation', async () => {
    const fetcher = vi.fn<typeof fetch>()
      .mockResolvedValueOnce(json({ services: {} }))
      .mockResolvedValueOnce(json({ status: 'complete', someip: { message_count: 93 } }))
    const c = client(fetcher)
    await c.importMapping(new File(['{"services":{}}'], 'names.json'))
    expect(fetcher.mock.calls[0][0]).toBe('/api/someip/config')
    expect(fetcher.mock.calls[0][1]?.method).toBe('PUT')
    expect(new Headers(fetcher.mock.calls[0][1]?.headers).get('Content-Type')).toBe('application/json')
    expect(c.state.value?.someip.message_count).toBe(93)
    expect(c.busy.value).toBe(false)
  })
})

describe('large PCAP imports', () => {
  it('accepts files above 64 MiB and rejects files above the advertised server limit', async () => {
    const fetcher = vi.fn<typeof fetch>(async path => path === '/api/interfaces'
      ? json({ interfaces: [], live_supported: false, max_import_bytes: 128 * 1024 ** 2 })
      : path === '/api/files' ? json({ directory: '/captures', files: [], truncated: false }) : json({ status: 'complete' }))
    const c = client(fetcher)
    c.startPolling()
    await vi.waitFor(() => expect(c.maxImportBytes.value).toBe(128 * 1024 ** 2))
    const file = new File(['fixture'], 'large.pcap')
    Object.defineProperty(file, 'size', { value: 100 * 1024 ** 2, configurable: true })
    await c.upload(file)
    expect(fetcher.mock.calls.filter(([path]) => String(path).startsWith('/api/import?'))).toHaveLength(1)
    Object.defineProperty(file, 'size', { value: 129 * 1024 ** 2 })
    await c.upload(file)
    expect(fetcher.mock.calls.filter(([path]) => String(path).startsWith('/api/import?'))).toHaveLength(1)
    expect(c.notice.value).toContain('128.0 MiB')
  })

  it('refreshes progress while uploading and can cancel the upload', async () => {
    let signal: AbortSignal | undefined
    const fetcher = vi.fn<typeof fetch>(async (path, init) => {
      if (String(path).startsWith('/api/import?')) {
        signal = init?.signal as AbortSignal
        return new Promise((_resolve, reject) => signal!.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError'))))
      }
      return json({ status: signal?.aborted ? 'stopped' : 'importing', packets: 12, import_bytes: 100000 })
    })
    const c = client(fetcher)
    const pending = c.upload(new File(['pcap'], 'large.pcap'))
    expect(c.uploading.value).toBe(true)
    await c.refresh()
    expect(c.state.value?.packets).toBe(12)
    c.cancelUpload()
    await pending
    expect(signal?.aborted).toBe(true)
    expect(c.uploading.value).toBe(false)
    expect(c.busy.value).toBe(false)
    expect(c.notice.value).toContain('取消')
    expect(c.state.value?.status).toBe('stopped')
  })
})


describe('server-side PCAP files', () => {
  it('lists and opens a server file without uploading its bytes', async () => {
    const fetcher = vi.fn<typeof fetch>()
      .mockResolvedValueOnce(json({ directory: '/captures', files: [{ name: 'drive.pcap', size: 10 * 1024 ** 3, modified: '' }], truncated: false }))
      .mockResolvedValueOnce(json({ ok: true }, 202))
      .mockResolvedValueOnce(json({ status: 'importing', source: '/captures/drive.pcap' }))
    const c = client(fetcher)
    await c.loadServerFiles()
    expect(c.serverFiles.value[0].size).toBe(10 * 1024 ** 3)
    c.selectedServerFile.value = 'drive.pcap'
    await c.openServerFile()
    expect(fetcher.mock.calls[1][0]).toBe('/api/files/open')
    expect(JSON.parse(String(fetcher.mock.calls[1][1]?.body))).toEqual({ name: 'drive.pcap' })
    expect(c.running.value).toBe(true)
    expect(c.busy.value).toBe(false)
  })

  it('clears a removed selection and surfaces directory errors separately', async () => {
    const fetcher = vi.fn<typeof fetch>()
      .mockResolvedValueOnce(json({ directory: '/captures', files: [], truncated: false }))
      .mockResolvedValueOnce(json({ error: 'directory unavailable' }, 400))
    const c = client(fetcher)
    c.selectedServerFile.value = 'removed.pcap'
    await c.loadServerFiles()
    expect(c.selectedServerFile.value).toBe('')
    await c.loadServerFiles()
    expect(c.filesError.value).toBe('directory unavailable')
    expect(c.filesLoading.value).toBe(false)
  })
})
