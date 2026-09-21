# Rule、Policy、Config 与 Strategy 边界

## 目录

- Business Decision Point
- Fact Dimension
- Parameter / Config
- Rule
- Policy
- Strategy
- Strategy Method Boundary
- Rule Placement
- Capability Completeness
- Pattern Selection Details

## 1. Business Decision Point

优先分析业务决策点，而不是业务名词。

不要先问：

- 用户类型要不要策略？
- 地区要不要策略？
- 商品类型要不要策略？
- 会员等级要不要策略？

应该先问：

- 价格如何计算？
- 优惠是否可用？
- 优惠金额如何计算？
- 税费如何计算？
- 支付方式是否可用？
- 支付流程如何执行？
- 物流方式如何选择？
- 发票是否可开？
- 积分如何计算？
- 权益如何发放？
- 订单状态如何流转？

策略应该围绕一个明确的业务决策点，而不是围绕一个大名词。

---

## 2. Fact Dimension

事实维度描述当前业务上下文“是什么”。

常见事实维度：

- 用户类型；
- 会员等级；
- 地区；
- 渠道；
- 商品类型；
- 商品税类；
- 活动类型；
- 支付方式；
- 订单金额；
- 发票类型；
- 账户主体；
- 订单状态。

事实维度通常放在 Context 中。

Example:

```go
type OrderContext struct {
    MemberLevel  MemberLevel
    Region       Region
    Channel      Channel
    ProductType  ProductType
    PromotionType PromotionType
    PaymentMethod PaymentMethod
    Amount       Money
}
```

注意：

> 事实维度本身不等于策略。

---

## 3. Parameter / Config

如果某个维度只是用来查数值、比例、阈值、开关，则它是参数索引。

例如：

- 会员等级 -> 折扣率；
- 会员等级 -> 积分倍率；
- 地区 + 商品税类 -> 税率；
- 活动 -> 满减门槛；
- 渠道 -> 功能开关。

判断标准：

> 算法结构不变，只是值不同。

Example:

```go
rate := memberConfig.DiscountRate(ctx.MemberLevel)
finalPrice := ctx.Amount.Mul(rate)
```

适合使用：

- 配置表；
- 参数表；
- 配置矩阵；
- Feature Flag。

不要为纯参数差异创建策略类型。

---

## 4. Rule

如果某个维度用于判断：

- 能不能；
- 允不允许；
- 是否适用；
- 是否满足资格；
- 是否需要拦截；
- 是否支持某组合。

那么它是 Rule 职责。

Rule 首先描述规则语义，不表示每个条件都要创建接口或结构体。同一决策点内的简单 Rule 直接写进 Policy。

例如：

- 海外地区不能参加赠品活动；
- 虚拟商品不能走物流；
- 企业用户不能使用个人券；
- 小程序不支持 PayPal；
- 金卡以下不能领取生日券；
- 黑名单用户不能下单。

当规则需要跨 Policy 复用、独立配置或编排时，才使用接口：

```go
type Rule[T any] interface {
    Match(ctx T) bool
    Validate(ctx T) error
}
```

Example:

```go
type OverseasCannotUseGiftRule struct{}

func (r OverseasCannotUseGiftRule) Match(ctx OrderContext) bool {
    return ctx.Region == RegionOverseas &&
        ctx.PromotionType == PromotionTypeGift
}

func (r OverseasCannotUseGiftRule) Validate(ctx OrderContext) error {
    return errors.New("海外地区不支持赠品活动")
}
```

也可以使用更简单的接口：

```go
type Rule[T any] interface {
    Validate(ctx T) error
}
```

Example:

```go
type MiniProgramCannotUsePaypalRule struct{}

func (r MiniProgramCannotUsePaypalRule) Validate(ctx PaymentContext) error {
    if ctx.Channel == ChannelMiniProgram &&
        ctx.PaymentMethod == PaymentMethodPaypal {
        return errors.New("小程序不支持 PayPal")
    }

    return nil
}
```

判断标准：

