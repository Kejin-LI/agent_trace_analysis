# Agent Trace 诊断平台 PRD

> 版本：v1.0  ｜  日期：2026-05-25  ｜  状态：草案，待评审
> 关联方案：[多 Agents 系统「步骤评估与时延归因」执行方案](./2026-05-25-multi-agent-step-evaluation-plan.md)

---

## 一、产品定位

### 1.1 一句话定义
**面向 Agent 研发同学的"诊断闭环"工具**——把多 Agent 系统的对话日志、Trace 数据、性能指标整合为可视化的诊断平台，让"哪里慢、为什么慢、怎么优化"在一个页面里说清楚。

### 1.2 与常见 Trace 平台的差异化

| 维度 | 常规 Trace 平台 | 本平台 |
|---|---|---|
| 定位 | 日志可视化 | 诊断闭环（看见 → 诊断 → 验证） |
| 主视图 | 树形日志 / Span 列表 | 对话流 + DAG 甘特图（关键路径高亮） |
| 排序 | 按时延 / 时间 | 按"诊断价值分"（异常度 × 影响面 × 新颖度 × 可优化度） |
| AI 分析 | 无 / 一键问答 | 三段式诊断报告（定位/根因/建议）+ 量化收益 + 反馈回流 |
| 模式发现 | 无 | 自动跨 Session 聚类，发现系统性问题 |
| 评估闭环 | 无 | 金标准库 + 评委一致率周报，AI 评委越用越准 |

### 1.3 核心使用人群
- **主要**：Agent 研发同学（信息密度高、技术细节默认展开）
- **次要**：值班 SRE / 运维（可选简化模式）

---

## 二、核心决策一览

| 维度 | 决策 |
|---|---|
| 数据量级 | < 1 万 session / 天 → 单库 PostgreSQL + JSONB |
| 权限 | P1 全开，不做多租户 |
| AI 模型 | 混合策略：常规诊断用内部模型，深度/聚类分析用 Claude Opus |
| 金标准库 | P1 完整闭环（标记 → 周报 → 校准） |
| UI 风格 | 苹果毛玻璃质感（详见 §6 视觉规范） |
| 跨 Session 聚类 | P1 进首版，离线每日跑一次 |

---

## 三、功能清单（P1 首版）

### 3.1 看板首页

| 区域 | 内容 | 交互 |
|---|---|---|
| 顶部状态栏 | 项目切换、搜索、时间范围筛选 | 切换刷新整个看板 |
| 健康度雷达图 | 5 维度：时延 / 重试 / 成本 / ROI / 基础设施 | 点击维度跳转对应详情 |
| Top 3 问题卡片 | 今日最值得关注的 3 个问题（聚类结果） | 点卡片进聚类详情页 |
| 今日核心数 | E2E P50/P95/P99、总成本、总 session 数、ROI 中位数 | 数字下方挂迷你折线图 |
| 本周异常聚类 | 横向滚动卡片，每张展示一个聚类的摘要 | 点卡片进聚类详情页 |
| 高危 Session 列表 | 按"诊断价值分"倒序，每行：ID / 时延 / 成本 / 异常类型 / 🤖 按钮 | 点行进 Session 详情；点 🤖 一键分析 |

### 3.2 Session 详情页

**三栏布局**（左右栏可折叠）：

| 栏 | 占比 | 内容 |
|---|---|---|
| 左栏：对话流 | 30% | 类 ChatGPT 多轮消息，每条消息下挂"🔍 看 DAG"小按钮 |
| 中栏：DAG 甘特图 | 50% | 横向时间轴，区分 Agent / Tool / MCP / Skill 形状；关键路径橙色描边；重试波浪线；排队灰色填充；时间轴可拖拽缩放 |
| 右栏：Span 详情 | 20% | Tab：Input / Output / Metadata / Token / Error；底部固定「🤖 AI 分析」按钮 |

**顶部操作栏**：返回 / 标金标准 / 一键 AI 分析 / 复制分享链接

### 3.3 AI 诊断报告（弹窗 / 侧抽屉）

**强制三段式结构**：

