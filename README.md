# transform

在当前服务器自己部署，照着 [操作步骤](https://github.com/Yinlerens/cloud-foundation/blob/main/docs/translate-deployment.md) 做。
第一次在 GitHub 仓库的 Actions Secrets 添加 `COHERE_API_KEY`，再到 Actions 的 delivery 页面点击 Run workflow，服务器会自动登记并部署服务。以后推送 main 分支就自动更新，全程不需要 SSH。

基于上级 `application-template` 改造的 Go 翻译微服务，调用 Cohere **North Small Translate**（`north-small-translate-1-0`），由模型自动识别原文语言，固定翻译成简体中文。保留模板的 `/api` 路由、健康检查、Prometheus、OpenTelemetry、请求追踪、非 root 容器、多架构构建、Helm 与供应链检查。服务无状态，无需数据库、worker 或前端。

## 本地运行

需要 Go 1.27.1 或更新版本，在 PowerShell 中运行：

```powershell
$env:COHERE_API_KEY = '<你的 Cohere API Key>'
go run ./cmd/transform
```

当前工作区已生成 Windows 可执行文件，未安装 Go 时可在设置上述环境变量后直接运行 `./bin/transform.exe`。修改代码后需重新构建该文件。

环境变量见 `.env.example`；程序不自动加载 `.env`，请由 shell、容器或部署平台注入。默认端口为 8080。未配置 Cohere 密钥时可启动，但 `/readyz` 和翻译接口返回 503。翻译接口无需客户端认证。

## 接口

### `POST /api/translate`

```powershell
$body = @{ '文本' = 'Hello, world!' } | ConvertTo-Json
Invoke-RestMethod -Uri http://localhost:8080/api/translate -Method Post -ContentType 'application/json; charset=utf-8' -Body ([System.Text.Encoding]::UTF8.GetBytes($body))
```

响应示例（ID、译文和用量由模型返回）：

```json
{
  "生成编号": "cohere-generation-id",
  "译文": "你好，世界！",
  "目标语言": "简体中文",
  "模型": "north-small-translate-1-0",
  "完成状态": "已完成",
  "用量": {
    "词元数量": { "输入词元": 18, "输出词元": 6 },
    "计费用量": { "输入词元": 18, "输出词元": 6 }
  }
}
```

只需提供 `文本`，无需指定原文语言或目标语言。原文由模型自行识别，响应的 `目标语言` 固定为 `简体中文`。兼容旧版输入字段 `text`，但响应仅使用中文字段。若同时传入 `文本` 和 `text`，两者必须相同。兼容接收 `目标语言` 或旧版 `target_language` 字符串字段，但这些字段不影响翻译目标。

`文本` 必填，最多 8000 个 Unicode 字符；请求体最多 64 KiB。原文换行和空白直接传给模型，无需额外调用语言检测接口。服务接受任意语言的输入文本；翻译质量受模型能力限制，官方支持的 52 种语言和地区变体可通过 `/api/languages` 查询，不能保证支持所有人类语言。上游上下文限制仍可能拒绝复杂或较长输入，请分段翻译。

错误统一为 `{"错误":{"类型":"文本无效","说明":"文本不能为空","请求编号":"..."}}`，类型和说明均为中文。无效输入为 400，输入过长为 413，非 JSON 为 415，上游拒绝输入为 422；上游故障/认证失败/译文截断为 502，未配置/上游限流为 503（转发 `Retry-After`），超时为 504。不会将被截断的译文当作成功结果，也不会自动重试模型调用。日志不记录原文、译文或密钥。

| 接口 | 用途 |
| --- | --- |
| `GET /api` | 服务、版本、环境与模型信息 |
| `GET /api/languages` | 中文字段 `支持语言`、`目标语言`，语言条目包含 `语言代码` 和中文 `名称` |
| `GET /healthz` | 进程存活 |
| `GET /readyz` | 本地配置就绪，不消耗模型额度、不保证上游即时可用 |
| `GET /metrics` | Prometheus 指标（保留 `foundation_http_*` 命名） |

所有响应带 `X-Request-ID` 与 `X-Release-Version`。客户端调用翻译、健康检查、指标及语言信息接口均无需提供访问令牌。服务通过 `COHERE_API_KEY` 调用 Cohere 模型。设置 `OTEL_EXPORTER_OTLP_ENDPOINT` 后启用 OTLP HTTP traces，支持 W3C `traceparent` 并追踪 Cohere 请求。应用 JSON 日志使用中文消息、级别和字段名（例如 `时间`、`级别`、`消息`、`请求编号`、`状态码`），默认应用、环境和版本分别显示为 `翻译服务`、`开发` 和 `开发版`。所有业务 JSON 响应字段、错误类型、提示、完成状态和语言名称均使用中文；客户端须按新字段读取响应。HTTP 头、接口路径、模型 ID、语言代码、版本摘要和 Prometheus 指标名保留技术协议标识。

## 配置

| 环境变量 | 默认值 / 说明 |
| --- | --- |
| `COHERE_API_KEY` | 必需，通过环境变量或 Secret 注入 |
| `COHERE_BASE_URL` | `https://api.cohere.com`，自动追加 `/v2/chat`；仅 localhost 允许 HTTP |
| `COHERE_MODEL` | `north-small-translate-1-0` |
| `COHERE_TIMEOUT` | `60s`，可设置为大于 0 且不超过 60 秒 |
| `COHERE_MAX_TOKENS` | `8192`，范围 1–16384；过低可能导致截断错误 |
| `HTTP_ADDR` | `:8080` |
| `APP_NAME` / `APP_ENVIRONMENT` / `RELEASE_VERSION` | `transform` / `development` / `development` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | 可选，模板集群默认指向 Alloy |

## 验证与镜像

```powershell
go vet ./...
go test ./...
go build -o bin/transform.exe ./cmd/transform
docker build -t transform:local .
docker run --rm -p 8080:8080 -e COHERE_API_KEY transform:local
```

CI 运行格式检查、`go vet`、race tests、构建、gitleaks、Helm 与 kubeconform；构建 AMD64/ARM64 镜像并做 Trivy 扫描和 SBOM。基础镜像和 GitHub Actions 均固定 digest/commit，Go 依赖由 `go.sum` 固定。

## Helm 与发布

`deploy` 沿用模板的 Gateway、Cilium、ServiceMonitor 约定，默认域名为 `transform.apps.makima.sbs`，镜像为 `ghcr.io/yinlerens/translate`，按实际仓库和集群覆盖。仅运行一个 API Deployment。探针与指标不通过公网路由；Cilium 只允许 DNS、遥测和 `api.cohere.com:443` 出站，调整上游地址时需同步 `externalDomains`。容器 UID/GID 10001，禁止提权，只读根文件系统，90 秒终止宽限期。HTTPRoute 请求/后端超时为 75/70 秒，以覆盖最长 60 秒的模型调用，需要 Gateway 支持 HTTPRoute timeouts。

当前系统的 `transform` 应用已经在平台登记，生产部署通过 GitHub Actions 更新现有 GitOps 配置。推送 `main` 或在 `delivery` 页面点击 **Run workflow** 都会检查、构建、签名、更新镜像 digest，并通过 Argo CD 自动部署。工作流不会创建新应用、namespace 或镜像拉取身份。

更换模型密钥时，编辑 GitHub 的 `COHERE_API_KEY` 再点一次 **Run workflow**；流程将密钥加密保存并触发服务更新。未提供新密钥时沿用集群已有 Secret。发布最后会执行一次真实翻译；变绿表示运行和模型调用都通过检查。发布授权和签名设置已经为此仓库准备好。

## 官方依据

- [North Small Translate 模型说明、支持语言及提示词示例](https://docs.cohere.com/docs/north-small-translate-1.0)
- [Chat V2：POST https://api.cohere.com/v2/chat](https://docs.cohere.com/reference/chat)

官方文档说明该模型可通过免费层 Chat V2 使用；实际账号权限和额度由 Cohere 决定。