> 如果分支结果是 error、true/false、allow/deny、eligible/ineligible，大概率属于 Rule 语义；是否抽类型另行判断。

不要把限制条件建模成 Strategy。

---

## 5. Policy

Policy 是一个业务决策点的规则边界，负责规则顺序、短路和结果表达。简单 Rule 可以直接写在 Policy 中：

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

Policy 回答的是“依据这些规则，最终是否允许或适用”。当部分 Rule 需要复用或独立配置时，Policy 再组合这些 Rule 类型；不要先把每个 `if` 都拆成 Rule struct。

---

## 6. Strategy

如果某个维度决定了不同算法、流程、外部依赖或生命周期，则它可能是策略分发维度。

例如：

- 活动类型 -> 满减、折扣、赠品算法不同；
- 支付方式 -> 微信、支付宝、PayPal 外部接口不同；
- 物流方式 -> 快递、自提、同城配送流程不同；
- 订单状态 -> 不同状态下可执行动作不同；
- 税务模型 -> 欧盟 VAT、美国销售税、大陆价内税算法不同。

Strategy 典型接口：

```go
type PromotionStrategy interface {
    Apply(ctx PromotionContext) (PromotionResult, error)
}
```

Example:

```go
type DiscountPromotionStrategy struct{}

func (s DiscountPromotionStrategy) Apply(ctx PromotionContext) (PromotionResult, error) {
    // 折扣算法
    return PromotionResult{}, nil
}

type FullReductionPromotionStrategy struct{}

func (s FullReductionPromotionStrategy) Apply(ctx PromotionContext) (PromotionResult, error) {
    // 满减算法
    return PromotionResult{}, nil
}

type GiftPromotionStrategy struct{}

func (s GiftPromotionStrategy) Apply(ctx PromotionContext) (PromotionResult, error) {
    // 锁赠品库存、生成赠品行
    return PromotionResult{}, nil
}
```

判断标准：

> 如果分支里是在执行不同的“怎么做”，而不是判断“能不能”，才考虑 Strategy。

---

## 7. Strategy Method Boundary

Strategy 的核心约束是单一抽象、高内聚和一致的变化原因，不是接口只能有一个方法。

多个方法可以放在同一 Strategy 中，但应同时满足：

1. 属于同一业务能力或同一决策点；
2. 因同一业务原因变化；
3. 共享同一生命周期；
4. 所有实现都必须提供这些方法；
5. 调用方通常将它们作为一个整体使用。

例如支付的授权、扣款和撤销如果构成不可拆分的统一支付生命周期，而且每种支付实现都必须支持，可以放在同一 `PaymentStrategy` 中。反之，如果退款、对账由不同团队、不同生命周期或不同实现集合负责，就应拆成独立能力接口。

判断信号：

| 现象 | 建议 |
|---|---|
| 方法围绕同一决策点协同完成能力 | 可保留在同一 Strategy |
| 方法因同一业务原因一起变化 | 可保留在同一 Strategy |
| 某些实现只能提供部分方法 | 拆成小接口 |
| 方法属于价格、库存、履约等不同决策点 | 拆成独立 Strategy |
| 调用方只依赖其中一个方法 | 优先暴露更小接口 |

因此不要机械执行“一接口一方法”，也不要用“同一个业务名词”作为塞入多个决策点的理由。

---

## 8. Rule And Policy Placement

判断可以直接写进 Policy，也可以抽成 Rule；依赖 Strategy 内部实现细节的不变量可以留在 Strategy。边界取决于复用范围、变化原因和是否需要独立编排。

直接写进 Policy：

- 属于同一个业务决策点；
- 逻辑简单且只在该 Policy 使用；
- 与 Policy 一起变化；
- 多个判断总是按固定顺序共同完成决策。

此时不要机械地为每个 `if` 创建一个 Rule struct。

保留在 Strategy 内部：

- 判断属于该 Strategy 的实现不变量；
- 判断依赖该 Strategy 的内部状态或外部适配器；
- 与 Strategy 实现一起变化，脱离实现没有业务意义。