| 段落 | 必含字段 |
|---|---|
| 问题定位 | 引用具体 span（点击跳回 DAG 高亮）+ 量化数据（"span#7 重试 4 次，浪费 2300 token / $0.04"） |
| 根因假设 | 1-3 个候选 + 置信度（高/中/低）+ 判断依据 |
| 修复建议 | 每条建议含：① 预估收益 ② 修复成本 ③ 「📌 跟踪此问题」按钮 |

**附加能力**：
- 👍/👎 反馈按钮（用于持续微调 Judge prompt）
- 复制分享链接（带高亮 anchor）
- 持久化存储（同一 session 不重复消费 token）
- 触发位置：看板列表行 / 详情页右栏底部 / DAG 选中 span 后右键

### 3.4 聚类详情页

| 区域 | 内容 |
|---|---|
| 顶部摘要 | "32 个 session 卡在 database MCP，本周浪费 $14.8" |
| 代表样本 | 自动选 3 个（最快/中位/最慢），并排展示 DAG |
| 共性分析 | 自动列时间分布、调用工具、错误码等共性特征 |
| AI 诊断整个聚类 | 用 Opus，引用多样本作为证据 |
| 跟踪入口 | 「📌 标记为 Issue」长期跟踪 |

### 3.5 金标准库

| 功能 | 说明 |
|---|---|
| 标记入口 | Session 详情页「📌 标金标准」按钮，弹标注表单（正确/错误/浪费 + 备注 + tag） |
| 金标准列表 | 独立 Tab，按 tag/项目/时间过滤 |
| 相似检索 | 新 session 自动找最相似金标准（基于 DAG 拓扑），并排对比 |
| 评委一致率周报 | 每周一邮件：本周 LLM-as-Judge 与人工的一致率，<85% 高亮提醒 |

---

## 四、数据库设计（PostgreSQL + JSONB）

### 4.1 设计原则
- 元数据走结构化字段（便于索引、聚合）
- 大体积内容（trace、报告、prompt）走 JSONB 或对象存储引用
- 所有时间戳精确到毫秒
- 软删除（保留审计能力）

### 4.2 核心表

#### `sessions` — 会话主表

| 字段 | 类型 | 说明 |
|---|---|---|
| id | BIGSERIAL PK | 自增主键 |
| session_id | VARCHAR(64) UNIQUE | 业务侧会话 ID |
| project_id | VARCHAR(64) | 所属项目 |
| user_id | VARCHAR(64) | 提问用户 |
| title | TEXT | 自动摘要的会话标题 |
| message_count | INT | 消息数 |
| started_at | TIMESTAMP(3) | 会话开始 |
| ended_at | TIMESTAMP(3) | 会话结束 |
| total_duration_ms | BIGINT | 总耗时 |
| total_tokens_input | BIGINT | 累计输入 token |
| total_tokens_output | BIGINT | 累计输出 token |
| total_cost_usd | DECIMAL(10,4) | 累计成本 |
| diagnostic_score | DECIMAL(5,2) | 诊断价值分（0-100） |
| status | VARCHAR(16) | normal / risky / critical |
| created_at / updated_at / deleted_at | TIMESTAMP | 审计字段 |

索引：`(project_id, started_at)`、`(diagnostic_score DESC)`、`(status)`

#### `messages` — 单轮 Query 表

| 字段 | 类型 | 说明 |
|---|---|---|
| id | BIGSERIAL PK | |
| message_id | VARCHAR(64) UNIQUE | trace_id |
| session_id | VARCHAR(64) FK | |
| role | VARCHAR(16) | user / assistant |
| content | TEXT | 消息内容 |
| started_at / ended_at | TIMESTAMP(3) | |
| duration_ms | BIGINT | 端到端耗时 |
| tokens_input / tokens_output | INT | |
| cost_usd | DECIMAL(10,4) | |
| critical_path_ms | BIGINT | 关键路径长度 |
| queue_ratio | DECIMAL(5,2) | 排队占比 |
| retry_count | INT | 该轮内总重试次数 |
| wasted_token_ratio | DECIMAL(5,2) | 浪费 token 比例 |
| created_at | TIMESTAMP | |

索引：`(session_id)`、`(message_id)`

#### `spans` — Span 明细表

