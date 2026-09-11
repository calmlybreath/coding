# 完整输出格式

本文件定义完整分析报告结构。局部问题可以只输出相关部分，避免生成无意义的空表。

使用本 Skill 时，按以下完整结构输出。

## 0. 维度建模分析

### 0.1 原始业务字段

| 字段 | 当前含义 | 问题 |
|---|---|---|
| xxx | xxx | 是否语义混杂 / 是否过粗 / 是否强耦合 |

---

### 0.2 需要拆细的维度

| 原字段 | 拆分后维度 | 拆分原因 |
|---|---|---|
| xxx | A, B, C | 原字段混合多个业务含义 |

---

### 0.3 需要合并的强耦合维度

| 原维度组合 | 合并后模型 | 合并原因 |
|---|---|---|
| A + B | BusinessModel | 组合后才表达稳定业务语义 |

---

### 0.4 需要建立的层级维度

| 原维度 | 层级结构 | 原因 |
|---|---|---|
| xxx | Level1 -> Level2 -> Level3 | 上层决定大流程，下层决定细规则 |

---

### 0.5 需要拆分的业务上下文

| 决策点 | Context | 包含字段 |
|---|---|---|
| 购买 | BuyContext | xxx |
| 计价 | PriceContext | xxx |
| 税费 | TaxContext | xxx |
| 支付 | PaymentContext | xxx |
| 履约 | FulfillmentContext | xxx |

---

## 1. 业务决策点

列出当前需求中真正的业务决策点。

格式：

```text
- 决策点 1：
- 决策点 2：
- 决策点 3：
```

---

## 2. 事实维度

列出参与决策的事实维度。

格式：

| 维度 | 含义 | 可能取值 |
|---|---|---|
| xxx | xxx | xxx |

---

## 3. 维度角色分析

对每个决策点分析维度角色。

格式：

| 决策点 | 维度 | 角色 | 原因 |
|---|---|---|---|
| xxx | xxx | Context / Config / Rule / Strategy / State | xxx |

---

## 4. 规则识别

列出哪些逻辑是“能不能”。

格式：

| 规则 | 条件 | 结果 | 放置位置 | 原因 |
|---|---|---|---|---|
| xxx | xxx | allow / deny / error | Strategy 内部 / 独立 Rule / Validator | 是否跨策略复用、是否与策略一起变化 |

---

## 5. 参数识别

列出哪些逻辑只是“值不同”。

格式：

| 参数 | 索引维度 | 示例值 | 建模方式 |
|---|---|---|---|
| xxx | xxx | xxx | Config / Matrix |

---

## 6. 策略识别

列出哪些逻辑是真正的“怎么做”。

格式：

| 策略接口 | 分发依据 | 策略实现 | 原因 |
|---|---|---|---|
| xxx | xxx | xxx | 算法/流程/外部依赖不同 |

对包含多个方法的 Strategy / Policy，补充检查：

```text
- 是否表达同一业务能力或决策点？
- 是否因同一业务原因变化？
- 是否处于同一生命周期？
- 是否所有实现都必须提供全部方法？
- 是否混入价格、库存、履约等其他决策点？
```

---

## 7. 非正交维度处理

列出非正交关系及处理方式。

格式：

| 非正交关系 | 类型 | 推荐处理 |
|---|---|---|
| A × B | 非法组合 / 参数矩阵 / 叠加计算 / 互斥算法 | Rule / Config / Pipeline / Decision Table + Strategy |

---

## 8. 是否需要中间业务模型

如果多个维度共同决定算法，需要判断是否引入中间业务模型。

格式：

```text
是否需要：是 / 否

原因：
xxx

建议模型：
xxx
```

---

## 9. 推荐结构

输出推荐类结构。

格式：

```text
Context
RuleValidator
Resolver / DecisionTable
Policy / Strategy
Config
Pipeline Step
ProductTypeModule / ProductPolicyFactory（仅在多种能力必须成套提供时）
```

示例：

```text
TaxContext
TaxRuleValidator
TaxModelResolver
TaxPolicyFactory
TaxPolicy
TaxRateConfig
```

---

## 10. 应避免的设计

明确指出哪些设计不要做。

格式：

```text
不建议：
- xxx

原因：
- xxx
```

---

## Final Summary Template

分析结束时，用以下格式总结：

```text
本需求中：

- xxx 是事实维度；
- xxx 是参数；
- xxx 是规则条件；
- xxx 是策略分发条件；
- xxx 是非正交关系；
- 非正交关系应该用 xxx 处理；
- xxx 规则应内聚在策略内部 / 外置为通用 Rule；
- xxx 多方法策略满足 / 不满足单一抽象与同一变化原因；
- xxx 成套能力通过组合接口、构造校验、模块工厂和完整性测试收束；
- 不应该为 xxx 建策略；
- 推荐使用 xxx 结构。
```

---


## Golden Rules

### 1. Dimension Modeling Golden Rules

必须遵守：

```text
1. 不要默认业务给出的字段就是正确维度。
2. 一个字段如果表达多个业务问题，要拆细。
3. 多个字段如果总是一起表达一个业务语义，要优先派生中间模型；只有单独存在无意义时才直接合并。
4. 维度有父子分类时，不要平铺成超长枚举。
5. 上层维度用于流程分发，下层维度用于规则和参数。
6. 不同决策点使用不同 Context。
7. 不要把数据库 Entity 直接当所有策略的输入。
8. Context 应该是某个决策点需要的事实快照。
9. 先做维度建模，再判断 Rule / Config / Strategy。
10. 维度建错，后面的设计模式都会变形。
```
### 2. Pattern Selection Golden Rules

必须遵守：

```text
1. 不要一上来写策略模式。
2. 先找业务决策点。
3. 事实维度放 Context。
4. 数值差异用 Config。
5. 合法性判断用 Rule。
6. 算法或流程差异才用 Strategy。
7. 多因素叠加用 Pipeline。
8. 多维共同决定互斥算法用 Decision Table + Strategy。
9. 组合有独立业务语义时，才抽中间业务模型。
10. 不要为笛卡尔积组合创建策略类型。
11. 策略接口围绕动词或决策点，而不是大名词。
12. Rule 负责“能不能”，Strategy 负责“怎么做”，Resolver 负责“选哪个”。
13. Go 中优先使用小接口。
14. Go 中避免把 Resolver、Rule、Strategy 混在一个类型里。
15. Go 中优先让业务编排服务组合 RuleValidator、Resolver、Policy，而不是让 Policy 自己判断所有规则。
16. Strategy 追求单一抽象和高内聚，不机械限制为单一方法。
17. 策略专属且共同变化的前置校验可以内聚；跨策略通用规则应外置。
18. 不同决策点使用小接口，必须成套提供时使用组合接口或模块工厂收束。
19. 编译期检查只保证方法集；业务类型的能力完整性还需要构造校验和枚举覆盖测试。
```
