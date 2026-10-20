export const number = (value = 0) => value.toLocaleString('zh-CN')
export const time = (value?: string) => !value || value.startsWith('0001') ? '—' : new Date(value).toLocaleTimeString('zh-CN', { hour12: false })
export const bytes = (value: number) => value >= 1024 ** 3 ? (value / 1024 ** 3).toFixed(1) + ' GiB' : value >= 1048576 ? (value / 1048576).toFixed(1) + ' MiB' : value >= 1024 ? (value / 1024).toFixed(1) + ' KiB' : Number(value.toFixed(1)) + ' B'
export const short = (value: string) => value ? value.slice(0, 8) + '…' + value.slice(-8) : '—'

export const statuses = {
  idle: '尚未开始', running: '正在采集', importing: '正在导入',
  complete: '离线分析完成', stopped: '采集已停止', error: '会话出错',
}
export const warnings: Record<string, [string, string]> = {
  rtps_fragment_unsupported: ['RTPS 分片暂不重组', 'DATA_FRAG 已识别，未计入完整用户样本。'],
  ip_fragment_unsupported: ['IP 分片暂不重组', 'MVP 不解析分片中的 UDP 载荷。'],
  protected_rtps: ['检测到受保护的 RTPS', '只能查看可见外层信息，不能解密。'],
  rtps_parse_failed: ['RTPS 解析失败', '报文不完整、格式异常或超出当前支持范围。'],
  discovery_decode_failed: ['发现载荷无法解析', '当前支持 PL_CDR 编码的 SPDP / SEDP。'],
  capture_truncated: ['捕获包被截断', '截断的包未计入样本，请检查抓包快照长度。'],
  unknown_submessage: ['未识别的子消息', '已按协议长度跳过。'],
  key_only_data: ['仅 Key 的数据', '未按普通业务样本统计。'],
  status_only_data: ['仅状态的 DATA', '本版本不据此推断完整实体生命周期。'],
  payload_preview_truncated: ['样本预览被截断', '只保留前 16 KiB，完整载荷长度仍参与统计。'],
  ipv6_unsupported: ['IPv6 未纳入本版解析', '此计数包括观察点可见的非 DDS IPv6 流量。'],
  participant_limit: ['参与者达到数量上限', '最多保留 1,024 个参与者。'],
  endpoint_limit: ['端点达到数量上限', '最多保留 4,096 个端点。'],
  malformed_packet: ['网络报文解析失败', '未完整解析的包不进入样本统计。'],
}
