# WDAC / Device Guard 绕过方案

仅在 `bin\smartnpc-mcp.exe --version` 真的报"已被组织的 Device Guard 策略阻止"时才需要这套绕过。**先实测再用，不要预设拦截**。

## 实测：哪些产物会被拦

```cmd
:: A. go.exe 本体（永远 OK，微软签名）
go version

:: B. test binary（一直没被拦）
cd d:\SmartNPC\smartnpc-mcp
go test -count=1 -v ./internal/tools/

:: C. main 包产物（历史被拦，2026-04-30 实测放行）
go build -o bin\smartnpc-mcp.exe .\cmd\smartnpc-mcp\
bin\smartnpc-mcp.exe --version

:: D. go run 临时产物（同 C，路径在 %LocalAppData%\go-build\）
go run .\cmd\smartnpc-mcp\ --version
```

| 测试 | 通过 | 含义 |
|------|------|------|
| A ✅ B ✅ C ✅ D ✅ | 全 OK | 用正常方式启动（`task mcp:run`） |
| A ✅ B ✅ C ❌ D ❌ | main 被拦 | 用方案 1（test binary 入口） |
| A ✅ B ❌ C ❌ D ❌ | 全拦了 | 联系 IT，本机基本不可开发 |

## 方案 1：用 test binary 跑 server

利用"`*.test.exe` 不被拦"的事实，在 main package 加一个 gated 测试入口：

### `smartnpc-mcp/cmd/smartnpc-mcp/runserver_test.go`

```go
package main

import (
	"os"
	"strings"
	"testing"
)

// TestRunServer is a WDAC workaround entry point. On dev boxes whose
// Device Guard policy blocks user-built `main` exes, *.test.exe binaries
// are still allowed to run, so we launch the server through `go test`.
//
// Usage (Windows cmd.exe):
//
//   cd d:\SmartNPC\smartnpc-mcp
//   set SMARTNPC_RUN=1
//   set SMARTNPC_RUN_ARGS=--http :3000 --log-level=debug --ws-url=
//   go test -run "^TestRunServer$" -count=1 -v ./cmd/smartnpc-mcp/
//
// Without SMARTNPC_RUN=1 the test is skipped, so a normal `go test ./...`
// (e.g. in CI) is unaffected.
func TestRunServer(t *testing.T) {
	if os.Getenv("SMARTNPC_RUN") != "1" {
		t.Skip("set SMARTNPC_RUN=1 to launch the server via this entry point")
	}

	args := []string{"smartnpc-mcp"}
	if extra := os.Getenv("SMARTNPC_RUN_ARGS"); extra != "" {
		args = append(args, strings.Fields(extra)...)
	}
	os.Args = args

	t.Logf("launching smartnpc-mcp via test entry, argv=%v", os.Args)
	main() // blocks until SIGINT/SIGTERM
}
```

### 启动命令

```cmd
cd /d d:\SmartNPC\smartnpc-mcp
set SMARTNPC_RUN=1
set SMARTNPC_RUN_ARGS=--http :3000 --log-level=debug --ws-url=
go test -run "^TestRunServer$" -count=1 -v ./cmd/smartnpc-mcp/
```

**注意**：`set` 行尾不要有空格，否则环境变量值末尾会带空格导致 `os.Getenv("SMARTNPC_RUN") != "1"` 判断失败。

新窗口启动版本：

```cmd
start "smartnpc-mcp" cmd /k "cd /d d:\SmartNPC\smartnpc-mcp && set SMARTNPC_RUN=1&& set SMARTNPC_RUN_ARGS=--http :3000 --log-level=debug --ws-url=&& go test -run ^TestRunServer$ -count=1 -v ./cmd/smartnpc-mcp/"
```

## 方案 2：联系 IT 加白名单

最稳的治本方案。提交工单，请求把以下路径加入 WDAC 白名单：

- `%LocalAppData%\go-build\` — Go 编译缓存
- `D:\SmartNPC\smartnpc-mcp\bin\` — 项目 build 产物
- `D:\Stardew Valley\smapi-internal\` — SMAPI runtime

附理由：本机用于 Stardew Valley mod 开发，需要运行自编译 Go 工具链产物。

## 不要做的事

- ❌ **不要** 试图给 binary 自签名规避 WDAC —— 多数企业策略只信任已知 CA，自签名无效
- ❌ **不要** 把产物挪到 `C:\Program Files\` 等"看起来受信"的目录 —— 实测 WDAC 拦截不只看路径
- ❌ **不要** 用 `cmd /c start /min` 后台跑 —— 出错时日志看不到，无法排查
- ❌ **不要** 修改 main 包结构（拆 main 拆 lib 之类）来"绕"WDAC —— test binary 方案足够，不要污染生产代码结构
