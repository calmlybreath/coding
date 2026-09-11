# Rule、Config 与 Strategy 边界

## 目录

- Business Decision Point
- Fact Dimension
- Parameter / Config
- Rule
- Strategy
- Strategy Method Boundary
- Rule Placement
- Capability Completeness
- Key Distinction Rules
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

那么它是规则条件。

例如：

- 海外地区不能参加赠品活动；
- 虚拟商品不能走物流；
- 企业用户不能使用个人券；
- 小程序不支持 PayPal；
- 金卡以下不能领取生日券；
- 黑名单用户不能下单。

Rule 典型接口：

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

> 如果分支结果是 error、true/false、allow/deny、eligible/ineligible，大概率是 Rule。

不要把限制条件建模成 Strategy。

---

## 5. Strategy

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

## 6. Strategy Method Boundary

Strategy 的核心约束是单一抽象、高内聚和一致的变化原因，不是接口只能有一个方法。

多个方法可以放在同一 Strategy / Policy 中，但应同时满足：

1. 属于同一业务能力或同一决策点；
2. 因同一业务原因变化；
3. 共享同一生命周期；
4. 所有实现都必须提供这些方法；
5. 调用方通常将它们作为一个整体使用。

例如支付的授权、扣款和撤销如果构成不可拆分的统一支付生命周期，而且每种支付实现都必须支持，可以放在同一 `PaymentPolicy` 中。反之，如果退款、对账由不同团队、不同生命周期或不同实现集合负责，就应拆成独立能力接口。

判断信号：

| 现象 | 建议 |
|---|---|
| 方法围绕同一决策点协同完成能力 | 可保留在同一 Strategy |
| 方法因同一业务原因一起变化 | 可保留在同一 Strategy |
| 某些实现只能提供部分方法 | 拆成小接口 |
| 方法属于价格、库存、履约等不同决策点 | 拆成独立 Policy |
| 调用方只依赖其中一个方法 | 优先暴露更小接口 |

因此不要机械执行“一接口一方法”，也不要用“同一个业务名词”作为塞入多个决策点的理由。

---

## 7. Rule Placement

Rule 可以是 Strategy 内部的前置校验，也可以是独立规则。边界取决于规则的复用范围和变化原因。

保留在 Strategy 内部：

- 规则只对该策略成立；
- 规则与策略实现一起变化；
- 规则依赖该策略的内部状态或外部适配器；
- 单独复用没有业务意义。

抽为独立 `Rule` / `Validator`：

- 多个策略共享；
- 需要在策略选择或执行前统一校验；
- 规则需要独立配置、编排或测试；
- 规则与策略具有不同变化原因。

不要为了“Rule 必须外置”而拆散策略内部不变量，也不要把跨策略通用资格判断复制到每个 Strategy。

---

## 8. Capability Completeness

小接口用于复用和按需依赖，但业务类型可能要求多种独立能力必须成套出现。例如新增一种商品类型时，必须同时提供：

```text
BuyPolicy
PricePolicy
InventoryPolicy
FulfillmentPolicy
```

这些能力属于不同决策点，不应合并成一个万能 Strategy；但如果分别注册，会产生漏注册和运行时才发现缺失的风险。此时使用：

- 组合接口表达“完整能力集”；
- 编译期接口检查保证方法集完整；
- `ProductTypeModule` / `ProductPolicyFactory` 等抽象工厂统一创建和注册整套能力。

原则是：

> 小接口负责解耦调用方；组合接口、构造校验、抽象工厂和完整性测试共同保证业务模块完整性。

具体 Go 结构见 `go-implementation.md`。

---

## Key Distinction Rules

使用以下口诀判断：

```text
事实是什么，放 Context；
值是多少，用参数；
能不能，用规则；
怎么算，用策略；
怎么做，用策略；
状态怎么变，用状态机；
多个因素叠加，用 Pipeline；
多维共同决定算法，用决策表 + 策略；
只是组合限制，不要做策略；
只是数值不同，不要做策略。
Strategy 单一抽象，不是单一方法；
通用规则外置，内部规则内聚；
小接口复用，组合接口收束；
成套能力用抽象工厂。
```

---

## Pattern Selection Details

完成维度建模后，再按以下顺序分析 Rule / Config / Strategy 等模式边界。

### Step 1: Identify Business Decision Points

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

### Step 2: Extract Fact Dimensions

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

### Step 3: Classify Each Dimension's Role

对每个决策点，判断每个维度的角色：

- Context 字段；
- 参数索引；
- 规则条件；
- 策略分发条件；
- 状态分发条件。

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

### Step 4: Separate Rule From Strategy

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
