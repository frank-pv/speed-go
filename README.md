# speed-go

HTTP/HTTPS 下载压测工具，用 Go 编写。压测行为尽可能贴近真实 Chrome 浏览器：默认模拟最新版 Chrome 的 User-Agent 与完整导航请求头，HTTPS 优先使用 HTTP/2。

## 功能特性

- **贴近 Chrome 的压测行为**：默认 UA 为最新版 Chrome（Windows x64），并携带 `Sec-Ch-Ua`、`Sec-Fetch-*`、`Accept`、`Accept-Language`、`Upgrade-Insecure-Requests` 等 Chrome 默认请求头；显式协商 `gzip, deflate, br, zstd` 编码，统计的是真实线上字节数
- **实时流量统计**：下载过程中按每次 `Read` 返回的数据块实时累加字节数（不等整个文件下载完成），每秒打印当前速率与累计流量
- **优雅退出**：`Ctrl+C` 随时中断，正常结束或中断均输出完整压测结果
- **自动单位换算**：总流量按 B → KB → MB → GB → TB 自动换算显示

## 安装

```bash
go build -o speed-go .
```

要求 Go 1.21+（无第三方依赖，仅标准库）。

## 安装

**方式一：从 GitHub Releases 下载**（推荐）

到 [Releases 页面](../../releases) 下载对应平台的二进制文件（Linux/macOS/Windows，含 arm64），校验 `.sha256` 后即可直接运行。

**方式二：源码编译**

```bash
go build -o speed-go .
```

要求 Go 1.21+（无第三方依赖，仅标准库）。

## 使用

```bash
# 默认参数: 5 连接, 60 秒
./speed-go -url https://example.com/file.zip

# 10 连接, 压 2 分钟, 忽略 HTTPS 证书校验
./speed-go -c 10 -t 2m -k -url https://example.com/file.zip

# 自定义 UA + 指定来源页面 Referer
./speed-go -ua "Mozilla/5.0 (Macintosh; ...)" -referer "https://example.com/page.html" -url https://example.com/file.zip

# 时长支持三种写法: 60 (纯数字按秒) / 60s / 2m
./speed-go -t 30 -url https://example.com/file.zip

# 附加自定义请求头 (可多次使用)
./speed-go -H "X-Token: abc" -H "X-Debug: 1" -url https://example.com/file.zip

# 也兼容位置参数写法
./speed-go https://example.com/file.zip
```

## 参数说明

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-url` | 必填 | 压测目标 URL（也兼容位置参数写法） |
| `-c` | `5` | 并发连接数 |
| `-t` | `60s` | 压测时长，支持 `60`（纯数字按秒）/ `60s` / `2m` |
| `-k` | 关闭 | 忽略 HTTPS 证书校验 |
| `-ua` | 最新版 Chrome | 自定义 User-Agent |
| `-referer` | 不携带 | 请求的来源页面 Referer |
| `-H` | 无 | 附加额外请求头，格式 `"Key: Value"`，可多次使用 |
| `-h` / `-help` | — | 显示帮助 |

未携带任何有效参数（缺少 URL）时会输出帮助信息并退出。

## 输出示例

压测过程中每秒打印一行实时速率：

```
speed-go v1.0.0
开始压测: https://example.com/file.bin
  连接数: 5 | 时长: 60s | 忽略证书: false
  UA: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36
[15:54:42] 速率:   97.50 MB/s | 总流量: 486.15 MB | 请求数: 5(错误0)
[15:54:43] 速率:   97.50 MB/s | 总流量: 972.31 MB | 请求数: 10(错误0)
[15:54:44] 速率:  102.10 MB/s | 总流量: 1.48 GB | 请求数: 15(错误0)
```

压测结束（或 `Ctrl+C` 中断）后输出结果统计：

```
================ 压测结果 ================
  (用户手动中断)
  压测时长:   2.003s
  总流量:     3.00 GB (3222536192 bytes)
  平均速率:   1533.96 MB/s
  完成请求:   31
  失败请求:   0
==========================================
```

## 说明

- 连接数通过 `MaxConnsPerHost` 严格限制，每个 worker 独立循环发请求，下载完成立即开始下一次
- 请求出错（连接失败、非 2xx/3xx 状态码等）计入错误数，worker 会小睡 200ms 后重试，不会中断压测
- 显式设置 `Accept-Encoding` 后 Go 不会做透明解压，因此统计的字节数即实际网络传输字节数
- 携带 `-referer` 时 `Sec-Fetch-Site` 自动设为 `cross-site`，更贴近真实浏览器跳转行为

## 发布

项目内置 GitHub Actions 发布流程（[.github/workflows/release.yml](.github/workflows/release.yml)）：

```bash
git tag v1.0.0
git push origin v1.0.0
```

在仓库 **Actions → Release → Run workflow** 手动运行（可选填版本号，留空用当天日期，会在当前分支 HEAD 上自动创建对应 tag）。CI 安装 upx 并编译两套产物——未压缩版（`gospeed-linux-amd64`、`gospeed-linux-mt7981-arm64`，MT7981 等 ARM64 路由器）与 upx 压缩版（同名加 `-upx` 后缀，体积缩小约 60%），各附带 `.sha256` 校验文件，自动创建 GitHub Release 并生成 Release Notes（基于 commits 与 PR）。本地运行 `./build.sh` 时默认用日期作为版本号。

## 许可

MIT
