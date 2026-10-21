# NanoAPI

轻量级接口测试工具，定位类似 Postman，但只保留日常调试最常用的能力。

桌面原生窗口（Go + Wails v2 + 系统 WebView2），单文件 exe，**零运行时依赖**。

## 功能

- **请求构造**：GET / POST / PUT / PATCH / DELETE / HEAD / OPTIONS，
  查询参数表、请求头表、请求体（JSON / text / x-www-form-urlencoded）
- **认证**：无、Basic、Bearer Token、API Key（可注入请求头或查询参数）
- **响应查看**：状态码、耗时、响应大小、响应头表格、JSON 语法高亮 / 原始文本切换
- **链路耗时**：DNS、TCP 连接、TLS 握手、首字节分别计时
- **集合管理**：集合 → 文件夹 → 请求 三级树，支持新建、重命名、复制、删除
- **环境变量**：多环境切换，`{{变量}}` 插值（URL / 参数 / 请求头 / Body / 认证），另有全局变量
- **历史记录**：最近 200 条请求，点击可回填
- **其他**：暗色 / 亮色主题、超时设置、跟随重定向开关、跳过 TLS 证书校验

## 构建

需要 Go 1.23+ 与 Wails CLI：

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

构建 Windows 64 位版本：

```bash
wails build -platform windows/amd64 -s -skipbindings -webview2 browser
```

产物在 `build/bin/NanoAPI.exe`。

> `-s` 跳过前端构建：本项目的前端是不依赖 npm 的原生 ES Module，
> 直接由 `embed` 打进 exe，不需要任何前端工具链。

前端改动后需要重新执行上面的构建命令。

## 运行

```bash
build/bin/NanoAPI.exe
```

数据保存在：

```
%APPDATA%\NanoAPI\nanoapi.json     # Windows
~/Library/Application Support/NanoAPI/nanoapi.json   # macOS
~/.config/NanoAPI/nanoapi.json     # Linux
```

单个 JSON 文件，集合、环境、历史与设置都在里面，直接复制即可备份。
文件损坏时会自动备份为 `nanoapi.corrupt-<时间戳>.json` 并以空数据继续启动。

## 快捷键

| 快捷键 | 作用 |
| --- | --- |
| `Ctrl` + `Enter` | 发送当前请求 |
| `Ctrl` + `S` | 保存到集合 |
| `Ctrl` + `K` | 聚焦地址栏 |

## 开发

```bash
# 单元测试
go test ./...

# 开发模式：前端热重载，浏览器打开 http://127.0.0.1:34115
wails dev -browser -skipbindings -s
```

### 本地 mock 服务

联调时可以用自带的 mock 服务，不依赖外网：

```bash
go run ./cmd/mockapi -addr 127.0.0.1:8788
```

提供的接口：

| 接口 | 说明 |
| --- | --- |
| `GET /users` | 返回 JSON 列表 |
| `GET /users/{id}` | 返回单个用户 |
| `POST /echo` | 回显方法、查询参数、请求头与请求体 |
| `GET /status/{code}` | 返回指定状态码 |
| `GET /redirect` | 302 跳转到 `/users` |
| `GET /binary` | 返回 PNG 二进制 |
| `GET /slow?ms=2000` | 延迟指定毫秒后返回 |

## 代码结构

```
├── main.go                     # Wails 入口，嵌入前端资源
├── app.go                      # 绑定给前端的方法：加载/保存/发送请求
├── internal/
│   ├── model/                  # 数据结构：请求、集合树、环境、历史
│   ├── store/                  # JSON 持久化（原子写 + 防抖落盘）
│   ├── interp/                 # {{变量}} 插值
│   └── proxy/                  # HTTP 执行引擎（net/http）
├── cmd/mockapi/                # 本地 mock 服务
└── frontend/dist/              # 前端资源，无构建步骤
    ├── index.html
    ├── css/app.css
    └── js/{app,bridge,store,util,kv,request,response,sidebar}.js
```

后端负责全部请求处理逻辑（插值、认证、TLS、重定向、超时、计时），
前端只负责编辑与渲染，因此业务逻辑可以脱离 UI 单独测试。
