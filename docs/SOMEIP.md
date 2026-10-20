# SOME/IP MVP 技术与交付说明

版本：0.2.0-mvp；日期：2026-10-20。

本次完成约定的 **TCP / UDP + SOME/IP-SD + 原始载荷观察**。保留 Go、Vue 3、Element Plus 和 Linux ARM64 单二进制部署，同一采集会话同时处理 DDS 和 SOME/IP，右上角切换协议只切换视图。

## 已实现范围

- IPv4 / UDP SOME/IP 报文头、长度和类型检查；一个 UDP 数据报中的多条 SOME/IP 消息。
- IPv4 / TCP 双向流重组：头部与载荷跨包、粘包、有限乱序、重传去重、序列号回绕及连接关闭处理。SD 仍通过 UDP 观察。
- 请求、无返回请求、响应、错误、事件通知的观察与计数；记录服务、方法 / 事件、Client、Session、接口主版本、返回码、通信方向和捕获时间。
- SOME/IP-SD FindService、OfferService、StopOfferService、SubscribeEventgroup、StopSubscribeEventgroup、Ack 和 Nack。
- SD 两组 options 引用边界检查；IPv4 / IPv6 端点与组播端点的地址、UDP / TCP 协议、端口展示。IPv6 数据报本身仍不进入消息解析。
- 根据公告 TTL 显示有效 / 过期状态；TTL 为 0 的 Offer 表示停止。状态只代表本观察点证据，不等同设备存活状态。
- 根据服务 ID、接口主版本、有效公告端点解析实例。不能匹配或存在歧义时保持未知，不把 0 当作未知实例。数据头没有接口次版本，未知时不填造数值。
- 服务与方法 / 事件统计、5 秒窗口频率和载荷速率；最近消息、发现记录、十六进制 / Base64 详情与 JSON 快照导出。
- JSON 名称映射：网页导入、示例、清除、导出；启动参数 `--someip-map`。可同时配置需要识别的 UDP / TCP 端口。

## 结构

```text
internal/capture    网卡 / PCAP 读取 → Sink 接口
internal/packet     Ethernet / SLL / VLAN + IPv4 + UDP / TCP 外层解析
internal/watch      会话计数、互斥保护、协议分发
  dds.go            DDS / RTPS 聚合
internal/someip
  parse.go          有界消息和 SD 解析
  tcp.go            有界 TCP 连接跟踪、序列重组和消息拆帧
  store.go          服务、方法、消息、公告 TTL 和统计
  config.go         名称映射校验与规范化
internal/server     同源 HTTP API、静态资源内嵌
frontend            协议切换、复用工作台与 SOME/IP 观察面板
```

采集器只依赖 Sink 接口，不再依赖 DDS 数据模型。现有 DDS API 保持兼容；`GET /api/state` 增加 `someip` 字段，DDS 计数和 SOME/IP 计数分别保存。总体 packets / wire_bytes 仍表示整个捕获会话。

新增接口：

| 接口 | 行为 |
| --- | --- |
| GET /api/someip/samples/{id} | 有限缓存中的消息元信息和原始载荷 |
| GET /api/someip/config | 当前名称和端口配置 |
| PUT /api/someip/config | 校验后整体替换配置；失败保留原配置 |
| GET /api/export | 增加 someip_config 与 someip_retained_samples |

## 识别和关联边界

SOME/IP 无固定魔数。已配置端口或有效 SD 端点提供较强识别依据；其他 UDP 载荷需要满足整个数据报的头部、长度和类型检查，界面标注“报文头结构匹配（推测）”。仅靠头部可能误识别，不能当作完整协议认证。没有匹配结构的普通 UDP 不计入 SOME/IP。

实例关联使用捕获时刻有效的 Offer。缺少公告的历史样本保持未知，不用晚到的公告倒推其当时实例。不同提供方、实例和接口主版本分别保存。对较旧时间戳的公告保留发现记录，但不覆盖较新的服务状态。未实现根据 SD 重启序列清除全部历史服务，也不根据订阅应答推断实际消息必达。

SOME/IP Session ID 可复用，不按 Session ID 做全局去重；TCP 按连接方向和字节序列去重，UDP 保留捕获到的重复消息。尚未实现请求 / 响应配对、调用耗时、超时诊断及事件与事件组的业务映射。

