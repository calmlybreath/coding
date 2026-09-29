# Rule、Policy、Config 与 Strategy 边界

完成维度建模（见 `dimension-and-context.md`）之后，再判断一个逻辑该建模为 Context、Config、Rule、Policy、Strategy、Resolver、Decision Table 还是 Pipeline。**维度建错，模式选择全会变形。**

角色总表见 SKILL.md Step 4，本文件只讲边界判据、放置规则和设计异味。

---

## 1. 优先分析业务决策点，不是业务名词

不要先问：

```text
用户类型要不要策略？ 地区要不要策略？ 商品类型要不要策略？ 会员等级要不要策略？
```

应该先问：

```text
价格如何计算？ 优惠是否可用？ 优惠金额如何计算？ 税费如何计算？
支付方式是否可用？ 支付流程如何执行？ 物流方式如何选择？ 发票是否可开？
积分如何计算？ 权益如何发放？ 订单状态如何流转？
```

策略围绕一个明确的业务决策点，而不是一个大名词。

---

## 2. 事实维度（Context）

事实维度描述当前业务上下文"是什么"，通常放在 Context 中。常见：用户类型、会员等级、地区、渠道、商品类型、商品税类、活动类型、支付方式、订单金额、发票类型、账户主体、订单状态。

> 事实维度本身不等于策略。

---

## 3. Parameter / Config

某个维度只用来查数值、比例、阈值、开关时，它是参数索引。

```text
会员等级 -> 折扣率 / 积分倍率      地区 + 商品税类 -> 税率
活动 -> 满减门槛                   渠道 -> 功能开关
```

判断标准：

> **算法结构不变，只是值不同。**

```go
rate := memberConfig.DiscountRate(ctx.MemberLevel)
finalPrice := ctx.Amount.Mul(rate)
```

适合：配置表、参数表、配置矩阵、Feature Flag。**不要为纯参数差异创建策略类型。**

---

## 4. Rule

维度用于判断能不能、允不允许、是否适用、是否满足资格、是否拦截、是否支持某组合时，它是 Rule 职责。

```text
海外地区不能参加赠品活动     虚拟商品不能走物流
企业用户不能使用个人券       金卡以下不能领取生日券
黑名单用户不能下单
```

Rule 首先描述规则语义，**不表示每个条件都要创建接口或结构体**。同一决策点内的简单 Rule 直接写进 Policy。

判断标准：

> 分支结果是 error、true/false、allow/deny、eligible/ineligible，大概率属于 Rule 语义；**是否抽类型另行判断**。

只有当规则需要跨 Policy 复用、独立配置或编排时才用接口：

```go
type Rule[T any] interface {
    Match(ctx T) bool
    Validate(ctx T) error
}

type OverseasCannotUseGiftRule struct{}

func (r OverseasCannotUseGiftRule) Match(ctx OrderContext) bool {
    return ctx.Region == RegionOverseas && ctx.PromotionType == PromotionTypeGift
}

func (r OverseasCannotUseGiftRule) Validate(ctx OrderContext) error {
    return errors.New("海外地区不支持赠品活动")
}
```

也可以只保留 `Validate(ctx T) error`。

> 不要把限制条件建模成 Strategy。

---

## 5. Policy

Policy 是一个业务决策点的规则边界，负责规则顺序、短路和结果表达。简单 Rule 直接写进 Policy：

```go
type CouponEligibilityPolicy struct{}

func (p CouponEligibilityPolicy) Decide(ctx CouponContext) error {
    if ctx.MemberLevel.LowerThan(MemberLevelGold) {
        return errors.New("会员等级不足")
    }
    if ctx.Region == RegionOverseas {
        return errors.New("海外地区不适用")
    }
    return nil
}
```

Policy 回答"依据这些规则，最终是否允许或适用"。部分 Rule 需要复用或独立配置时，Policy 再组合这些 Rule 类型；**不要先把每个 `if` 都拆成 Rule struct**。

> Policy 不是维度角色，也不是算法实现。

---

## 6. Strategy

维度决定不同算法、流程、外部依赖或生命周期时，它可能是策略分发维度。

```text
活动类型 -> 满减、折扣、赠品算法不同
支付方式 -> 微信、支付宝、PayPal 外部接口不同
物流方式 -> 快递、自提、同城配送流程不同
订单状态 -> 不同状态下可执行动作不同
税务模型 -> 欧盟 VAT、美国销售税、大陆价内税算法不同
```

```go
type PromotionStrategy interface {
    Apply(ctx PromotionContext) (PromotionResult, error)
}

type DiscountPromotionStrategy struct{}
type FullReductionPromotionStrategy struct{}
type GiftPromotionStrategy struct{}
```

判断标准：

> 分支里是在执行不同的"怎么做"，而不是判断"能不能"，才考虑 Strategy。

---

## 7. Strategy 方法边界

Strategy 的核心约束是**单一抽象、高内聚、一致的变化原因**，不是"接口只能有一个方法"。

