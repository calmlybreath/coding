# 状态维度与时间维度

## 何时读取

当需求涉及订单状态、支付状态、履约状态、生命周期、规则生效时间、历史数据重放或“当前值与下单时值不一致”时读取本文件。

## 1. 区分状态事实与状态机

状态字段是事实维度，状态机是管理合法迁移的行为模型，两者不是同一个概念。

```text
OrderStatus = PAID
```

表示当前事实；而：

```text
PENDING_PAYMENT + PaymentSucceeded -> PAID
```

表示状态迁移。

只有同时存在以下特征时才优先考虑 State Machine：

- 状态集合相对稳定；
- 事件驱动状态变化；
- 迁移存在明确的合法与非法边界；
- 迁移带有 guard、动作或副作用；
- 需要处理幂等、重试、补偿或并发。

如果只是根据状态判断某操作是否允许，把简单 Rule 直接写进对应操作的 Policy 即可。

## 2. 不要盲目合并多个状态

`OrderStatus`、`PaymentStatus`、`DeliveryStatus` 经常相关，但不一定应该合并成一个大枚举。

直接合并可能产生：

```text
WAIT_PAY_NOT_SHIPPED
PAID_NOT_SHIPPED
PAID_SHIPPING
PAID_DELIVERED
REFUNDING_SHIPPING
REFUNDED_RETURNING
...
```

这会把多个可独立变化的生命周期做成笛卡尔积状态。

优先判断：

1. 各状态是否由不同事件驱动；
2. 是否由不同服务或聚合负责；
3. 是否存在各自独立的终态；
4. 一个状态变化是否必然导致另一个状态变化；
5. 组合后是否形成稳定且被业务直接使用的阶段概念。

若生命周期可独立变化，应保留多个状态机，并通过 Policy 或 Saga/Process Manager 协调。若组合稳定表达业务阶段，可额外派生 `OrderPhase`，但不要丢失原始状态。

```text
OrderStatus + PaymentStatus + DeliveryStatus
    -> OrderPhaseResolver
    -> OrderPhase
```

`OrderPhase` 是派生维度，不应成为所有原始状态的唯一数据源。

## 3. 状态迁移表

状态机至少应显式描述：

| 当前状态 | 事件 | Guard | 新状态 | 动作 |
|---|---|---|---|---|
| PENDING_PAYMENT | PaymentSucceeded | 金额一致 | PAID | 记录支付单 |
| PAID | Ship | 库存已锁定 | SHIPPING | 创建物流单 |
| SHIPPING | ConfirmReceipt | 已签收 | COMPLETED | 结算 |

还需要定义：

- 未知事件如何处理；
- 重复事件是否幂等；
- 并发迁移如何避免覆盖；
- 动作失败时回滚、重试还是补偿；
- 是否允许管理员修正状态；
- 状态迁移如何审计。

## 4. 时间是独立维度

复杂业务中至少区分：

| 时间 | 含义 |
|---|---|
| EventTime | 业务事件实际发生时间 |
| ProcessTime | 系统处理事件的时间 |
| EffectiveTime | 规则或配置开始生效的时间 |
| ExpireTime | 规则或权益失效时间 |
| SnapshotTime | Context 中事实的快照时点 |

不要使用一个模糊的 `Time` 或 `UpdatedAt` 同时承担这些语义。

## 5. 当前事实与历史快照

分析决策时必须明确读取当前值还是历史快照。

例如：

```text
会员当前等级：Gold
下单时会员等级：Silver
退款时应按哪个等级解释优惠？
```

常见原则：

- 定价、优惠、税费通常保存决策时快照；
- 展示当前用户信息通常读取实时值；
- 退款、重算、审计需要明确沿用历史规则还是使用新规则；
- 配置化决策应记录配置版本或命中规则 ID。

Context 应表达快照语义：

```go
type PriceContext struct {
    MemberLevel  MemberLevel
    SnapshotTime time.Time
    RuleVersion  string
}
```

## 6. 时间相关的非正交关系

时间经常与其他维度形成条件依赖：

```text
Region × ProductCategory × EffectiveTime -> TaxRate
MemberLevel × SnapshotTime               -> DiscountRate
OrderStatus × EventTime                  -> AllowedTransition
```

处理方式：

- 相同公式、值随时间变化：版本化 Config / Effective-dated Table；
- 规则随时间变化：版本化 Decision Table；
- 状态由事件推动：State Machine；
- 需要历史重放：保存事实快照、规则版本和命中依据。

## 7. 自检

- 状态字段和状态机是否被混为一谈？
- 是否把多个独立状态做成组合枚举？
- 派生阶段是否保留原始状态和推导依据？
- 是否区分事件时间、处理时间和生效时间？
- Context 使用的是实时事实还是历史快照？
- 历史订单是否能够按原规则解释或重放？
- 状态迁移是否处理幂等、并发、失败和审计？
