# Chirepk

Chirepk 是一个使用 Go 构建的 Web 自动排课工具，用于根据班级、课程、教师、课时和每日作息生成周课表。项目提供 Excel 导入、任课配置、自动排课、约束校验、课表微调、教师查询和 Excel 导出等完整流程。

> 本仓库只允许使用合成或脱敏数据。代码、文档、测试、截图、日志和提交记录中不得包含真实学校名称、人员姓名、联系方式、访问凭据或其他可识别信息。

## 功能

- 从 `.xlsx` 工作簿同时导入每日作息和任课课时。
- 根据导入结果动态生成班级、教师、课程和周课时统计。
- 支持普通课时、连堂课和无需教师的活动课程。
- 异步生成课表并显示任务进度与校验结果。
- 校验教师冲突、课时数量、连堂结构、专项时段配额和课程分布。
- 查询班级课表与教师行程，并在完整约束校验下调整或撤销课表。
- 将通过校验的排课结果导出为 Excel 工作簿。
- 支持可选的本地管理员登录和飞书 OAuth 登录。

## 技术架构

项目采用前后端分区和分层后端结构。浏览器资源会嵌入 Go 可执行文件，部署时仍可使用单个二进制文件。

```text
Browser
   |
   v
transport/http  ->  application  ->  domain
                         |             ^
                         v             |
                     scheduler      store.Repository
                         |
                     adapter/xlsx
```

```text
backend/
├── cmd/chirepk/                 # 服务启动与依赖装配
└── internal/
    ├── domain/                  # 领域模型、默认值和结构校验
    ├── application/             # 导入、排课、导出和调整用例
    ├── scheduler/               # 排课、分布校验和课表调整算法
    ├── store/                   # Repository 接口与内存实现
    ├── adapter/xlsx/            # Excel 输入输出适配器
    └── transport/http/          # HTTP API、认证和中间件
frontend/
├── embed.go                     # 静态资源嵌入边界
└── static/                      # HTML、CSS、JavaScript 和图片资源
start-feishu.ps1                 # 飞书模式启动脚本
```

主要边界如下：

- HTTP 层只处理认证、协议转换、状态码和响应格式。
- `application.Service` 负责编排业务用例。
- `domain` 与 `scheduler` 不依赖 HTTP 或具体存储实现。
- `store.Repository` 隔离持久化边界，后续可新增数据库实现。
- `adapter/xlsx` 集中处理工作簿格式，避免文件协议进入领域层。

## 环境要求

- Go 1.22 或更高版本
- 支持现代 JavaScript 的浏览器
- 不超过 20 MB 的 `.xlsx` 工作簿（需要导入时）

## 快速开始

应用不提供硬编码的默认账号。首次启动前，至少配置一种登录方式。

### 本地管理员登录

PowerShell：

```powershell
$env:CHIREPK_ADMIN_USERNAME = "示例管理员"
$env:CHIREPK_ADMIN_PASSWORD = "<设置高强度密码>"
go run ./backend/cmd/chirepk
```

macOS 或 Linux：

```bash
CHIREPK_ADMIN_USERNAME='示例管理员' \
CHIREPK_ADMIN_PASSWORD='<设置高强度密码>' \
go run ./backend/cmd/chirepk
```

服务默认监听 `:8080`。启动后访问 <http://localhost:8080>。

可以通过参数或 `PORT` 环境变量修改端口：

```bash
go run ./backend/cmd/chirepk -addr :8090
```

```powershell
$env:PORT = "8090"
go run ./backend/cmd/chirepk
```

### 构建运行

```bash
go build -o bin/chirepk ./backend/cmd/chirepk
./bin/chirepk -addr :8080
```

Windows 可将输出文件改为 `bin/chirepk.exe`。

## 认证配置

| 环境变量 | 必填条件 | 说明 |
| --- | --- | --- |
| `CHIREPK_ADMIN_USERNAME` | 使用本地登录时 | 本地管理员账号，必须与密码同时配置 |
| `CHIREPK_ADMIN_PASSWORD` | 使用本地登录时 | 本地管理员密码，不得提交到仓库 |
| `FEISHU_APP_ID` | 使用飞书登录时 | 飞书网页应用 App ID |
| `FEISHU_APP_SECRET` | 使用飞书登录时 | 飞书网页应用 App Secret |
| `FEISHU_REDIRECT_URL` | 使用飞书登录时 | 与飞书后台完全一致的 OAuth 回调地址 |
| `CHIREPK_SECURE_COOKIES` | HTTPS 部署建议设置 | 设为 `true` 后强制会话 Cookie 使用 `Secure` |
| `PORT` | 否 | 未传入 `-addr` 时使用的监听端口 |

只有三项飞书配置同时存在时，飞书登录才会启用；只有本地账号和密码同时存在时，本地登录才会启用。`GET /api/health` 始终公开，其余业务接口需要有效会话。

### 飞书 OAuth

以下示例使用保留域名，不代表任何真实部署地址：

```powershell
$env:FEISHU_APP_ID = "cli_example"
$env:FEISHU_APP_SECRET = "<从飞书开发者后台获取>"
$env:FEISHU_REDIRECT_URL = "https://schedule.example.test/api/auth/feishu/callback"
$env:CHIREPK_SECURE_COOKIES = "true"
./start-feishu.ps1 -Address ":8080"
```

飞书开发者后台需要配置：