多个方法放在同一 Strategy 中，须同时满足：

1. 属于同一业务能力或同一决策点；
2. 因同一业务原因变化；
3. 共享同一生命周期；
4. 所有实现都必须提供这些方法；
5. 调用方通常将它们作为一个整体使用。

判断信号：

| 现象 | 建议 |
|---|---|
| 方法围绕同一决策点协同完成能力 | 可保留在同一 Strategy |
| 方法因同一业务原因一起变化 | 可保留在同一 Strategy |
| 某些实现只能提供部分方法 | 拆成小接口 |
| 方法属于价格、库存、履约等不同决策点 | 拆成独立 Strategy |
| 调用方只依赖其中一个方法 | 优先暴露更小接口 |

既不要机械执行"一接口一方法"，也不要用"同一个业务名词"作为塞入多个决策点的理由。Go 代码见 `go-implementation.md`。

---

## 8. Rule 与 Policy 的放置

| 场景 | 放置 |
|---|---|
| 属于同一决策点、逻辑简单、只在该 Policy 用、随 Policy 一起变化 | 直接写进 Policy |
| 判断依赖该 Strategy 的内部状态或外部适配器、与实现共同变化 | 保留在 Strategy 内部 |
| 多个策略共享、需统一校验、需独立配置 / 审计 / 替换 / 编排、变化原因不同 | 抽为独立 Rule |

**独立测试不是充分的拆分理由**：Policy 内的简单 Rule 同样可以通过 Policy 测试覆盖。不要为了"Rule 必须外置"而拆散一个决策，也不要把跨 Policy 通用判断复制多份。

---

## 9. 成套能力完整性

小接口用于解耦调用方和复用能力，但业务类型可能要求多种**独立能力必须成套出现**。例如新增一种商品类型时必须同时提供：

```text
BuyStrategy  PriceStrategy  InventoryStrategy  FulfillmentStrategy
```

这些能力属于不同决策点，不应合并成一个万能 Strategy；但分别注册会漏注册、且要到运行时特定链路才暴露。此时使用：

- 组合接口表达"完整能力集"；
- 编译期接口检查保证方法集完整；
- `ProductTypeModule` / `ProductStrategyFactory` 等抽象工厂统一创建和注册整套能力；
- 构造校验 + 枚举覆盖测试检查完整性。

> 小接口负责解耦调用方；组合接口、构造校验、抽象工厂和完整性测试共同保证业务模块完整性。

具体 Go 结构见 `go-implementation.md`。

---

## 10. 设计异味

### 过大的维度与万能 Strategy

```go
type UserStrategy interface {
    CalculatePrice(order Order) (Money, error)
    CanUseCoupon(coupon Coupon) bool
    CalculatePoints(order Order) (int, error)
    CanInvoice(order Order) bool
    ChooseDelivery(order Order) (DeliveryPlan, error)
    AfterPayment(order Order) error
}
```

一个接口混合了价格计算、优惠券资格、积分、发票、物流、支付后处理多个决策点，应拆成围绕动词的小接口：`PriceStrategy` / `CouponEligibilityPolicy` / `PointStrategy` / `InvoicePolicy` / `DeliveryStrategy`。

```text
Avoid:  UserStrategy  RegionStrategy  ProductStrategy  ChannelStrategy
Prefer: PriceStrategy TaxStrategy PaymentStrategy DeliveryStrategy
        PromotionStrategy CouponEligibilityPolicy InvoicePolicy PointStrategy
```

> 策略接口应该围绕一个明确动词或决策点，而不是围绕一个大名词。

### 专项：会员等级

会员等级通常是**事实维度**，影响折扣率、积分倍率、权益资格、免邮门槛、活动准入——这些是"值不同 / 资格不同"，不是"算法完全不同"。因此一般先建模为：

```text
Context 字段 + Config 参数 + Rule 条件
```

同一个会员等级在不同决策点角色不同：

| 决策点 | 会员等级的角色 | 建模方式 |
|---|---|---|
| 价格计算 | 查折扣率 | Config |
| 优惠资格 | 判断等级是否足够 | Rule |
| 积分计算 | 查积分倍率 | Config |
| 复杂权益发放 | 流程不同 | Strategy |
| 页面展示 | 展示字段 | Context |

只有不同会员等级真的有完全不同的权益流程时才考虑 Strategy（如普通会员只算基础积分，黑金会员叠加专属客服、线下预约、人工审批）。

---

## 11. 判断表

| 分支行为 | 建模方式 |
|---|---|
| 返回 error / true / false、判断资格、判断是否支持 | Rule |
| 查折扣率、税率、阈值 | Config |
| 算法步骤不同 | Strategy |
| 外部系统不同 | Strategy / Adapter |
| 流程生命周期不同 | Strategy / State Machine |
| 多步骤叠加 | Pipeline |
| 多个维度共同选互斥算法 | Decision Table + Strategy |
