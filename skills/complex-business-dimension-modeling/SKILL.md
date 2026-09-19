---
name: complex-business-dimension-modeling
description: 分析复杂业务中的维度边界、多维变化、非正交关系以及 Rule、Policy、Strategy 边界，帮助拆分过粗维度、识别强耦合组合、建立层级维度、拆分业务 Context，并判断逻辑应该建模为 Context、Rule、Policy、Config、Decision Table、Resolver、Pipeline、Strategy 或 State Machine。适用于优惠、价格、税费、支付、物流、权益、订单状态、发票、风控等复杂业务建模场景；当用户提到维度拆分、正交或非正交、组合爆炸、巨型 Context、规则与策略边界、复杂 if/else 或希望获得 Go 代码结构时，应优先使用本 Skill。代码示例默认使用 Go。
---

# 复杂业务维度建模

## 目标

围绕业务决策点分析变化，不直接围绕用户、地区、商品等业务名词套设计模式。

```text
事实是什么            -> Context
值是多少              -> Config / Matrix
单个判断条件          -> Rule（职责）
同一决策点的规则      -> Policy
选哪个模型或实现      -> Decision Table / Resolver
怎么算、怎么执行      -> Strategy
多个步骤依次叠加      -> Pipeline
状态如何合法流转      -> State Machine
```

核心原则：

1. 先判断当前复杂度是否值得拆分，再进入完整建模流程。
2. Rule 是规则语义，不等于独立类型；简单 Rule 直接收进同一决策点的 Policy。
3. 先校验维度边界，再选择设计模式。
4. 先识别决策点，再分析参与决策的事实维度。
5. 同一维度在不同决策点可以扮演不同角色。
6. 参数差异不等于 Strategy，限制条件也不等于 Strategy。
7. 多维组合有独立业务语义时，才派生中间业务模型。
8. 不为笛卡尔积组合创建策略类型。
9. Policy 承载一个决策点下的规则；Strategy 封装可替换的算法或执行流程。
10. Strategy 追求单一抽象和高内聚，不机械限制为单一方法。
11. 跨决策点的方法需要拆分；跨策略复用且独立变化的规则才需要外置。
12. 必须成套提供的多种策略能力，使用组合接口、构造校验、抽象工厂和完整性测试统一收束。

## 适用判断

先判断是复杂建模问题，还是一个实现内的局部差异。只有一个决策点、少量分支时，直接给最小实现建议，不展开完整维度矩阵。

同一份数据、同一条查询、同一调用方，只是取值口径或读法不同，默认合在一个实现里。不要因为接口“以后可能变胖”提前拆；等独立变化真实出现后再抽象。

## Reference 加载规则

根据问题按需读取，不要无条件加载全部 Reference：

| 问题特征 | 需要读取 |
|---|---|
| 维度过粗、强耦合、父子分类、巨型 Context | `references/dimension-and-context.md` |
| Rule、Policy、Config、Strategy 的职责不清，Strategy 方法边界或 Rule 放置不清 | `references/pattern-selection.md` |
| 非正交组合、配置矩阵、组合爆炸、多维选策略 | `references/non-orthogonal-dimensions.md` |
| 万能 Strategy、会员等级等典型设计问题 | `references/design-smells-and-examples.md` |
| 状态组合、生命周期、规则生效时间、历史快照 | `references/state-and-temporal-dimensions.md` |
| 需要 Go 接口、Resolver、Decision Table、Pipeline、组合接口或抽象工厂代码 | `references/go-implementation.md` |
| 需要输出完整分析报告 | `references/output-format.md` |

加载原则：

- 涉及维度拆分时，先读 `dimension-and-context.md`，再读其他相关文件。
- 涉及非正交关系时，同时读取 `pattern-selection.md`，避免把组合限制误做成策略。
- 涉及状态流转或历史规则时，读取 `state-and-temporal-dimensions.md`。
- 需要生成 Go 代码时，先完成业务建模，再读取 `go-implementation.md`。
- 用户只问局部问题时，只加载并输出相关部分。

## Analysis Workflow

### Step 0：明确输入与假设

收集业务目标、原始字段、关键分支、配置、状态和外部依赖。信息不足时区分：

- 已知事实；
- 合理假设；
- 待确认问题。

不要凭空补齐业务规则。

### Step 1：维度建模

先筛选，再分析。不要把所有输入字段都列成维度：只被读取、透传或展示，且不影响决策和变化的字段（如歌词、开播时间）是普通数据字段。

对剩下的字段回答：

1. 哪些字段混合多个业务含义，需要拆细？
2. 哪些字段总是共同表达稳定业务语义，需要派生或封装中间模型？
3. 哪些字段存在父子层级？
4. 哪些字段只属于某个决策点？
5. 是否把数据库 Entity 当成了通用 Context？

如果筛选后只是一个决策点内的少量差异，回到「适用判断」给最小实现。

需要详细判断信号和案例时，读取 `references/dimension-and-context.md`。

### Step 2：识别业务决策点

围绕明确问题建模，例如：

- 优惠是否可用？
- 优惠金额如何计算？
- 支付方式是否可用？
- 支付流程如何执行？
- 物流方式如何选择？
- 状态如何流转？

不要问“会员等级是不是策略”，而要问“会员等级在当前决策点中起什么作用”。

### Step 3：提取事实维度

