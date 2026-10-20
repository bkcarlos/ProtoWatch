import type { SomeIPEndpoint } from './types'
export const hexID = (value?: number | null, digits = 4) => value == null ? '未知' : `0x${value.toString(16).padStart(digits, '0')}`
export const endpointText = (values: SomeIPEndpoint[]) => values.map(e => `${e.address}:${e.port} / ${e.transport || '未知'}${e.multicast ? ' 组播' : ''}`).join(' · ') || '未提供'
export const serviceStatuses: Record<string, string> = { offered: '公告有效', stopped: '已停止公告', expired: '公告已过期', observed: '仅观察到数据' }
export const someipWarnings: Record<string, string> = {
  tcp_incomplete: 'TCP 结束、空闲超时或回收时仍有缺包 / 未完成消息，未计入完整消息',
  tcp_midstream: '未捕获 TCP 握手，从首个可见报文头开始观察，可能缺少此前数据',
  tcp_invalid_header: 'TCP 中出现无效 SOME/IP 报文头，该方向停止解析至新连接或超时',
  tcp_invalid_message: 'TCP 中出现 SD 或 TP 报文，不按普通 TCP 用户消息解析',
  tcp_message_limit: 'TCP 单条 SOME/IP 消息超过 4 MiB，该方向停止解析',
  tcp_buffer_limit: 'TCP 缓存内容达到 32 MiB 上限，丢弃当前方向未完成数据',
  tcp_reorder_limit: 'TCP 乱序缓存达到 128 段或 256 KiB 上限',
  tcp_gap_limit: 'TCP 序号间隔超过 256 KiB 窗口，该方向停止解析',
  tcp_overlap_conflict: 'TCP 重叠片段内容冲突，该方向停止解析',
  tcp_connection_limit: 'TCP 跟踪达到 256 条连接，最旧连接被回收',
  tcp_magic_cookie: '已识别 TCP 同步标记，不计入用户消息',
  parse_failed: 'SD／已知端口上的报文解析失败', tp_reassembly_unsupported: 'SOME/IP-TP 分段未重组，未计入完整消息',
  sd_option_unsupported: 'SD 选项暂不解析，已按长度跳过',
  sd_entry_unsupported: '未知 SD 条目，已保留观察记录', sd_transport_unsupported: 'SD 端点传输协议不支持',
  ambiguous_instance: '多个公告匹配同一端点，实例保持未知', out_of_order_sd: '旧时间戳 SD 未覆盖较新的服务公告',
  service_limit: '服务记录达到 1,024 条上限', member_limit: '方法／事件记录达到 4,096 条上限', payload_preview_truncated: '载荷预览截断为前 16 KiB',
}
