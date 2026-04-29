---
name: ci-doctor
description: 当用户要求检查、诊断或修复 SmartNPC 项目的 GitHub Actions CI 运行结果时使用此 skill。触发词包括 "看 CI"、"check ci"、"Actions 挂了"、"ci 失败"、"修复 ci"、"ci 怎么样了"、"ci status" 等。提供一套确定性 SOP：通过 gh CLI 拉取最近一次运行状态、对失败进行分类、提出修复方案、并循环验证直到 CI 变绿。
---

# CI Doctor

诊断并修复 SmartNPC 项目的 GitHub Actions CI 失败。

## 何时使用

应当使用：
- 用户在 push 之后说 "看 CI" / "check ci" / "ci 怎么样了"
- 用户报告 Actions 显示红色或贴出失败日志
- 自动 commit + push 完成之后，必须验证 CI 状态

不应使用：
- 距离上一次 CI 变绿之后没有任何新提交（直接问用户而不是猜测）
- 仓库里没有 `.github/workflows/`（CI 还没搭起来；先去搭 CI）

## 前置检查

在任何诊断动作之前先核查环境：

1. 确认 `gh` CLI 已安装：`gh --version`。缺失则提示用户运行 `winget install GitHub.cli` 后接 `gh auth login`，然后停止诊断。
2. 确认已认证：`gh auth status`。未认证则提示 `gh auth login`，然后停止诊断。
3. 确认当前工作目录是仓库根（含 `.git/`）。

## 诊断 SOP

严格按以下步骤执行，不允许跳步。

### 步骤 1 — 拉取最近一次 run

运行辅助脚本获取结构化快照：

```cmd
python .codebuddy\skills\ci-doctor\scripts\fetch_run.py --limit 1
```

脚本输出 JSON，包含 `runId`、`status`、`conclusion`、`event`、`headBranch`、`headSha`、`displayTitle`、`url`。

判断分支：
- `conclusion == "success"`：用一行字汇报 PASS + run URL + 耗时，结束。
- `conclusion in ["failure", "cancelled", "timed_out"]`：进入步骤 2。
- `status == "in_progress"`：告知用户 run 仍在执行，建议 N 分钟后重查（按典型耗时估算），不要阻塞。

### 步骤 2 — 拉取失败 job 的日志

```cmd
gh run view <runId> --log-failed > .codebuddy\skills\ci-doctor\.tmp_failed.log
```

然后用 `read_file` 工具读取该日志，定位失败标记。参考 `references/failure-patterns.md` 中已知的失败模式与快速分诊建议。

### 步骤 3 — 失败分类

把日志与 `references/failure-patterns.md` 中的类别对照，归入下列之一：

- **编译错误（compile）** — Go build / dotnet build 失败
- **测试失败（test）** — `--- FAIL:` 或 `[xUnit]` 红
- **静态检查失败（lint）** — `go vet` 报错或 schema 违规
- **依赖漂移（dependency）** — `go.sum mismatch` / `dotnet restore` 失败
- **环境问题（environment）** — runner OS 问题、action 版本未 pin、secret 缺失
- **Workflow 配置（workflow）** — YAML 语法错、path filter 不匹配
- **Flake（瞬时不稳定）** — 本地能过 + 偶发（网络、race），重跑就好——只能在重跑过之后才能下此结论

回复中**必须显式说出**归类。

### 步骤 4 — 给出修复方案

`failure-patterns.md` 对每种分类给出推荐的修复方向。把诊断结果按下列格式汇报给用户：

```
CI 失败汇报
- run: <url>
- 失败 job: <jobName>
- 分类: <category>
- 根因: <一句话>
- 建议修复:
  - <要点1>
  - <要点2>
```

如果修复很小（≤ 5 行、单文件、机械改动），直接动手不必再问。否则等待用户确认。

### 步骤 5 — 修改、验证、重新 push

1. 用标准编辑工具改代码。
2. 本地跑 `task ci`。失败必须停下来——禁止 push 红的代码。
3. 按 `git-workflow` 规则 commit（用户标记为"自动 commit"时使用 Claude 身份）。
4. `git push`。
5. 回到步骤 1，循环直到 success 或第 3 次仍失败。**第 3 次失败之后必须停止自动修复**，把所有失败信息汇总给用户。

## 反模式（绝对不做）

- 不修改 `.github/workflows/` 来掩盖失败（例如 `continue-on-error: true`、删掉失败 job）
- 同一个失败"原样重跑看看是不是 flake"不允许超过 1 次
- 不用 `git push --force` 改 CI 历史
- 不在没有看到 `gh run view` 的 `conclusion: success` 输出之前声称 CI 已绿

## 内置资源

| 路径 | 用途 |
|------|------|
| `scripts/fetch_run.py` | 把 `gh run list/view` 包装成结构化 JSON，便于解析 |
| `references/failure-patterns.md` | 失败分类目录 + 分诊提示 |