## 按服务提供方查看

SOME/IP 面板的“服务提供方”选择器按 IP 聚合服务记录和累计消息数，联动筛选服务、方法 / 事件及最近消息。切换提供方会清除服务子筛选并回到第一页。方法、消息列表和消息详情均显示提供方；同一 Service ID / Instance ID 在不同提供方下分别保留。

匹配有效 SD 时，提供方为 Offer 来源 IP，并通过公告端点关联用户消息；没有匹配公告时，按消息类型与通信方向使用服务端 IP，实例保持未知。请求归属目标服务端，响应和事件通知归属来源服务端，Client ID 不作为发布方标识。同一 IP 下不同端口 / 实例可在服务列表继续区分，不能仅凭抓包识别设备上的进程或自动得到 ECU 名称。

顶部指标仍为全会话；发现记录与观察提示也保留全会话视图，页面会标注范围，避免把客户端发出的 Find / Subscribe 错标为发布方公告。消息缓存仍为全局最近 256 条，筛选不改变缓存配额。

## 名称配置

完整示例见 [someip-names.json](examples/someip-names.json)。键支持十进制或 `0x` 十六进制，保存后规范化为四位十六进制。methods 同时映射方法 ID 和事件 ID，eventgroups 映射事件组 ID；名称来自配置，不是从载荷中推断。

```json
{
  "ports": [30501],
  "services": {
    "0x1234": {
      "name": "VehicleStatus",
      "methods": {"0x0001": "GetTemperature", "0x8001": "TemperatureChanged"},
      "eventgroups": {"0x0001": "Telemetry"}
    }
  }
}
```

```sh
./proto-watch-linux-arm64 --listen 0.0.0.0:8080 --interface eth0 --someip-map someip-names.json
```

配置是可选用户数据；不提供配置文件也可用一个二进制运行，查看数字 ID 或通过网页导入映射。网页导入仅在进程内保存，开始新观察会话保留映射，重启程序后需重新导入或通过参数加载。清除映射不会删除观察数据。名称立即更新已保留记录；端口配置只影响后续报文识别，不回溯重算历史报文。

## 资源边界与后续范围

| 资源 | 限制 |
| --- | --- |
| 服务记录 | 1,024 条，达到上限后提示 |
| 方法 / 事件记录 | 4,096 条，达到上限后提示 |
| 用户消息 / SD 记录 | 各保留最近 256 条，淘汰数单独统计 |
| 载荷预览 | 每条最多 16 KiB |
| 配置 | 1 MiB；1,024 个服务；16,384 个成员名称；256 个端口；名称最多 256 字节 |
| 单 UDP 数据报 | 最多 65,507 字节、1,024 条消息；长度错误时整体拒绝，避免部分入库 |

| TCP 连接跟踪 | 最多 256 条双向连接，包含关闭后暂存的去重记录；超限淘汰最旧记录并提示 |
| TCP 单条消息 | 最多 4 MiB，包含 16 字节 SOME/IP 头；不是 16 KiB 预览上限 |
| TCP 缓存内容 | 全局最多 32 MiB；不等于进程总内存或分配器实际占用 |
| TCP 乱序缓存 | 每方向最多 128 段、256 KiB 内容，前向序列距离也限制为 256 KiB |
| TCP 空闲清理 | 60 秒；离线按捕获时间水位，实时模式也按当前时间清理 |

仍不支持 SOME/IP-TP 与 IP 分片重组、ARXML / FIDL 类型导入、业务字段解码、PCAPNG、IPv6 数据流、加密内容和主动调用。已识别的 UDP TP 报文会提示且不计入完整消息。TCP 中出现 SD 或 TP 消息会拒绝该方向。

## TCP 重组语义