| 字段 | 类型 | 说明 |
|---|---|---|
| id | BIGSERIAL PK | |
| span_id | VARCHAR(64) UNIQUE | |
| message_id | VARCHAR(64) FK | trace_id |
| parent_span_id | VARCHAR(64) | 父 span，根为空 |
| service_name | VARCHAR(128) | Agent/Tool/Service 名 |
| span_kind | VARCHAR(16) | Internal / Client / Server |
| span_type | VARCHAR(16) | agent / tool / mcp / skill / llm |
| started_at / ended_at | TIMESTAMP(3) | |
| duration_ms | BIGINT | |
| status_code | VARCHAR(16) | Ok / Error / Unset |
| error_message | TEXT | |
| success | BOOLEAN | 业务成功标识 |
| retry_count | INT | |
| is_critical_path | BOOLEAN | 是否在关键路径上 |
| tool_name | VARCHAR(128) | |
| mcp_server_name | VARCHAR(128) | |
| mcp_tool_name | VARCHAR(128) | |
| skill_name | VARCHAR(128) | |
| skill_version | VARCHAR(32) | |
| gen_ai_model | VARCHAR(64) | |
| gen_ai_input_tokens | INT | |
| gen_ai_output_tokens | INT | |
| input_payload | JSONB | 输入内容（大体积转 TOS 引用） |
| output_payload | JSONB | 输出内容 |
| metadata | JSONB | 其他元数据 |

索引：`(message_id, started_at)`、`(parent_span_id)`、`(span_type)`、`(mcp_server_name)`、`(skill_name)`

#### `ai_reports` — AI 诊断报告表

| 字段 | 类型 | 说明 |
|---|---|---|
| id | BIGSERIAL PK | |
| target_type | VARCHAR(16) | session / message / cluster |
| target_id | VARCHAR(64) | |
| model_used | VARCHAR(64) | 内部模型 / claude-opus 等 |
| trigger_user_id | VARCHAR(64) | 触发人 |
| trigger_scope | VARCHAR(16) | full / partial（部分 span） |
| problem_summary | TEXT | 问题定位段 |
| root_causes | JSONB | 根因假设数组（含置信度） |
| suggestions | JSONB | 建议数组（含预估收益、成本） |
| evidence_spans | JSONB | 引用的 span_id 列表 |
| feedback_thumb | INT | 1=赞 -1=踩 0=未评 |
| feedback_comment | TEXT | |
| cost_usd | DECIMAL(10,4) | 生成本报告的成本 |
| created_at | TIMESTAMP | |

索引：`(target_type, target_id)`、`(created_at)`

#### `clusters` — 异常聚类表

| 字段 | 类型 | 说明 |
|---|---|---|
| id | BIGSERIAL PK | |
| cluster_id | VARCHAR(64) UNIQUE | |
| project_id | VARCHAR(64) | |
| signature | VARCHAR(255) | DAG 拓扑哈希 + 错误码 |
| title | TEXT | 自动生成摘要 |
| session_count | INT | 包含的 session 数 |
| total_wasted_cost_usd | DECIMAL(10,4) | |
| avg_p95_overhead_ms | BIGINT | 平均拉长的 P95 |
| representative_session_ids | JSONB | 代表样本数组（最快/中位/最慢） |
| common_features | JSONB | 共性特征（时段、工具、错误码） |
| trend | VARCHAR(16) | improving / stable / worsening |
| status | VARCHAR(16) | open / tracking / resolved |
| computed_at | TIMESTAMP | 最近一次计算时间 |
| created_at | TIMESTAMP | |

索引：`(project_id, computed_at)`、`(signature)`

#### `cluster_sessions` — 聚类与 Session 多对多

| 字段 | 类型 | 说明 |
|---|---|---|
| cluster_id | VARCHAR(64) FK | |
| session_id | VARCHAR(64) FK | |
| similarity_score | DECIMAL(5,4) | 与聚类中心的相似度 |
| created_at | TIMESTAMP | |

主键：`(cluster_id, session_id)`

#### `gold_standards` — 金标准库

| 字段 | 类型 | 说明 |
|---|---|---|
| id | BIGSERIAL PK | |
| session_id | VARCHAR(64) FK | |
| labeler_user_id | VARCHAR(64) | 标注人 |
| verdict | VARCHAR(16) | correct / wrong / wasteful |
| tags | JSONB | 标签数组 |
| comment | TEXT | 备注 |
| created_at / updated_at | TIMESTAMP | |

