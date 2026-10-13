<!-- 文件用途：记录本人要求扩展历史、优先长期线索并由 AI 预标注后审阅的探索流程及边界。 -->
# AI 预标注与历史探索

用户明确要求助手预标注、本人最后审计，并扩大历史范围、增加长期记忆候选。因此这条流程不再是原实验方案的独立人工标注；原方案和严格 30 天采样保持不变，正式入口也不接受 AI 辅助结果。

## 本次数据

只读扫描本人已授权的本机 `sessions` 和 `archived_sessions`。初次扫描 1,168 个记录文件，整理出 3,188 条候选，记录时间覆盖 2026 年 5 月至 10 月。排除子代理及明确自动 turn；旧格式可能只有 user 角色和日志时间，不能声称这些全部已验证为真人输入或原始发言时间。探索工具为此保留 `source_basis`，页面要求本人核对。

代理通过候选检索、阅读和上下文判断，目的性挑选 41 条原话、33 个会话。其中 33 条有长期候选，预标注含 49 条 auto、5 条 temporary、1 条 sensitive 事实；另有 1 条语境不明的待定原话及 2 条非本人信息的对照。长期候选涉及明确身份、经历、协作习惯、学习方式、笔记约定与持续项目。旧项目加时间与范围，不把一次指令推成永久偏好，不从产品举例推断性别或关系。

这是**长期线索富集的探索集**，不代表自然聊天分布，不据此计算总体抽取成功率。日期跨度扩大、选样方式变化、AI 建议可见都偏离正式协议，不能拿它给出正式 Go 结论。

## 使用

打开 `http://127.0.0.1:8789` 或桌面“记忆标注”：读原话、建议和理由；有误直接修改，无误点“确认这条，下一条”。普通保存和跳题不算审阅确认。全部逐条确认后才能点“完成审阅并保存”。之后告诉代理“审完了”即可读取。

私人目录 `D:\app\MemorySpike\private\codex-curated-20261013`：

- `preview.json`：冻结 C2 输入和摘要，正式实验资格与对外模型传输同意均为 false。
- `ai-suggestions.json`：不可冒充人工的原始 AI 建议，包含作者、摘要、逐题理由、来源说明和未审阅的建议。
- `selection-audit.json`：候选来源映射、选择方式和计数。
- `annotation-review.json`：本人保存时创建的审阅结果。`marks.*.reviewed` 表示逐题确认，`annotation.method=ai_assisted` 保留辅助来源；`annotation.complete` 表示审阅是否完成。

完整候选与本次准备脚本保留在私人目录 `codex-discovery-20261013`。原始聊天只读，旧 `codex-preview-20261013-v2` 保留。私人内容、事实和建议不得提交 Git。没有调用学校或其他额外模型 API；建议由本次 Codex 会话依据用户授权读取后生成。

## 命令与实现

```powershell
# 只发现候选；输出路径必须是新的仓库外文件。
.\scripts\spike.ps1 discover -roots 'C:\Users\LiangYS\.codex\sessions;C:\Users\LiangYS\.codex\archived_sessions' -local-consent -out D:\app\MemorySpike\private\new-discovery\candidates.json

# preview 同目录存在匹配的 ai-suggestions.json 时自动进入审阅模式。
.\scripts\spike.ps1 annotate -preview D:\app\MemorySpike\private\codex-curated-20261013\preview.json -out D:\app\MemorySpike\private\codex-curated-20261013\annotation-review.json -port 8789
```

`discover` 与严格 `codex` 采样器分开；不放宽正式来源要求。新探索命令还过滤计划实施包装，增强无分隔符密码遮盖。原始建议与人工结果分离；保存、刷新和翻页不能自动接受建议。后端逐题检查 reviewed，保存时强制保留 AI 来源；正式 `Readiness` 拦截非独立人工方法。

自动测试覆盖明确用户/旧格式/子代理和自动 turn、无分隔符密码遮盖、建议不可自动完成、本人确认后的来源持久化及正式入口拒绝 AI 金标。浏览器验收使用独立虚构样本，不代替本人点击真实建议的确认按钮。