- 网络四元组区分连接，两个方向独立维护序列；新 SYN 区分连接代次。已经接收的字节按首次观察结果保留，重传不再次计入用户消息。缓存中的乱序段若内容冲突，会提示并停止该方向重组。
- 先恢复连续字节，再按 SOME/IP Length 字段加 8 字节拆帧。只有完整消息进入服务、方法与消息统计；TCP 包数与用户消息数不同。相关载荷包计数包含已识别连接的重传和配置端点上的载荷包，不含纯握手 / ACK。
- 如果捕获从连接中途开始，只有首段恰好处于 SOME/IP 消息边界才能继续解析，并显示中途捕获提示。不会扫描任意字节来猜测新消息边界；未捕获的前半段和历史消息无法恢复。
- 缺口不会被跳过；非法头部、超限或冲突后停止该方向，等待新连接或超时清理。FIN、RST、文件结束、停止观察、空闲和连接淘汰会清理缓存，对已识别流中的残缺消息提示。完整读完 PCAP 不代表每条 TCP 消息完整，应同时查看观察提示。
- 识别消息边界处的 vsomeip magic cookie，不把它计入用户消息；暂不使用 cookie 在缺包后重新同步。
- TCP 样本的捕获时间与包号指向完成连续重组的捕获包；完整载荷长度参与统计，详情只保留前 16 KiB。已关闭连接的去重记录受 60 秒 / 256 连接限制，清理后不保证迟到重传仍能去重。
- 实例关联同时匹配端点地址、端口和 TCP / UDP，方法统计按传输方式分开。消息列表与 JSON 导出带 `transport`；`someip` 状态增加 `tcp_messages`、`udp_messages`、`tcp_connections`、`tcp_buffered`。连接数为内部跟踪记录数，不能当成设备当前在线连接数。

TCP 与 UDP 的承载方式参考 [COVESA 协议介绍](https://github.com/COVESA/vsomeip/wiki/vsomeip-in-10-minutes)，cookie 字节参考 [COVESA 常量定义](https://github.com/COVESA/vsomeip/blob/master/interface/vsomeip/constants.hpp)。

## 开发与验收

```sh
python3 tools/generate_demo.py
python3 tools/generate_someip_demo.py
python3 tools/generate_tcp_demo.py
make test build arm64
go test -race ./...
go test ./internal/someip -fuzz=FuzzParse -fuzztime=5s -parallel=4
go test ./internal/someip -fuzz=FuzzTCPStream -fuzztime=5s -parallel=4
```

- `demo.pcap`：原有 DDS 示例，128 包 / 120 样本。
- `someip-demo.pcap`：102 包 / 93 用户消息 / 9 发现条目，包含 1 个未重组 TP 包。
- `mixed-demo.pcap`：230 包，同时包含上述两种协议。SOME/IP 页面“试用混合示例”加载此文件。
- `tcp-demo.pcap`：86 包 / 4 条 TCP 用户消息 / 1 条 UDP 用户消息 / 1 条 SD，包含 100 KiB 跨包消息、乱序、重传和双向通信；提供方 192.0.2.30、实例 0x0001。预览截断提示为预期行为。
- 混合示例 SOME/IP 预期：31 请求、30 响应、32 通知、1 错误，4 服务记录、3 方法 / 事件记录；服务状态分别为有效、停止、过期、实例未知。

所有示例均为合成数据，不能作为真实车辆或厂商兼容性证明。实际 Linux ARM64 设备、真实 ECU / vsomeip 和长期负载尚需现场验证。

## 本次任务清单

- [x] 采集 Sink、公共网络外层、DDS / SOME/IP 分发拆分
- [x] UDP 多消息与 SOME/IP-SD 有界解析
- [x] TCP 有界流重组、重传去重、乱序、连接清理与诊断
- [x] TCP / UDP 展示、消息筛选与详情 / 导出传输字段
- [x] 服务实例关联、TTL / StopOffer 状态和协议独立计数
- [x] 服务、方法、事件、消息与发现记录的有限缓存
- [x] 名称与端口配置、CLI 加载、网页导入 / 清除 / 导出
- [x] SOME/IP 详情接口与两种协议快照导出
- [x] Element Plus 协议切换、列表筛选、分页与原始载荷详情
- [x] 独立 / 混合示例、长度与引用边界测试、容量与并发测试
- [x] 类型检查、前后端回归、race 和 SOME/IP fuzz
- [x] 最终浏览器验收、交付摘要与 ARM64 产物检查（静态 ELF，体积随内嵌资源更新）

实现格式参考 [COVESA 协议介绍](https://github.com/COVESA/vsomeip/wiki/vsomeip-in-10-minutes) 和 [COVESA SD 格式实现](https://github.com/COVESA/vsomeip/tree/master/implementation/service_discovery)。本项目自行实现被动解析，不链接 vsomeip 运行库。
