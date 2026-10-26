# NanoAPI

轻量级 Windows 接口测试工具。Go + Wails v2 + 系统 WebView2，单个 exe，前端无 npm 构建步骤，数据保存在一个 JSON 文件里。

NanoAPI 只保留日常调试最常用的能力：请求构造、环境变量、集合管理、响应查看、cURL 互转、备份迁移。没有账号、云同步、插件市场和遥测。

## 下载与安装

发布页会提供三个 Windows x64 产物：

| 产物 | 说明 |
| --- | --- |
| `NanoAPI-<版本>-x64.exe` | 便携版，双击运行 |
| `NanoAPI-<版本>-x64-installer.exe` | NSIS 安装包，带开始菜单/桌面快捷方式，缺少 WebView2 时会尝试安装 |
| `NanoAPI-<版本>-x64.msi` | WiX MSI 安装包，适合域部署/静默安装；要求机器已安装 WebView2 Runtime |

系统要求：Windows 10/11 x64。程序使用系统 WebView2；Windows 10/11 通常已自带，若没有可从 [Microsoft WebView2](https://developer.microsoft.com/microsoft-edge/webview2/) 安装。

发布版本由 `.github/workflows/release.yml` 自动构建并上传到 GitHub Releases。

## 功能

### 请求

- **请求标签页**：独立请求、脏状态和响应；支持新建、切换、关闭（含中键），关闭脏标签前确认；重启后恢复标签页与未保存状态，不保存响应体。
- **请求构造**：GET / POST / PUT / PATCH / DELETE / HEAD / OPTIONS；查询参数、请求头、请求体（JSON / text / x-www-form-urlencoded / multipart）。
- **参数导入**：从地址栏 URL 一键导入查询参数，保留没有值的 `?flag`。
- **文件上传**：multipart 文本字段 + 本地文件，支持系统文件选择框；路径支持 `{{变量}}`；单文件 20 MB，总大小 50 MB。
- **cURL 互转**：导入支持 `-X` / `-H` / `-d` / `--data-raw` / `--data-binary` / `--json` / `-F` / `-u` / `--oauth2-bearer` 等常用参数；导出时按当前环境解析变量，生成 Bash 风格命令。`-d @文件` 会读取文件内容，`--data-raw @x` 保留字面量。
- **Body 校验**：JSON 请求体实时校验；超过 512 KB 时跳过实时校验，避免卡顿。
- **认证与 Cookie**：无、Basic、Bearer Token、API Key（Header 或 Query）；Cookie 在本次运行期间自动保留，可在设置中清空。
- **请求右键**：在集合树中打开、展开/折叠、新建、重命名、复制、删除。

### 响应

- 状态码、耗时、大小、Content-Type、重定向次数、截断提示。
- JSON 语法高亮 / 原始文本切换；超过 1 MB 自动跳过美化和高亮。
- 图片预览；其他二进制以 Base64 展示。
- **保存响应体**：系统保存框写文件；优先使用 `Content-Disposition` 文件名，其次使用最终响应 URL 和 Content-Type，再退回请求名。二进制会从 Base64 还原为原始字节。
- **响应体右键**：全选、复制选中/全部、保存选中内容（`.txt`）或完整响应。
- **响应头**：表格展示；按名称/值实时筛选并显示匹配数；右键复制选中/全部响应头或保存为 `.txt`。
- **链路耗时**：DNS、TCP、TLS、首字节 TTFB、总耗时、远端地址。
- **大响应保护**：默认最多读取 20 MB；超过后截断并提示。

### 组织与数据

- **集合管理**：集合 → 文件夹 → 请求三级树；支持新建、重命名、复制、删除、拖拽排序/移入文件夹/移到顶层；禁止把文件夹放进自身的后代。
- **环境变量**：多环境切换，`{{变量}}` 插值 URL / 参数 / 请求头 / Body / 认证；全局变量作为兜底；支持 `.env` 风格 `KEY=value` 批量编辑，保留同名变量的启用状态和说明。
- **历史记录**：最近 200 条请求，点击回填；清空同步写入数据文件。
- **数据备份**：导出/导入集合与环境；导入生成新 ID、追加合并、环境重名自动加“（导入）”、全局变量按 key 合并。
- **编辑保护**：未保存修改显示在标题栏和标签页；切换标签页不丢修改。
- **外观与设置**：暗色/亮色主题、超时、跟随重定向、跳过 TLS 校验。

## 快捷键

| 快捷键 | 作用 |
| --- | --- |
| `Ctrl` + `Enter` | 发送当前请求 |
| `Ctrl` + `S` | 保存到集合 |
| `Ctrl` + `K` | 聚焦地址栏 |
| `Esc` | 关闭当前弹窗/菜单；响应头筛选框有内容时先清空筛选 |
| 双击分割条 | 恢复请求区/响应区默认比例 |

弹窗打开时发送和保存快捷键会被暂时屏蔽。

## 数据与备份

Windows 数据文件：

```text
%APPDATA%\NanoAPI\nanoapi.json
```

macOS/Linux 从源码运行时会使用 Go 的标准配置目录（`~/Library/Application Support/NanoAPI/`、`~/.config/NanoAPI/`）。

数据文件包含集合、环境、全局变量、设置、历史与打开的标签页；不包含响应体和请求计时结果。

- 文件损坏时会自动备份为 `nanoapi.corrupt-<时间戳>.json`，然后以空数据启动。
- 设置里的“数据备份”适合分享或迁移集合与环境；备份不包含历史、设置和标签页。
- 导入上限 32 MB；响应读取上限 20 MB；multipart 单文件 20 MB、总计 50 MB。

## 构建

### 环境要求

- Go 1.25+
- Wails CLI v2.17.0（与 `go.mod` 一致）
- Node.js 20+（只用于前端测试，不需要 npm install）
- NSIS（生成安装包）与 WiX Toolset v3（生成 MSI）

安装 Wails CLI：

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.0
```

### 开发模式

```powershell
wails dev -browser -skipbindings -s
```

### 只构建便携 exe

```powershell
wails build -platform windows/amd64 -s -skipbindings -webview2 browser
```

产物：`build/bin/NanoAPI.exe`。

`-s` 跳过前端构建：前端是原生 ES Module，直接由 `go:embed` 打进 exe。`-skipbindings` 跳过生成 wailsjs，前端通过运行时 `window.go` 调用绑定。

注入版本号：

```powershell
wails build -platform windows/amd64 -s -skipbindings -webview2 browser `
  -ldflags "-X main.appVersion=0.2.0"
```

### 构建便携 exe + NSIS + MSI

先安装打包工具（任选 Chocolatey）：

```powershell
choco install nsis -y
choco install wixtoolset --version=3.14.1 -y
```

然后运行：

```powershell
.\scripts\build-windows.ps1 -Version 0.2.0
```

脚本会：

1. 校验版本号为 `x.y.z`；
2. 临时把版本写入 `wails.json` 的 `info`（影响 exe 资源与 NSIS 元数据），构建后恢复；
3. 以 `-ldflags "-X main.appVersion=0.2.0"` 构建便携 exe；
4. 调用 Wails 的 `-nsis` 生成 NSIS 安装包；
5. 调用 WiX `candle` / `light` 生成 MSI；
6. 输出：
   - `build/bin/NanoAPI-0.2.0-x64.exe`
   - `build/bin/NanoAPI-0.2.0-x64-installer.exe`
   - `build/bin/NanoAPI-0.2.0-x64.msi`

> MSI 使用 WiX v3；NSIS 安装包由 Wails 内置模板生成。MSI 只包含 exe 和开始菜单快捷方式，安装前通过 LaunchCondition 检查 WebView2。需要完整安装体验时优先使用 NSIS 安装包。

## 测试

```powershell
# Go 格式、静态检查和全部单元测试
$unformatted = @(gofmt -l .)
if ($unformatted.Count -gt 0) { $unformatted; exit 1 }
go vet ./...
go test ./...

# 前端语法检查
Get-ChildItem frontend/dist/js -Filter *.js | ForEach-Object { node --check $_.FullName }

# 前端纯逻辑测试（Node 内置 test runner，不需要 npm install）
node --test frontend/tests/store.test.mjs frontend/tests/envbulk.test.mjs frontend/tests/download.test.mjs frontend/tests/headers.test.mjs
```

也可以在 `frontend/` 下执行 `npm test`；`frontend/package.json` 只声明 `"type": "module"` 和测试入口，没有 npm 依赖。

前端纯逻辑模块包括集合树移动、环境变量批量解析、响应文件名、响应头格式化/筛选等，测试不依赖浏览器。

### 本地 mock 服务

```powershell
go run ./cmd/mockapi -addr 127.0.0.1:8788
```

| 接口 | 说明 |
| --- | --- |
| `GET /users` | JSON 列表 |
| `GET /users/{id}` | 单个用户 |
| `POST /echo` | 回显方法、查询、请求头与请求体；multipart 会解析字段和文件 |
| `GET /status/{code}` | 指定状态码 |
| `GET /redirect` | 302 到 `/users` |
| `GET /binary` | PNG 二进制，可在响应区预览 |
| `GET /cookie/set` | 写入会话 Cookie |
| `GET /cookie/check` | 检查 Cookie |
| `GET /slow?ms=2000` | 延迟返回 |

## GitHub Actions 与发布

workflow：`.github/workflows/release.yml`。

### 触发方式

| 事件 | 行为 |
| --- | --- |
| push 默认分支（当前为 `master`） | 测试 → 构建 exe/NSIS/MSI → 按 `wails.json` 的 `info.productVersion` 自动创建/更新 Release 和 tag |
| push 非默认分支 / pull request | 只运行完整测试（Go、前端） |
| push tag `v*` | 测试 → 构建 exe/NSIS/MSI → 创建或更新对应 Release |
| Actions → Run workflow，填写版本号 | 测试 → 构建 → **创建 tag 和 Release** |

默认分支自动发布的版本来源是 `wails.json`：

```json
"info": {
  "productVersion": "0.1.0"
}
```

- 第一次推送默认分支会创建 `v0.1.0` tag 和 Release；
- 版本号不变时再次推送会更新同一个 Release 的 exe/安装包（不改 tag 指向）；
- 需要新版本时，把 `info.productVersion` 改成 `0.1.1`（并保持 `appVersion` 默认值同步更新，虽然 CI 会通过 `-ldflags` 覆盖运行版本），提交并推送默认分支；
- 也可以手动推 tag 或使用 Run workflow 指定版本。

手动发布：

1. 打开 GitHub Actions → `Test and Release` → `Run workflow`；
2. 填写版本，例如 `v0.2.0`；
3. workflow 先跑全量测试，再构建三个 Windows 产物，最后用 `gh release create --target <当前提交>` 创建 tag 和 Release。

也可以手动推 tag：

```powershell
git tag v0.2.0
git push origin v0.2.0
```

这种情况下 tag 已存在，workflow 会为该 tag 创建 Release；重复运行会更新已有 Release 的产物（`--clobber`）。

默认分支 push 的发布逻辑在 `release` job 中：`github.ref_name == github.event.repository.default_branch`。版本从 `wails.json` 读取，tag 为 `v<版本>`；`gh release create --target <提交>` 会在 tag 不存在时自动创建 tag。

### workflow 做了什么

- `test` job（windows-latest）：
  - `gofmt -l` 检查；
  - `go vet ./...`、`go test ./...`；
  - 所有前端 JS `node --check`；
  - 4 组 Node 纯逻辑测试。
- `release` job（默认分支 push / tag / 手动触发，且需要 test 成功）：
  - 默认分支 push 时从 `wails.json` 读取版本；tag 时使用 tag；手动触发时使用输入版本；
  - 按 `go.mod` 安装对应版本的 Wails CLI；
  - 安装 NSIS 与 WiX Toolset v3；
  - 调 `scripts/build-windows.ps1` 生成便携 exe、NSIS 安装包和 MSI；
  - 用 `GITHUB_TOKEN`（`contents: write`）创建/更新 Release 并上传：
    - `NanoAPI-<版本>-x64.exe`
    - `NanoAPI-<版本>-x64-installer.exe`
    - `NanoAPI-<版本>-x64.msi`

版本规则：`v主版本.次版本.修订号`，例如 `v0.2.0`。当前 MSI ProductVersion 需要纯数字三段版本，因此暂不支持 `v0.2.0-beta` 这类 tag。

当前未配置代码签名。下载 exe/安装包时 Windows SmartScreen 可能提示未知发布者；正式分发可在此 workflow 中增加证书签名步骤。

## 项目结构

```text
.
├── main.go                       # Wails 入口，嵌入 frontend/dist
├── app.go                        # App 绑定、发送请求、设置/文件等
├── app_test.go                   # App/请求层测试
├── go.mod / go.sum
├── wails.json                    # Wails 项目、exe 元数据
├── internal/
│   ├── bundle/                   # 集合/环境备份导入导出
│   ├── curl/                     # cURL 解析与生成
│   ├── interp/                   # {{变量}} 插值
│   ├── model/                    # 请求、集合树、环境、历史数据结构
│   ├── proxy/                    # HTTP 执行引擎
│   └── store/                    # JSON 持久化（原子写 + 防抖）
├── cmd/mockapi/                  # 本地 mock 服务
├── frontend/
│   ├── package.json              # ESM 标记 + 测试入口（无依赖）
│   ├── dist/
│   │   ├── index.html
│   │   ├── css/app.css
│   │   └── js/
│   │       ├── app.js            # 启动、全局事件、设置/备份弹窗
│   │       ├── bridge.js         # Wails 绑定封装
│   │       ├── store.js          # 状态、持久化、集合树移动
│   │       ├── tabs.js           # 请求标签页
│   │       ├── request.js        # 请求编辑器
│   │       ├── response.js       # 响应体/头/耗时、保存、右键菜单
│   │       ├── sidebar.js        # 集合树、环境、历史
│   │       ├── kv.js             # 键值对编辑器
│   │       ├── envbulk.js        # .env 批量变量解析/格式化
│   │       ├── download.js       # 响应保存文件名
│   │       ├── headers.js        # 响应头格式化/筛选
│   │       ├── menu.js           # 通用右键菜单
│   │       └── util.js           # DOM/格式化/剪贴板等工具
│   └── tests/                    # Node 内置 test runner 测试
├── build/windows/msi/NanoAPI.wxs # WiX MSI 定义
├── scripts/build-windows.ps1     # 本地/CI 打包脚本
└── .github/workflows/release.yml # 测试、构建、发布
```

## 架构说明

- 后端负责全部网络与文件处理：URL 合并、变量插值、认证、multipart、TLS、重定向、超时、计时、响应读取、备份和文件保存。
- 前端只负责编辑与渲染；纯逻辑拆在独立 ESM 模块中，用 Node test runner 覆盖。
- 请求发送与 cURL 导出共用 `proxy.BuildURL`，避免两套 URL 规则。
- 插值基于请求副本，历史记录保留 `{{变量}}`，不会写入解析后的密钥。
- 响应体保存在内存和标签状态中，不写数据文件。
- 数据文件采用临时文件 + 重命名原子写入，并带防抖；损坏时备份原文件。

## 常见问题

**Wails 不在 PATH**

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.0
$env:PATH += ";$(go env GOPATH)\bin"
```

**启动提示 WebView2 缺失**

安装 [WebView2 Runtime](https://developer.microsoft.com/microsoft-edge/webview2/)，或使用 NSIS 安装包。MSI 会在安装前检查 WebView2。

**`wails build -nsis` 没生成安装包**

Wails 在找不到 `makensis` 时只打印警告并继续。安装 NSIS 并把 `C:\Program Files (x86)\NSIS` 加入 PATH，或直接运行 `scripts/build-windows.ps1`。

**MSI 构建失败**

需要 WiX Toolset v3 的 `candle.exe` / `light.exe`：

```powershell
choco install wixtoolset --version=3.14.1 -y
```

**Release 失败**

- 版本必须是 `vX.Y.Z`；
- 手动 Run workflow 需要填写版本号；
- 仓库 Actions 权限需要允许 `contents: write`；
- tag 已存在但没有 Release 时，workflow 会创建 Release 并上传产物。

**数据文件损坏**

同目录下会生成 `nanoapi.corrupt-<时间戳>.json`。修复原文件或从备份导入；不要直接删除，以免丢失可恢复的数据。

**大响应/下载**

默认最多读取 20 MB，超过会标记“已截断”。保存的是实际接收到的部分；二进制响应会解码 Base64 后写入。

## 开发约定

- 不引入前端运行时依赖，不增加 npm 构建步骤；保持单 exe 分发。
- 新增纯逻辑优先拆到独立模块，并补 Node 测试；后端逻辑补 Go 测试。
- 请求/响应相关改动必须跑 `gofmt`、`go vet`、`go test`、JS 语法检查和前端测试。
- UI 和文档使用中文；错误信息尽量可直接定位问题。
- 分类目录：`internal/bundle` 只处理备份，`internal/curl` 只处理 cURL，避免根目录堆积功能文件。
