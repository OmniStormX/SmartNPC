# GitHub Actions 失败模式 — SmartNPC 目录

`ci-doctor` skill 的参考目录。每条记录包含：
- **匹配标记（Marker）**：可在失败日志里 grep 的字符串或正则
- **分类（Category）**：compile / test / lint / dependency / environment / workflow / flake 之一
- **分诊（Triage）**：必须收集的最少信息
- **修复方向（Fix vector）**：典型解决思路

---

## Go 编译错误

- **匹配标记**：`: undefined:`、`cannot find package`、`syntax error`、`imported and not used`
- **分类**：compile
- **分诊**：定位文件 + 行号 + 所属 Go module（`smartnpc-mcp` 还是 `smartnpc-agent`）
- **修复方向**：
  - 缺少 import → 补上
  - 未使用的 import → 删除或改 `_` 别名
  - 跨模块依赖问题 → 检查 `go.work` 是否同步，跑 `go work sync`
  - `go get -u` 之后 SDK API 漂移 → 回退到上一版本，或适配新 API

## Go 测试失败

- **匹配标记**：`--- FAIL:`、测试输出里的 `panic:`、`FAIL\tgithub.com/...`
- **分类**：test
- **分诊**：
  - 从 `--- FAIL: TestXxx` 拿到失败的测试名
  - 阅读 FAIL 行上方的 assertion 信息
  - 本地复现：`go test -run TestXxx -v ./path/to/pkg`
- **修复方向**：
  - 真实 regression → 修被测代码
  - 测试脆弱（依赖时序、顺序）→ 加固测试（用 channel、确定性时钟）
  - MCP 工具 schema/jsonschema 漂移 → 同步 Input/Output struct + 测试 fixture

## Go vet / lint 失败

- **匹配标记**：`go vet` 输出非空、`printf format`、`composite literal uses unkeyed fields`
- **分类**：lint
- **修复方向**：直接按诊断改——vet 警告几乎总是对的

## 依赖漂移

- **匹配标记**：`missing go.sum entry`、`verifying ...: checksum mismatch`、`inconsistent vendoring`
- **分类**：dependency
- **修复方向**：
  - 在每个受影响的 module 跑 `go mod tidy`
  - 仓库根跑 `go work sync`
  - commit 更新后的 `go.sum` / `go.work.sum`

## C# 编译 / 测试失败

- **匹配标记**：`error CS\d+`、`Build FAILED`、`[xUnit.net]`、`Test Run Failed`
- **分类**：compile 或 test
- **分诊**：定位 `.cs` 文件 + 诊断码（CSxxxx）
- **修复方向**：
  - CS0246（类型未找到）→ 检查 `using` + 项目引用
  - SMAPI API 不匹配 → 检查 `manifest.json` 的 `MinimumApiVersion` 与本地 SMAPI 版本

## 环境 / runner 问题

- **匹配标记**：`Unable to locate executable`、`Resource not accessible by integration`、`Error: Process completed with exit code 137`（OOM）
- **分类**：environment
- **修复方向**：
  - 缺工具 → 加 `setup-go` / `setup-dotnet` 步骤
  - action 版本不对 → pin 到已知可用的 `@vN` tag
  - OOM → 拆分 test job、提升 runner 规格、或精简测试数据

## Workflow YAML 错误

- **匹配标记**：`Invalid workflow file`、`you have an error in your yaml syntax`、`Unrecognized named-value`
- **分类**：workflow
- **修复方向**：
  - 本地 lint：`python -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))"`
  - 注意 YAML 必须用空格缩进，不能用 tab

## Flake（瞬时不稳定）

- **匹配标记**：未改代码重跑就过；日志中含 network timeout、`i/o timeout`、`connection reset`、`429 Too Many Requests`
- **分类**：flake
- **修复方向**：
  - 第 1 次 flake → 用 `gh run rerun --failed <runId>` 重跑一次
  - 反复 flake → 加固测试（重试机制、mock 外部服务），或加 `t.Skip` 隔离 + 建跟踪 issue
  - **不允许**用 `continue-on-error: true` 来"消除" flake

---

## 给诊断脚本用的快速 grep 模式

```
FAIL\t                    → Go 测试失败
--- FAIL:                 → Go 测试失败（具体测试名）
^panic:                   → Go runtime panic
error CS                  → C# 编译错误
go: missing go.sum        → 依赖漂移
Error: Process completed  → workflow / runner 失败
Invalid workflow file     → workflow YAML 错
```