只列参与决策或变化分析的事实，如会员等级、地区、渠道、商品形态、活动类型、支付方式、金额和状态。被读取或返回的普通数据字段无需列入。

### Step 4：判断维度角色

对每个“决策点 × 维度”分别判断：

| 作用 | 建模方式 |
|---|---|
| 描述当前事实 | Context |
| 查询比例、阈值、开关 | Config / Matrix |
| 判断条件 | Rule（职责） |
| 选择中间模型或实现 | Resolver / Decision Table |
| 算法、流程、外部依赖不同 | Strategy |
| 多个步骤可组合叠加 | Pipeline |
| 状态与事件决定合法迁移 | State Machine |

边界不清时读取 `references/pattern-selection.md`。

Policy 不是字段或维度角色。识别完规则后，把同一决策点的简单 Rule 直接收进 Policy；只有需要跨 Policy 复用、配置或独立编排时，才为 Rule 创建独立类型。

### Step 5：处理非正交关系

先判断关系本质：

| 关系 | 推荐方式 |
|---|---|
| 非法或不支持组合 | Rule |
| 公式相同，仅参数不同 | Config Matrix |
| 多个因素分别贡献结果 | Pipeline |
| 多维共同选择互斥算法 | Decision Table + Strategy |
| 组合形成稳定业务概念 | Intermediate Model + Resolver |
| 状态与事件决定合法迁移 | State Machine |

需要判断组合爆炸或中间模型时，读取 `references/non-orthogonal-dimensions.md`。
涉及多个状态组合或时间快照时，读取 `references/state-and-temporal-dimensions.md`。

### Step 6：设计结构

推荐编排：

```text
Context
  -> Policy（仅在存在独立业务决策时）
  -> Resolver / Decision Table
  -> Strategy
  -> Pipeline（仅在需要叠加时）
  -> Result
```

这套编排是复杂场景的可选结构，不要求每一层都创建类型。只有一个实现的接口、只调用一次的 Resolver、只有一步的 Pipeline，应优先退回普通方法或局部分支。

接口围绕一个明确动词或决策点设计。避免 `UserStrategy`、`RegionStrategy` 等万能接口。

Strategy 不强制单方法。多个方法只有同时满足以下条件才放在同一接口：

- 表达同一业务能力；
- 因同一原因变化；
- 处于同一生命周期；
- 每个实现都必须提供。

Rule 描述判断条件，Policy 承载一个决策点的规则，Strategy 执行算法或流程。简单 Rule 直接写进 Policy；需要跨 Policy 复用、配置或独立编排时才抽成 Rule 类型。Strategy 内只保留依赖实现细节、与实现共同变化的不变量。如果新增一个业务类型必须同时提供 Buy、Price、Inventory、Fulfillment 等多个独立决策点的能力，不要合并成万能 Strategy，也不要分散注册；使用 `ProductTypeModule` / `ProductStrategyFactory` 统一提供这些小接口，并通过构造校验和枚举覆盖测试检查完整性。需要 Go 结构时读取 `references/go-implementation.md`。

### Step 7：验证设计

检查：

- 新增维度取值需要修改哪些位置；
- 新增规则是否需要修改策略；
- 是否存在遗漏、冲突或未匹配组合；
- 是否出现策略类型组合爆炸；
- Context 是否包含无关字段；
- 是否能够独立测试 Rule、Resolver 和 Strategy；
- 同一 Strategy 的多个方法是否属于同一变化原因和生命周期；
- 同一决策点的简单 Rule 是否已收进 Policy，而不是各建一个类型；
- 新增业务类型所需的成套能力是否有统一装配入口。

同时检查是否拆过头：是否出现单实现接口、一条判断一个 Rule，或把同一份数据和查询拆进多个实现。

## 输出要求

简单场景默认只输出结论摘要和最小实现建议。确认存在多维组合或多个独立决策点时，再输出：

1. 业务决策点；
2. 必要的维度与 Context 分析；
3. 非正交关系处理；
4. 推荐结构和必要的 Go 骨架；
5. 应避免的设计与待确认问题。

需要正式完整报告时，读取并遵循 `references/output-format.md`。局部问题不要强制输出完整模板。

## 最终自检

- 是否先判断了场景是否值得展开完整建模？
- 是否把普通数据字段错当成维度，或把简单 Rule 都建成独立类型？
- 同一份数据、同一条查询、同一调用方的差异是否可以留在一个实现？
- 是否先做维度建模，再谈 Rule / Policy / Config / Strategy？
- 是否围绕业务决策点，而不是大业务名词建接口？
- 是否把参数差异误做成 Strategy？
- 是否把“能不能”塞进 Strategy？
- 是否明确非正交关系的本质？
- 是否保留原始事实，并仅在有稳定业务语义时派生中间模型？
- 是否避免笛卡尔积策略类型？
- 是否给出最小、可测试的结构？
- 是否把“Strategy 单一抽象”误解成“Strategy 只能有一个方法”？
- 是否用小接口复用、组合接口收束，并通过构造校验和枚举覆盖测试检查成套能力的完整性？

建模口诀：

```text
先问值不值得拆，再问怎么拆；
Rule 描述判断，Policy 承载规则决策；
一份数据一条查询，优先一个实现；
Strategy 单一抽象，不是单一方法；
简单 Rule 收进 Policy，复用 Rule 才外置；
小接口复用，组合接口收束；
成套能力用抽象工厂。
```
