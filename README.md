<!-- 文件用途：说明既定记忆抽取实验的工具、运行步骤和仍需人工提供的材料。 -->

# Memory Spike

遵循 [抽取实验方案](docs/MEMORY_EXTRACTION_SPIKE.md)，检查模型能否从聊天中提取准确、有证据的个人记忆。采用 Go 独立脚本，包含数据库只读取样、样本/金标准备、V0/V1 跑批和人工评分汇总。

**当前是工具准备完成，正式实验未执行。** U4/U5 合成样本已生成；仍缺 U1/U2/U3/U6 已同意的真实样本、人工金标、模型实际价格和结构化输出能力验证。不能据此宣布 Go。

## 你现在只需提供自己的聊天

在学校 AI 网站的“会话管理”选择愿意用于实验的聊天，导出 JSON，保存到 `D:\app\MemorySpike\private\incoming`。告诉代理文件名及同意使用的范围。之后由代理检查文件、取样和准备标注材料，详见 [提供数据](docs/提供数据.md)。一个人只算一份真实用户样本。

## 环境与命令

在 PowerShell 中进入 `D:\Repo\memory-spike` 后执行。脚本优先使用 PATH 中的 Go，未找到则使用已安装的 `D:\app\MemorySpike\sdk\go`（1.26.9）。缓存和测试临时文件放 D 盘。跨机器可直接在 `scripts/memory_spike` 使用 Go 1.26+ 执行对应 `.go` 文件。

```powershell
.\scripts\spike.ps1 test
.\scripts\spike.ps1 inspect -file 'D:\app\MemorySpike\private\incoming\你的导出.json'
```

以下是完整流程参考；已有输出不会被覆盖。当前已生成的 `samples/U4.jsonl`、`samples/U5.jsonl` 及合并的 `samples.jsonl` 不要重复创建。

```powershell
# 只生成方案允许的 U4/U5，不会伪造 U1/U2/U3/U6。
.\scripts\spike.ps1 fixtures -out-dir D:\app\MemorySpike\private\samples -now 2026-10-13T12:00:00Z

# 真实取样前：在当前进程设置 MEMORY_SPIKE_DSN（只读连接）、MEMORY_SPIKE_OWNER。
# owner 从获准账号核实；-sample 按真实用户分型选择；只有实际同意后才用 -consent。
.\scripts\spike.ps1 collect -sample U1 -consent -out D:\app\MemorySpike\private\samples\U1.jsonl

# 把真实样本加入后，使用新目录创建完整输入和待人工填写的 gold。
.\scripts\spike.ps1 prepare -samples-dir D:\app\MemorySpike\private\samples -out D:\app\MemorySpike\private\batch1\samples.jsonl -gold-dir D:\app\MemorySpike\private\batch1\gold

# 复制示例配置到私人目录，补齐实际模型/协议/能力证据/价格。示例不能直接正式开跑。
Copy-Item -LiteralPath .\configs\models.example.json -Destination D:\app\MemorySpike\private\models.json
.\scripts\spike.ps1 run -samples D:\app\MemorySpike\private\batch1\samples.jsonl -gold-dir D:\app\MemorySpike\private\batch1\gold -config D:\app\MemorySpike\private\models.json -check
```

`-check` 不读取 Key、不调用模型；未齐全时列出缺项并返回非零退出码。金标由真人独立填写，U2/U6 各需两份；格式见 [人工评分说明](docs/人工评分.md)。网站导出可能缺正式取样字段，目前检查工具只检查结构，不宣称能直接替代数据库采集。

检查就绪后，在当前进程的 `MEMORY_SPIKE_API_KEY` 中设置密钥（不要写入仓库或命令历史），运行：

```powershell
.\scripts\spike.ps1 run -samples D:\app\MemorySpike\private\batch1\samples.jsonl -gold-dir D:\app\MemorySpike\private\batch1\gold -config D:\app\MemorySpike\private\models.json -out D:\app\MemorySpike\private\runs\v0
.\scripts\spike.ps1 report -run-dir D:\app\MemorySpike\private\runs\v0 -out D:\app\MemorySpike\private\reviews\v0
# 人工完成 blocks.csv、run_reviews.csv 后才汇总。
.\scripts\spike.ps1 report -score -run-dir D:\app\MemorySpike\private\runs\v0 -reviews D:\app\MemorySpike\private\reviews\v0 -out D:\app\MemorySpike\private\scores\v0
```

每组合只请求一次；失败保留，不重试、不切换模式。模型输出、usage、费用估计与人工评分分开保留。均值排除合成样本，缺失评分保持待评，不自动给 Go。协议实现见 [实施对照](docs/实施对照.md)，已执行检查见 [准备结果](docs/MEMORY_EXTRACTION_SPIKE_RESULT.md)。

## 数据与版本

真实聊天、gold、raw 响应、包含原文的 manifest/评审材料都放私人目录，不提交 Git。路径检查拒绝任意 Git 仓库内的实验数据输出，也检查父目录符号链接。测试必须使用仓库外的临时目录，Windows 包装脚本已配置；如果整个盘符被初始化为 Git 仓库，应先解除该盘符的仓库状态，不能仅依靠忽略规则绕过数据隔离。

`prompts/v0.txt` 保留方案原始提示词；最多一次修订时另存 v1 并保留 v0 结果。`go.mod`/`go.sum` 是独立模块及其生成的依赖锁定记录，直接依赖只有 `go-mssqldb`。本仓库不修改 Chat、UI、UniAuth 或 open-platform 的生产代码。

协作约定：[工程协作规范](docs/工程协作规范.md)、[PR 撰写规范](docs/PR撰写规范.md)。