抽为独立 `Rule`：

- 多个策略共享；
- 需要在策略选择或执行前统一校验；
- 规则需要独立配置、审计、替换或编排；
- 规则与策略具有不同变化原因。

独立测试不是充分的拆分理由：Policy 内的简单 Rule 同样可以通过 Policy 测试覆盖。不要为了“Rule 必须外置”而拆散一个决策，也不要把跨 Policy 通用判断复制多份。

Policy 可以直接实现简单 Rule，也可以组合独立 Rule；它不是算法实现。

---

## 9. Capability Completeness

小接口用于复用和按需依赖，但业务类型可能要求多种独立能力必须成套出现。例如新增一种商品类型时，必须同时提供：

```text
BuyStrategy
PriceStrategy
InventoryStrategy
FulfillmentStrategy
```

这些能力属于不同决策点，不应合并成一个万能 Strategy；但如果分别注册，会产生漏注册和运行时才发现缺失的风险。此时使用：

- 组合接口表达“完整能力集”；
- 编译期接口检查保证方法集完整；
- `ProductTypeModule` / `ProductStrategyFactory` 等抽象工厂统一创建和注册整套能力。

原则是：

> 小接口负责解耦调用方；组合接口、构造校验、抽象工厂和完整性测试共同保证业务模块完整性。

具体 Go 结构见 `go-implementation.md`。

---

## Pattern Selection Details

完成维度建模后，再按以下顺序分析 Rule / Policy / Config / Strategy 等模式边界。

### Identify Business Decision Points

先列出当前业务中真正的决策点。

Examples:

```text
1. 优惠是否可用？
2. 优惠金额如何计算？
3. 税费如何计算？
4. 支付方式是否可用？
5. 支付流程如何执行？
6. 物流方式如何选择？
7. 发票是否可开？
8. 积分如何计算？
9. 权益如何发放？
```

---

### Extract Fact Dimensions

列出参与这些决策点的事实维度。

Example:

```text
memberLevel
region
channel
productType
productTaxCategory
promotionType
paymentMethod
invoiceType
accountType
amount
orderStatus
```

---

### Classify Each Dimension's Role

对每个决策点，判断每个维度的角色：

- Context 字段；
- 参数索引；
- 规则条件；
- 策略分发条件；
- 状态分发条件。

这些角色用于理解职责，不直接决定类型数量。先归类，再判断是否存在独立复用、变化或编排需求。

Policy 不是维度角色；它是同一决策点的规则边界，简单 Rule 可直接实现，复用 Rule 可组合。

注意：

> 同一个维度在不同决策点中可能扮演不同角色。

例如 `memberLevel`：

| 决策点 | memberLevel 的角色 | 建模方式 |
|---|---|---|
| 价格计算 | 查折扣率 | Config |
| 优惠资格 | 判断等级是否足够 | Rule |
| 积分计算 | 查积分倍率 | Config |
| 复杂权益发放 | 如果不同等级流程不同，可能是 Strategy | Strategy |
| 页面展示 | 展示字段 | Context |

不要问：

```text
会员等级到底是不是策略？
```

应该问：

```text
在当前决策点中，会员等级到底起什么作用？
```

---

### Separate Rule From Strategy

看分支结果。

如果分支是：

```go
return errors.New("xxx")
return false
return true
```

大概率是 Rule。

如果分支是：

```go
calculateDiscount()
calculateFullReduction()
lockGiftStock()
callWechatAPI()
callAliPayAPI()
generateVATInvoice()
```

大概率是 Strategy。

判断表：

| 分支行为 | 建模方式 |
|---|---|
| 返回 error | Rule |
| 返回 true / false | Rule |
| 判断资格 | Rule |
| 判断是否支持 | Rule |
| 查折扣率、税率、阈值 | Config |
| 算法步骤不同 | Strategy |
| 外部系统不同 | Strategy / Adapter |
| 流程生命周期不同 | Strategy / State Machine |
| 多步骤叠加 | Pipeline |

---