索引：`(session_id)`、`(verdict)`

#### `judge_calibrations` — 评委一致率记录

| 字段 | 类型 | 说明 |
|---|---|---|
| id | BIGSERIAL PK | |
| period_start / period_end | DATE | 统计周期 |
| project_id | VARCHAR(64) | |
| sample_count | INT | 样本数 |
| agreement_rate | DECIMAL(5,4) | 一致率 |
| llm_judge_model | VARCHAR(64) | |
| breakdown | JSONB | 按 verdict 分桶的一致率 |
| created_at | TIMESTAMP | |

#### `tracked_issues` — 跟踪问题表

| 字段 | 类型 | 说明 |
|---|---|---|
| id | BIGSERIAL PK | |
| source_type | VARCHAR(16) | session / cluster / report |
| source_id | VARCHAR(64) | |
| title | TEXT | |
| owner_user_id | VARCHAR(64) | |
| status | VARCHAR(16) | open / in_progress / resolved |
| created_at / resolved_at | TIMESTAMP | |

---

## 五、API 设计（RESTful）

> Base URL: `/api/v1`
> 认证：P1 不做（默认全开）；预留 `Authorization` header

### 5.1 看板相关

#### `GET /dashboard/overview`
返回看板首页所需的全部聚合数据。

**Query**: `project_id`, `time_range=24h|7d|30d`

**Response**:
```json
{
  "health_radar": {
    "latency": 78, "retry": 92, "cost": 65, "roi": 70, "infra": 88
  },
  "top_problems": [
    { "cluster_id": "...", "title": "...", "session_count": 32, "wasted_cost": 14.8 }
  ],
  "today_metrics": {
    "p50_ms": 1200, "p95_ms": 8400, "p99_ms": 15200,
    "total_cost_usd": 124.5, "session_count": 8420, "roi_median": 0.6,
    "p95_trend": [/* mini sparkline */]
  },
  "weekly_clusters": [/* 同 top_problems 结构 */]
}
```

#### `GET /sessions`
高危 Session 列表。

**Query**: `project_id`, `sort=diagnostic_score|started_at|cost`, `status=critical|risky|all`, `page`, `size`

**Response**:
```json
{
  "items": [
    {
      "session_id": "abc123",
      "title": "查询用户消费记录",
      "started_at": "...",
      "duration_ms": 12400,
      "cost_usd": 0.4,
      "diagnostic_score": 87.5,
      "status": "critical",
      "anomaly_tags": ["database_mcp_slow", "retry_storm"]
    }
  ],
  "total": 4280,
  "page": 1
}
```

### 5.2 Session 详情

#### `GET /sessions/{session_id}`
返回会话基础信息 + 消息列表。

#### `GET /sessions/{session_id}/dag`
返回该 session 的 DAG 数据（用于甘特图渲染）。

**Response**:
```json
{
  "spans": [
    {
      "span_id": "...", "parent_span_id": "...",
      "service_name": "Routing_Agent", "span_type": "agent",
      "started_at_ms": 0, "duration_ms": 320,
      "is_critical_path": true,
      "retry_count": 0,
      "status_code": "Ok"
    }
  ],
  "critical_path_span_ids": ["...", "..."],
  "total_duration_ms": 12400
}
```

#### `GET /spans/{span_id}`
返回单个 span 的详细 Input/Output/Metadata。

#### `GET /sessions/{session_id}/messages`
按时间顺序的消息列表（左栏对话流）。

### 5.3 AI 诊断

#### `POST /ai-reports`
触发 AI 诊断（手动一键）。

**Body**:
```json
{
  "target_type": "session",
  "target_id": "abc123",
  "scope_span_ids": null,
  "model_preference": "auto"
}
```

**Response**: 立即返回 `report_id`，异步生成；前端轮询或 WebSocket。

#### `GET /ai-reports/{report_id}`
返回报告内容。

#### `POST /ai-reports/{report_id}/feedback`
**Body**: `{ "thumb": 1, "comment": "..." }`

#### `GET /ai-reports?target_type=session&target_id=abc123`
列出某 target 的历史报告。

### 5.4 聚类

