# ProtoWatch

[![Build and test](https://github.com/bkcarlos/ProtoWatch/actions/workflows/build.yml/badge.svg)](https://github.com/bkcarlos/ProtoWatch/actions/workflows/build.yml)

面向通用 DDS 与 SOME/IP 的被动协议观察台。使用一个二进制程序提供网页与 PCAP 分析，无需安装 DDS SDK、libpcap、Node.js 或数据库。Linux 支持实时抓包；macOS 与 Windows 支持离线分析。

当前为 0.2 MVP：DDS 支持参与者、读写端点、Topic、部分 QoS 和原始样本；SOME/IP 支持 TCP / UDP 消息、服务发现、服务实例与方法 / 事件统计、名称映射和原始载荷。右上角切换协议，同一会话可以观察两种协议。ROS 2 尽量复用 DDS 能力兼容。

- [产品需求](docs/PRD.md)：完整首版目标及产品边界。
- [技术方案](docs/TECHNICAL_DESIGN.md)：选型、架构、MVP 范围、部署和验证方案。
- [SOME/IP 方案与任务清单](docs/SOMEIP.md)：支持范围、配置、接口、资源限制和验收。
- [验证记录](docs/VALIDATION.md)：本轮执行的检查与尚未验证的条件。

## 本地运行

需要与 `go.mod` 兼容的 Go（当前使用 Go 1.25.5）。仓库保留已构建的网页资源，可以直接运行：

```sh
go run ./cmd/proto-watch
```

浏览器打开 [本地网页](http://127.0.0.1:8080)。可从“服务端 PCAP”选择程序工作目录中的抓包文件，也可点击“试用合成示例”。示例数据有明确标记，不来自真实 DDS 网络。

macOS 与 Windows 只支持离线文件；实时采集在 Linux 上执行。

## 下载与跨平台编译

打开 [GitHub Actions](https://github.com/bkcarlos/ProtoWatch/actions/workflows/build.yml)，选择成功的运行，在 **Artifacts** 下载对应平台的压缩包及 SHA-256 校验文件。解开 Actions 外层 ZIP，再解开其中的平台压缩包，得到 `proto-watch` 或 `proto-watch.exe`。运行只需要该二进制，随包文档与许可用于说明和分发。

| 平台 | Artifact | 能力 |
| --- | --- | --- |
| Linux x86-64 | `proto-watch-linux-amd64` | 实时抓包、PCAP、网页 |
| Linux ARM64 | `proto-watch-linux-arm64` | 实时抓包、PCAP、网页 |
| macOS Intel | `proto-watch-darwin-amd64` | PCAP、网页 |
| macOS Apple Silicon | `proto-watch-darwin-arm64` | PCAP、网页 |
| Windows x86-64 | `proto-watch-windows-amd64` | PCAP、网页 |

Linux / macOS 解压后运行 `./proto-watch --pcap-dir /path/to/captures`；Windows PowerShell 运行 `.\proto-watch.exe --pcap-dir C:\captures`，然后打开 http://127.0.0.1:8080。启动目录也可直接放置 `.pcap` 文件。

流水线在 `master` / `main` 推送、Pull Request、`v*` 标签推送以及手动触发时执行。前端只构建一次，5 个原生平台分别执行 Go 检查、race 测试、无 CGO 编译以及实际启动程序的 HTTP / PCAP 冒烟验证。产物保存 30 天；当前生成 Actions Artifacts，不自动创建 Release。`--version` 和 API 返回版本与提交号。

本地跨平台打包需要 Go、Node.js 和 Python 3（标准库即可）：

```sh
make frontend
python3 tools/build.py linux-arm64
python3 tools/build.py linux-amd64
python3 tools/build.py darwin-arm64
python3 tools/build.py darwin-amd64
python3 tools/build.py windows-amd64
```

产物位于 `dist/`，Linux / macOS 为 `.tar.gz`，Windows 为 `.zip`，每份附 `.sha256`。Windows 本地构建可先执行 `npm --prefix frontend ci` 和 `npm --prefix frontend run build`，再用 `python tools/build.py windows-amd64`，无需 Make。macOS / Windows 产物暂未进行发行者签名或公证。

## 构建 Linux ARM64 程序

开发机构建需要 Go 和 Node.js（20.19+ 的 20.x，或 22.12+）；设备运行无需 Node.js。

```sh
make arm64
```

命令会安装锁定的前端依赖、检查 TypeScript、构建网页，再编译 Go。产物为 `bin/proto-watch-linux-arm64`，网页已内嵌。若使用仓库中已有的网页构建产物，也可直接执行：

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath \
  -ldflags="-s -w" -o bin/proto-watch-linux-arm64 ./cmd/proto-watch
```

## 在设备上观察

接口名按设备实际配置替换：

```sh
chmod +x ./proto-watch-linux-arm64
sudo setcap cap_net_raw=ep ./proto-watch-linux-arm64
./proto-watch-linux-arm64 --interface eth0
```

默认仅监听设备的 `127.0.0.1:8080`。可以在开发机建立 SSH 转发后访问网页：

```sh
ssh -L 8080:127.0.0.1:8080 user@device-ip
```

也可直接对局域网开放：

```sh
./proto-watch-linux-arm64 --listen 0.0.0.0:8080 --interface eth0
```

浏览器访问设备地址即可使用，无需登录或配置令牌。普通 HTTP 不加密；需要加密时使用 SSH 转发，或传入 `--tls-cert` 和 `--tls-key` 开启内置 HTTPS。无法设置文件 capability 的设备需由系统管理员以合适权限运行；离线文件模式不需要抓包权限。

从文件启动：

```sh
./proto-watch-linux-arm64 --read capture.pcap
```

## SOME/IP 快速体验

启动后在右上角选择 **SOME/IP**，点击“试用混合示例”，可看到 4 个服务记录、93 条用户消息和 9 条发现记录。切回 DDS，同一抓包中有 120 条 DDS 样本。

“载入示例映射”会将合成示例的 ID 显示成可读名称。自己的项目使用“导入名称映射”，JSON 格式见 [配置示例](docs/examples/someip-names.json)。网页配置仅保存在当前进程；启动时可加载可选配置文件：

```sh
./proto-watch-linux-arm64 --someip-map someip-names.json --read capture.pcap
```

无映射也可观察数字 ID，不影响单二进制部署。SOME/IP 普通消息不包含实例 ID，通过有效 SD 公告和端点关联；无证据时显示未知。TCP 支持有界流重组、乱序和重传去重；SOME/IP-TP 重组及 ARXML 业务字段解码尚未支持，完整说明见 [SOME/IP 文档](docs/SOMEIP.md)。

TCP 合成示例包含跨包、乱序、重传、粘包和 100 KiB 载荷，可从服务端文件列表加载：

```sh
./bin/proto-watch --pcap-dir frontend/public --read frontend/public/tcp-demo.pcap
```

在 SOME/IP 视图预期看到 4 条 TCP、1 条 UDP 用户消息和 1 条发现记录。消息区支持按 TCP / UDP 筛选，详情显示重组消息的载荷；16 KiB 预览上限仍有效。

## DDS 支持范围

- 单个 Ethernet 兼容网卡；RTPS 2.x over UDP / IPv4。
- 未压缩 classic PCAP，网页上传默认最多 4 GiB（`--max-import-mib` 可调整），本地 `--read` 不限总大小，支持 Ethernet / Linux SLL 封装。
- PL_CDR 编码的 SPDP / SEDP 基础发现、未分片 DATA 样本。
- 最近 256 条样本、每条最多 16 KiB 原始预览、5 秒统计窗口。
- 按 GUID 与序列号在最近 65,536 个组合内去重；超出窗口不保证全会话去重。
- 查看与导出有限内存快照；新会话替换旧结果，重启不保留数据。

当前不支持 IDL 业务字段解码、RTPS / IP 分片重组、PCAPNG、IPv6、DDS over TCP、共享内存、加密内容、主动订阅、完整租约生命周期和 ROS 节点图。相关情况会显示提示或保留可见信息；未知类型不会猜测字段。SOME/IP 的独立范围见上文。

## 前端开发

前端使用 **Vue 3 + TypeScript + Vite + Element Plus**。源码在 `frontend/src/`，构建产物输出到 `internal/server/web/` 并由 Go embed 嵌入。不要直接编辑构建产物。

启动 Go 后端：

```sh
go run ./cmd/proto-watch
```

另一个终端启动带热更新的前端：

```sh
make dev-web
```

浏览器打开 http://127.0.0.1:5173，Vite 将 `/api` 代理到本地 8080 端口；后端地址可用 `PROTOWATCH_BACKEND`（兼容旧名 `DDSWATCH_BACKEND`）环境变量覆盖。生产模式用 `make run` 启动，或用 `make build` / `make arm64` 生成自包含程序。

组件分为观察表格、样本列表、详情弹窗和空状态；请求与轮询集中在 `frontend/src/composables/useWatch.ts`。界面使用 Element Plus 的菜单、卡片、统计、可搜索下拉框、可排序表格、分页、状态提示和详情弹窗。样本支持每页 10 / 20 / 50 条、Topic 标签清除，以及十六进制 / Base64 切换。目前使用 Vue 自带响应式状态，单页不引入路由或额外状态库。

## 开发检查

```sh
make test
go test -race ./...
go test ./internal/rtps -fuzz=FuzzParse -fuzztime=10s
go test ./internal/someip -fuzz=FuzzParse -fuzztime=5s -parallel=4
```

`make test` 会进行前端类型检查、生产构建、Vitest 测试和 Go 测试。前端依赖锁定在 `frontend/package-lock.json`。运行 `tools/generate_demo.py` 可重新生成 `frontend/public/demo.pcap`，再运行 `tools/generate_someip_demo.py` 生成 SOME/IP 与混合示例，运行 `tools/generate_tcp_demo.py` 生成 TCP 示例，随后执行 `make frontend` 更新内嵌示例；Python 仅用于生成开发数据。

后端依赖许可文本内嵌于 `/licenses.txt`，Vue 许可在 `/vue-license.txt`；Element Plus、图标及前端运行依赖许可汇总在 `/frontend-licenses.txt`。

项目许可沿用仓库现有的 [GNU GPL v3](LICENSE)。源代码见 [bkcarlos/ProtoWatch](https://github.com/bkcarlos/ProtoWatch)。


## 服务端离线文件（推荐）

默认从**程序启动时的工作目录**列出 `.pcap` / `.cap` 普通文件。在网页“服务端 PCAP”中搜索文件名并点击“加载文件”，后端直接逐包读取，浏览器只发送文件名，不传输文件内容。文件列表可以手动刷新。

```sh
# 默认使用当前工作目录
./proto-watch-linux-arm64

# 使用设备上的专用抓包目录
./proto-watch-linux-arm64 --pcap-dir /data/captures

# 启动时立即在后台分析指定文件
./proto-watch-linux-arm64 --read /data/captures/drive.pcap
```

服务端文件没有总大小限制。后台任务独立于浏览器连接，关闭或刷新页面仍继续解析；可在网页点击“停止解析”保留部分结果，再选择其他文件。同一时间只运行一个观察会话。

目录选择只列出当前目录，暂不递归子目录，也不列出符号链接；最多返回 1,000 个文件、扫描 10,000 个目录项，超过时会提示改用更具体的 `--pcap-dir`。网页不能指定此目录外的路径。`--read` 为命令行指定文件，不受该目录约束。浏览器上传保留为辅助入口。

## 大文件导入

网页导入默认上限 **4 GiB**，可通过启动参数调整：

```sh
# 网页上传上限调到 16 GiB（参数单位为 MiB）
./proto-watch-linux-arm64 --max-import-mib 16384

# 文件已在设备上：逐包读取，不限制总文件大小
./proto-watch-linux-arm64 --read large.pcap
```

网页边上传边解析，显示已读取字节和包数，可取消导入。文件不会整份读入内存，也不写临时文件；单包 1 MiB 和实体 / 样本缓存上限继续有效。取消、上传失败或文件损坏时保留部分结果，状态不会标为完成。导出仍只包含有限缓存中的样本。

持续上传不受原来 60 / 90 秒总请求时限影响，读取空闲超过 2 分钟会超时；如使用反向代理，也需配置相应的请求体上限和超时。`--read` 在后台解析，HTTP 服务无需等待整份文件读完。4 GiB 是默认允许值，目标 ARM64 设备的性能尚未实测。
