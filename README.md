<!-- 文件用途：说明既定记忆抽取实验的工具、运行步骤和仍需人工提供的材料。 -->

# Memory Spike

遵循 [抽取实验方案](docs/MEMORY_EXTRACTION_SPIKE.md)，检查模型能否从聊天中提取准确、有证据的个人记忆。采用 Go 独立脚本，包含数据库只读取样、样本/金标准备、V0/V1 跑批和人工评分汇总。

**当前已完成工具准备和一份 Codex 本地聊天预览，正式实验未执行。** U4/U5 合成样本已生成。新增 `codex` 命令，能从本人授权的本地记录准备待核对的 C1 样本；它不发送消息给模型，也不替代学校平台 U1/U2/U3/U6。正式实验仍缺真实样本、人工金标、模型实际价格和结构化输出能力验证。

## 直接在页面里做标记

现在也支持[AI 预标注后由本人审阅](docs/AI预标注与历史探索.md)。按本人要求，从更广历史优先挑选长期记忆线索；当前 C2 为 41 条原话、33 个会话，其中 33 条含长期候选，共 49 条长期候选事实。每条附建议和理由，点“确认这条”才计入已审阅。当前电脑的审阅入口是 `http://127.0.0.1:8789`，桌面“记忆标注”快捷方式已指向这一批。旧 C1 样本和标注文件保留。

AI 辅助结果携带 `method=ai_assisted`，只用于探索；正式实验入口拒绝把它当作独立人工金标。样本与建议仍存仓库外，不调用学校 API。

每条事实还可单独标注“跨项目通用 / 仅某个项目 / 仅当前对话”，项目范围需填写名称；项目内长期约定仍可选择“值得长期记住”。旧数据的范围保持未知，已有保存进度兼容。这是审阅信息，尚未接入聊天端的范围过滤。

本机[标注工作台](docs/本地标注页面.md)也保留独立标注模式：一条原话一题，选择类别、划选具体事实，再点“保存，下一条”。可中途保存、恢复题号，全部完成后点“完成并保存标注”，无需手改 JSON。页面本身不调用模型。

```powershell
.\scripts\spike.ps1 annotate -preview D:\app\MemorySpike\private\codex-preview-20261013-v2\preview.json -out D:\app\MemorySpike\private\codex-preview-20261013-v2\annotation-review.json
```

服务运行后打开 `http://127.0.0.1:8787`；重启服务仍可继续已保存的进度。标注结果位于同一私人目录的 `annotation-review.json`，其中 `annotation.complete=true` 才表示人工提交完成。仅标注仍不构成正式实验结果。

## 你现在只需提供自己的聊天

可以先使用本人同意提供的 Codex 记录开展预实验，命令与限制见 [Codex 本地取样](docs/CODEX_LOCAL_PREVIEW.md)。本机已经生成待本人核对的 `D:\app\MemorySpike\private\codex-preview-20261013-v2\sample-review.md`，其中没有人工金标或模型评估结论。

学校平台的正式数据准备见 [提供数据](docs/提供数据.md)。网站导出入口目前只按代码核对，线上打开设置页会要求登录，不能保证当前账号有同样的入口。一个人只算一份真实用户样本。

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

# 真实取样前：在当前进程设置 MEMORY_SPIKE_SQL_DSN（只读连接）、MEMORY_SPIKE_OWNER。
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