#### `GET /clusters`
**Query**: `project_id`, `status`, `sort=session_count|wasted_cost`, `time_range`

#### `GET /clusters/{cluster_id}`
返回聚类详情：摘要、代表样本、共性分析。

#### `POST /clusters/{cluster_id}/track`
将聚类转化为跟踪 Issue。

### 5.5 金标准

#### `POST /gold-standards`
**Body**:
```json
{
  "session_id": "abc123",
  "verdict": "correct",
  "tags": ["normal_baseline", "math_query"],
  "comment": "经典正例"
}
```

#### `GET /gold-standards`
列表查询。

#### `GET /sessions/{session_id}/similar-gold-standards`
返回最相似的 N 条金标准（基于 DAG 拓扑）。

#### `GET /judge-calibrations/latest`
返回最新一周的评委一致率周报。

### 5.6 跟踪问题

#### `POST /tracked-issues`
#### `GET /tracked-issues`
#### `PATCH /tracked-issues/{id}` — 更新状态/负责人

---

## 六、视觉规范（毛玻璃苹果风）

| 元素 | 规范 |
|---|---|
| 背景 | 渐变浅灰 `#F5F5F7 → #FAFAFA`，大块留白 |
| 卡片 | `backdrop-filter: blur(20px)` + `rgba(255,255,255,0.72)` 半透明白底 + `rgba(0,0,0,0.06)` 极淡边框 + 16px 圆角 |
| 阴影 | `0 8px 32px rgba(0,0,0,0.04)` 极轻、长扩散 |
| 字体 | SF Pro Display；标题 28-32px / 正文 14-15px / 辅助 12-13px 浅灰 |
| 强调色 | 主蓝 `#0071E3` / 告警橙 `#FF9F0A` / 成功绿 `#30D158` / 危险红 `#FF453A`（饱和度收敛） |
| DAG 配色 | 浅彩底 + 深色描边；关键路径橙色描边 + 微光晕；重试波浪线动画 |
| 形状语言 | Agent=圆角矩形 / Tool=矩形 / MCP=菱形 / Skill=六边形 |
| 交互过渡 | `200ms ease-out`；Tab 切换 crossfade；卡片展开 spring |
| 图标 | SF Symbols 或同等线性图标，描边 1.5px |

---

## 七、技术架构（轻量版）

```
前端: React + Tailwind + Framer Motion
      DAG: reactflow / dagre + d3
后端: Go (Gin + Gorm) — REST API + 异步任务（消息队列）
存储: PostgreSQL（元数据 + JSONB） + 对象存储（大 trace、报告） + Redis（缓存）
AI 调度: 路由器（简单→内部模型 / 深度→Opus） + Prompt 模板版本化 + 报告持久化
离线任务: 每日聚类（DAG LCS） / 每周评委一致率周报
```

---

## 八、P1 开发顺序（6 周）

| 周 | 内容 |
|---|---|
| W1 | 数据接入 + Session 列表 + 基础详情页（对话流 + JSON trace） |
| W2 | DAG 甘特图渲染（核心难点） |
| W3 | 看板首页 + 高危排序 + 健康度雷达 |
| W4 | AI 诊断报告（先内部模型跑通三段式） |
| W5 | 聚类引擎 + 聚类详情页 |
| W6 | 金标准库 + 周报 + 毛玻璃 UI 全面打磨 |

---

## 九、风险与对策

| 风险 | 对策 |
|---|---|
| DAG 图渲染性能（百级 span） | 虚拟化渲染 + 时间轴聚合层级；超 500 span 自动折叠 |
| AI 诊断成本失控 | 路由器分流 + 报告持久化；用户手动触发 + 频次限制 |
| 聚类质量不稳定 | 先用规则特征（DAG 哈希 + 错误码），效果不佳再上向量召回 |
| 金标准库样本不足 | 提供"批量标注模式"；周报反向激励团队补样本 |

---

## 十、待办与未来迭代

| 编号 | 内容 | 计划阶段 |
|---|---|---|
| F1 | 时间旅行回放（DAG 底部播放杆） | P2 |
| F2 | 聚类跨周趋势对比 | P2 |
| F3 | 多租户与权限 | P2 |
| F4 | 一键 A/B 实验 | P3 |
| F5 | 向量召回的语义相似检索 | P3 |