- 网页应用入口：`https://schedule.example.test/api/auth/feishu/start`
- OAuth 重定向 URL：`https://schedule.example.test/api/auth/feishu/callback`

入口域名、协议和端口应替换为实际部署值，重定向 URL 必须与 `FEISHU_REDIRECT_URL` 完全一致。`FEISHU_APP_SECRET` 只能保存在运行环境或专用密钥管理系统中。

## Excel 导入格式

导入器读取所有未隐藏的工作表，并使用以下约定：

- `任课课时汇总` 工作表必须存在；名称同时包含“任课”和“课时”即可识别。
- `每日作息` 工作表必须存在，并作为当前配置的作息来源。
- 任课表标题行可位于前 20 行，标题行之后为班级数据。
- A 列为年级，B 列为班级标识，D 列为班主任。
- 每门课程占两列：教师列后必须紧邻标题为 `课时` 的列。
- 每日作息表必须包含 `时段安排`、`开始时间`、`结束时间` 和 `排课属性` 四列。
- `排课属性` 仅接受 `可排课` 或 `不排课`。
- 课时可以填写普通课时，例如 `4`，或“普通课时+连堂次数”，例如 `4+2`。
- 支持语文、数学、英语、道法、历史、地理、生物、物理、化学、音乐、美术、体育、社团、劳技和安全；常见课程别名会被归一化。
- 社团、劳技和安全按无教师课程处理，空白课时按 `0` 处理。

脱敏示例：

| A 列 | B 列 | D 列 | E 列 | F 列 |
| --- | --- | --- | --- | --- |
| 年级 | 班级标识 | 班主任 | 语文 | 课时 |
| 示例年级 | 示例班级 A | 教师甲 | 教师乙 | 4+1 |

实际工作簿需要继续提供其他课程的“教师列 + 课时列”组合。列顺序可以调整，但标题及其相邻关系必须保持一致。

## 使用流程

1. 登录应用并导入已经脱敏的 Excel 工作簿。
2. 检查并保存每日作息。
3. 逐班检查教师、普通课时和连堂次数，保存任课设置。
4. 在“开始排课”中创建任务并等待生成完成。
5. 查看自检报告、班级课表和教师行程。
6. 在约束允许时微调课表，必要时撤销最近一次调整。
7. 导出课表，并按照数据管理要求处置导出文件。

## HTTP API

认证接口：

| 方法 | 路径 | 访问要求 | 用途 |
| --- | --- | --- | --- |
| `POST` | `/api/auth/login` | 公开 | 本地管理员登录 |
| `GET` | `/api/auth/feishu/start` | 公开 | 发起飞书 OAuth |
| `GET` | `/api/auth/feishu/callback` | 公开 | 处理飞书 OAuth 回调 |
| `GET` | `/api/auth/session` | 公开 | 查询当前会话 |
| `POST` | `/api/auth/logout` | 公开 | 注销当前会话 |
| `GET` | `/api/health` | 公开 | 健康检查 |

业务接口均要求有效会话：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `GET` / `PUT` | `/api/config` | 读取或保存配置 |
| `POST` | `/api/config/reset` | 清空业务配置 |
| `POST` | `/api/import/xlsx` | 导入 Excel 工作簿 |
| `GET` | `/api/teachers` | 获取教师课时统计 |
| `GET` | `/api/preflight` | 检查是否具备排课条件 |
| `GET` / `POST` | `/api/runs` | 查询或创建排课任务 |
| `GET` | `/api/runs/{id}` | 查看排课详情 |
| `GET` | `/api/runs/{id}/adjustment-candidates` | 获取可用的课表调整方案 |
| `POST` | `/api/runs/{id}/adjustments` | 应用课表调整 |
| `POST` | `/api/runs/{id}/adjustments/undo` | 撤销最近一次调整 |
| `GET` | `/api/runs/{id}/export.xlsx` | 导出课表 |
| `DELETE` | `/api/runs/{id}` | 删除已结束的排课记录 |

## 开发与验证

```bash
gofmt -w backend frontend
go test ./...
go vet ./...
go build -o bin/chirepk ./backend/cmd/chirepk
```

测试覆盖排课生成、约束校验、Excel 导入导出、课表调整、并发版本检查、本地会话和飞书 OAuth 流程。

## 隐私与安全

- 默认配置和测试夹具只使用 `示例学校`、`示例班级`、`教师甲` 等合成占位符。
- 不要提交原始 Excel、导出课表、截图、日志、Cookie、App ID、App Secret、真实回调地址或本地密钥文件。
- 导入前应替换学校、年级、班级、教师和文件名中的可识别信息。
- 服务使用内存存储；配置、任务、排课结果和登录会话会在进程重启后清空。
- 公网部署必须使用 HTTPS，并通过反向代理补充访问控制、限流、安全响应头和日志脱敏。
- 飞书授权范围与可访问人员应在飞书开发者后台按最小权限原则配置。

## 已知限制

- 当前版本固定为每周五个教学日，可排容量由作息中的可排课时段动态计算。
- 仅支持 `.xlsx` 导入，不读取隐藏工作表。
- 当前只有单一的本地管理员凭据，不支持用户库、角色权限和细粒度授权。
- 当前没有持久化、审计日志、登录限流、会话共享和多实例协调能力。
- 飞书登录未在应用内维护额外的人员白名单，访问范围依赖飞书应用配置。
- 当前实现适合本地使用或受控环境验证，不应未经安全加固直接暴露到公网。
