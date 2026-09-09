---
name: complex-business-dimension-modeling
description: 用于分析复杂业务中的维度边界、多维变化、非正交关系以及规则与策略边界，帮助拆分过粗维度、识别强耦合组合、建立层级维度、拆分业务 Context，并判断逻辑应该建模为 Context、Rule、Config、Decision Table、Resolver、Pipeline、Strategy 或 State Machine。适用于优惠、价格、税费、支付、物流、权益、订单状态、发票、风控等复杂业务建模场景。代码示例默认使用 Go。
---

# Complex Business Dimension Modeling Skill

## Purpose

当业务存在多个变化维度时，使用本 Skill 帮助分析：

- 哪些是事实维度；
- 哪些只是参数；
- 哪些是规则条件；
- 哪些适合作为策略分发维度；
- 哪些非正交关系应该用 Rule、Config Matrix、Decision Table、Pipeline 或 Strategy 处理；
- 如何避免维度过大、组合爆炸、规则误划成策略。

核心原则：

> 不围绕业务名词建模，而围绕业务决策点建模。  
> 不把所有维度都做成策略。  
> 不把规则误划成策略。  
> 策略表达“怎么做”，规则表达“能不能”，参数表达“值是多少”。

---

# 使用顺序

本文包含两个互补层次，使用时按以下顺序执行：

```text
第一层：维度建模
拆细过粗维度 -> 识别强耦合组合 -> 建立层级维度 -> 拆分业务 Context

第二层：模式选择
识别决策点 -> 提取事实维度 -> 判断维度角色
-> 选择 Rule / Config / Strategy / Pipeline / Decision Table / State Machine
```

其中：

- `Dimension Decomposition & Context Splitting` 负责第一层；
- `Core Concepts` 到 `Go Implementation Guidance` 负责第二层；
- `Analysis Workflow` 是唯一的最终执行流程；
- `Required Output Format` 是唯一的完整输出入口。

后文保留了完整判断信号、反例、正例和 Go 代码示例，不要只根据口诀直接下结论。

---

# When To Use

当用户的问题涉及以下场景时，应使用本 Skill：

- 多个业务维度共同影响逻辑；
- 用户在纠结是否应该使用策略模式；
- 用户在设计优惠、税费、支付、物流、会员权益、价格、订单状态等复杂业务；
- 用户提到“维度”、“正交”、“非正交”、“组合爆炸”、“规则”、“策略”、“多态”、“配置化”；
- 用户不确定某个字段应该作为规则条件还是策略分发维度；
- 用户需要 AI coding 生成合理的类结构、模块边界或代码骨架；
- 用户要求使用 Go 进行代码设计或架构分析。

---

# Dimension Decomposition & Context Splitting

以下内容优先级高于后文中的模式选择分析。
在执行 Rule / Config / Strategy 分析之前，必须先执行本节的维度建模分析。

在判断一个维度应该建模为 Context、Rule、Config、Strategy、Pipeline 之前，必须先判断：

```text
这个维度本身拆得是否正确？
是否太粗？
是否太细？
是否把强耦合概念拆散了？
是否应该抽象成层级维度？
是否属于当前业务上下文？
```

很多设计问题不是出在 Strategy、Rule、Config 的选择上，而是出在：

```text
维度边界错误；
上下文边界错误；
业务概念层级错误；
强耦合维度被错误拆散；
多个上下文被混在一个 Context 里。
```

---

## 1. 拆出更细的维度

### 什么时候需要拆细维度？

如果一个维度里混合了多个业务含义，并且这些含义在不同决策点中独立变化，就应该拆细。

判断信号：

```text
1. 一个字段被大量 if/else 解析；
2. 一个枚举值名字越来越长；
3. 一个维度同时影响多个不相关决策点；
4. 不同业务方对同一个字段有不同解释；
5. 一个维度既表达类型，又表达状态，又表达来源；
6. 增加新业务时，总是在原枚举上追加复杂组合值。
```

---

### Bad Example: 维度过粗

```go
type ProductType string

const (
    ProductTypeNormalPhysicalDomestic ProductType = "NORMAL_PHYSICAL_DOMESTIC"
    ProductTypeNormalPhysicalCrossBorder ProductType = "NORMAL_PHYSICAL_CROSS_BORDER"
    ProductTypeVirtualDomestic ProductType = "VIRTUAL_DOMESTIC"
    ProductTypeVirtualOverseas ProductType = "VIRTUAL_OVERSEAS"
    ProductTypePresalePhysicalDomestic ProductType = "PRESALE_PHYSICAL_DOMESTIC"
)
```

这个 `ProductType` 混合了：

```text
商品形态：实物 / 虚拟
销售模式：普通 / 预售
贸易模式：境内 / 跨境
履约模式：发货 / 权益发放
```

应该拆细。

---

### Good Example: 拆成独立事实维度

```go
type ProductForm string

const (
    ProductFormPhysical ProductForm = "PHYSICAL"
    ProductFormVirtual  ProductForm = "VIRTUAL"
)

type SaleMode string

const (
    SaleModeNormal  SaleMode = "NORMAL"
    SaleModePresale SaleMode = "PRESALE"
    SaleModeGroupBuy SaleMode = "GROUP_BUY"
)

type TradeMode string

const (
    TradeModeDomestic    TradeMode = "DOMESTIC"
    TradeModeCrossBorder TradeMode = "CROSS_BORDER"
)

type FulfillmentMode string

const (
    FulfillmentModeDelivery FulfillmentMode = "DELIVERY"
    FulfillmentModeBenefit  FulfillmentMode = "BENEFIT"
)
```

Context：

```go
type BuyContext struct {
    ProductForm     ProductForm
    SaleMode        SaleMode
    TradeMode       TradeMode
    FulfillmentMode FulfillmentMode
    Region          Region
    Channel         Channel
    MemberLevel     MemberLevel
}
```

拆细后，每个维度可以独立参与不同决策点：

| 决策点 | 相关维度 |
|---|---|
| 购买流程 | SaleMode, ProductForm |
| 是否需要物流 | FulfillmentMode, ProductForm |
| 是否需要报关 | TradeMode |
| 税费计算 | TradeMode, Region, ProductTaxCategory |
| 库存扣减 | ProductForm, SaleMode |
| 权益发放 | FulfillmentMode |

---

### 拆细判断标准

如果一个维度可以被拆成多个稳定问题，就应该拆：

```text
它是什么形态？
它怎么销售？
它怎么履约？
它属于哪个贸易模式？
它处于什么状态？
它来自哪个渠道？
它适用于哪个主体？
```

每个问题对应一个维度。

---

## 2. 合并强耦合维度

拆维度不是越细越好。

如果多个维度总是一起变化、一起判断、一起决定同一个业务语义，应优先保留原始事实并派生一个中间业务模型。只有单独维度确实没有独立意义、且属于同一生命周期时，才考虑直接合并。

---

### 什么时候需要合并？

判断信号：

```text
1. 两个维度几乎总是同时出现；
2. 一个维度的取值依赖另一个维度；
3. 单独看任意一个维度都无法表达真实业务语义；
4. 大量规则都是 A + B + C 的固定组合；
5. 多个维度共同决定一个稳定业务模型；
6. 代码里到处出现同一组组合判断。
```

---

### Bad Example: 强耦合维度被拆散

```go
if ctx.SaleMode == SaleModePresale &&
    ctx.PaymentStage == PaymentStageDeposit {
    // 预售定金支付
}

if ctx.SaleMode == SaleModePresale &&
    ctx.PaymentStage == PaymentStageFinal {
    // 预售尾款支付
}

if ctx.SaleMode == SaleModeNormal &&
    ctx.PaymentStage == PaymentStageFull {
    // 普通全款支付
}
```

这里 `SaleMode` 和 `PaymentStage` 强耦合，它们组合后才是真正的业务语义。

可以抽象成：

```go
type PurchasePhase string

const (
    PurchasePhaseNormalFullPay PurchasePhase = "NORMAL_FULL_PAY"
    PurchasePhasePresaleDeposit PurchasePhase = "PRESALE_DEPOSIT"
    PurchasePhasePresaleFinalPay PurchasePhase = "PRESALE_FINAL_PAY"
)
```

然后：

```go
type PurchasePhaseResolver struct{}

func (r PurchasePhaseResolver) Resolve(ctx BuyContext) (PurchasePhase, error) {
    switch {
    case ctx.SaleMode == SaleModeNormal && ctx.PaymentStage == PaymentStageFull:
        return PurchasePhaseNormalFullPay, nil
    case ctx.SaleMode == SaleModePresale && ctx.PaymentStage == PaymentStageDeposit:
        return PurchasePhasePresaleDeposit, nil
    case ctx.SaleMode == SaleModePresale && ctx.PaymentStage == PaymentStageFinal:
        return PurchasePhasePresaleFinalPay, nil
    default:
        return "", errors.New("invalid purchase phase")
    }
}
```

这样后续策略基于 `PurchasePhase`，而不是反复判断 `SaleMode + PaymentStage`。

---

### 合并后的好处

```text
1. 减少重复组合判断；
2. 让隐含业务概念显式化；
3. 降低策略数量；
4. 避免到处拼维度；
5. 便于规则审计和配置化；
6. 让后续代码围绕真实业务模型展开。
```

---

### 合并判断标准

如果多个维度组合后形成了一个稳定业务名词，就应该考虑派生或封装为中间模型。

Examples:

| 原始维度组合 | 中间业务模型 |
|---|---|
| SaleMode + PaymentStage | PurchasePhase |
| Region + ProductTaxCategory + AccountType | TaxModel |
| ProductForm + FulfillmentType | FulfillmentModel |
| PromotionType + CouponType + DiscountMode | PromotionModel |
| OrderStatus + PaymentStatus + DeliveryStatus | OrderLifecycleState |
| Channel + PaymentMethod + AccountRegion | PaymentRoute |
| TradeMode + CustomsMode + Region | TradeComplianceModel |

---

## 3. 建层级维度

有些维度不是平铺枚举，而是天然有层级。

如果直接平铺，会导致枚举爆炸、规则重复、策略选择困难。

---

### 什么时候需要层级维度？

判断信号：

```text
1. 维度有明显父子分类；
2. 上层分类决定大流程，下层分类决定细节；
3. 多个子类型共享同一类规则；
4. 一些规则只关心大类，一些策略关心小类；
5. 枚举值出现大量前缀重复。
```

---

### Bad Example: 平铺枚举

```go
type ProductCategory string

const (
    ProductCategoryPhysicalBook       ProductCategory = "PHYSICAL_BOOK"
    ProductCategoryPhysicalFood       ProductCategory = "PHYSICAL_FOOD"
    ProductCategoryPhysicalMedicine   ProductCategory = "PHYSICAL_MEDICINE"
    ProductCategoryVirtualCoupon      ProductCategory = "VIRTUAL_COUPON"
    ProductCategoryVirtualMembership  ProductCategory = "VIRTUAL_MEMBERSHIP"
    ProductCategoryVirtualCourse      ProductCategory = "VIRTUAL_COURSE"
)
```

这里混合了：

```text
一级分类：实物 / 虚拟
二级分类：图书 / 食品 / 药品 / 券 / 会员 / 课程
```

---

### Good Example: 层级维度

```go
type ProductForm string

const (
    ProductFormPhysical ProductForm = "PHYSICAL"
    ProductFormVirtual  ProductForm = "VIRTUAL"
)

type ProductCategory string

const (
    ProductCategoryBook       ProductCategory = "BOOK"
    ProductCategoryFood       ProductCategory = "FOOD"
    ProductCategoryMedicine   ProductCategory = "MEDICINE"
    ProductCategoryCoupon     ProductCategory = "COUPON"
    ProductCategoryMembership ProductCategory = "MEMBERSHIP"
    ProductCategoryCourse     ProductCategory = "COURSE"
)
```

或者显式建层级：

```go
type ProductClassification struct {
    Form     ProductForm
    Category ProductCategory
    SubType  ProductSubType
}
```

---

### 层级维度的使用原则

```text
上层维度用于大流程分发；
下层维度用于细规则、参数、配置。
```

例如：

| 层级 | 用途 |
|---|---|
| ProductForm | 决定实物履约还是虚拟履约 |
| ProductCategory | 决定税类、风控、资质要求 |
| ProductSubType | 决定具体参数、模板、展示 |

---

### Example

```go
type FulfillmentStrategy interface {
    Fulfill(ctx FulfillmentContext) (FulfillmentResult, error)
}
```

用上层维度选择大策略：

```go
type FulfillmentStrategyFactory struct {
    strategies map[ProductForm]FulfillmentStrategy
}

func (f FulfillmentStrategyFactory) Get(form ProductForm) (FulfillmentStrategy, error) {
    strategy, ok := f.strategies[form]
    if !ok {
        return nil, errors.New("unsupported product form")
    }

    return strategy, nil
}
```

用下层维度处理细规则：

```go
type MedicineQualificationRule struct{}

func (r MedicineQualificationRule) Validate(ctx FulfillmentContext) error {
    if ctx.ProductCategory == ProductCategoryMedicine &&
        !ctx.User.HasMedicineQualification {
        return errors.New("用户缺少药品购买资质")
    }

    return nil
}
```

---

## 4. 拆业务上下文

不要把所有字段都放进一个巨大的 `Context`。

Context 应该围绕具体决策点定义。

---

### Bad Example: 巨型上下文

```go
type OrderContext struct {
    UserID             string
    MemberLevel        MemberLevel
    Region             Region
    Channel            Channel
    ProductID          string
    ProductForm        ProductForm
    ProductCategory    ProductCategory
    ProductTaxCategory ProductTaxCategory
    SaleMode           SaleMode
    TradeMode          TradeMode
    PaymentMethod      PaymentMethod
    PaymentStage       PaymentStage
    InvoiceType        InvoiceType
    Address            Address
    CouponID           string
    PromotionType      PromotionType
    InventoryType      InventoryType
    OrderStatus        OrderStatus
    DeliveryStatus     DeliveryStatus
    Amount             Money
}
```

这个 Context 什么都能做，但问题是：

```text
1. 决策点边界不清；
2. 每个策略都能访问过多字段；
3. 容易形成隐式耦合；
4. 测试数据构造困难；
5. 规则不知道自己真正依赖什么；
6. 新字段会污染所有逻辑。
```

---

### Good Example: 按决策点拆 Context

```go
type BuyContext struct {
    UserID       string
    MemberLevel  MemberLevel
    Region       Region
    Channel      Channel
    ProductID    string
    ProductForm  ProductForm
    SaleMode     SaleMode
    TradeMode    TradeMode
    Amount       Money
}

type PriceContext struct {
    UserID        string
    MemberLevel   MemberLevel
    Channel       Channel
    ProductID     string
    ProductForm   ProductForm
    SaleMode      SaleMode
    PromotionType PromotionType
    Amount        Money
}

type TaxContext struct {
    Region             Region
    TradeMode          TradeMode
    ProductTaxCategory ProductTaxCategory
    AccountType        AccountType
    InvoiceType        InvoiceType
    Amount             Money
}

type PaymentContext struct {
    UserID        string
    Channel       Channel
    Region        Region
    PaymentMethod PaymentMethod
    PaymentStage  PaymentStage
    Amount        Money
}

type FulfillmentContext struct {
    OrderID         string
    ProductForm    ProductForm
    ProductCategory ProductCategory
    FulfillmentMode FulfillmentMode
    Address         *Address
}
```

---

### Context 拆分原则

```text
一个 Context 服务一个决策点；
Context 只放该决策点需要的事实；
不要为了省事传全量 Order；
不要让策略能访问无关字段；
Context 是业务输入，不是数据库实体；
Context 应该稳定、明确、可测试。
```

---

### Context 与 Entity 的区别

| 类型 | 作用 |
|---|---|
| Entity | 表达领域对象生命周期，例如 Order、Product、User |
| Context | 表达某个决策点需要的事实快照 |
| Config | 表达参数、阈值、开关 |
| Rule | 判断能不能 |
| Strategy | 执行怎么做 |

不要把数据库实体直接当所有策略的上下文。

Bad:

```go
type PricePolicy interface {
    Calculate(order Order) (Money, error)
}
```

Good:

```go
type PricePolicy interface {
    Calculate(ctx PriceContext) (Money, error)
}
```

---

## 5. 维度拆分分析流程

在做 Rule / Config / Strategy 判断之前，先执行维度拆分分析。

### Step 0: Dimension Modeling

必须先回答以下问题：

```text
1. 当前有哪些原始业务字段？
2. 哪些字段其实混合了多个业务含义，需要拆细？
3. 哪些字段强耦合，总是一起表达一个业务模型，需要合并？
4. 哪些字段有天然层级，需要建立父子维度？
5. 哪些字段只属于某个决策点，不应该进入全局 Context？
6. 哪些字段是数据库属性，哪些字段是决策上下文？
7. 是否需要引入中间业务模型？
```

然后再进入：

```text
Rule / Config / Strategy / Pipeline / State Machine
```

---

## 6. 拆维度、合维度、建层级的判断表

| 现象 | 问题 | 处理方式 |
|---|---|---|
| 一个枚举值越来越长 | 维度过粗 | 拆细维度 |
| 一个字段同时表达类型、状态、来源 | 语义混杂 | 拆细维度 |
| 大量 if 判断同一组字段组合 | 强耦合组合 | 合并成中间业务模型 |
| 多个字段单独看没有业务意义 | 维度过细 | 合并 |
| 枚举值有明显父子分类 | 层级缺失 | 建层级维度 |
| 上层决定流程，下层决定参数 | 平铺导致复杂 | 建层级维度 |
| 所有策略都传巨大 Context | 上下文污染 | 按决策点拆 Context |
| 策略访问了无关字段 | 职责泄漏 | 缩小 Context |
| 新增规则要改多个策略 | 规则被塞进策略 | 抽 Rule |
| 新增组合导致策略类暴涨 | 组合爆炸 | 中间模型 / 决策表 |

---

## 7. 维度拆分口诀

```text
太粗就拆；
太散就合；
有父子就分层；
有组合语义就抽模型；
不同决策点用不同 Context；
数据库对象不是决策上下文；
维度先建对，再谈策略、规则、配置。
```

---

# Core Concepts

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

# Key Distinction Rules

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
```

---

# Pattern Selection Details

完成维度建模后，再按以下顺序分析 Rule / Config / Strategy 等模式边界。

## Step 1: Identify Business Decision Points

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

## Step 2: Extract Fact Dimensions

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

## Step 3: Classify Each Dimension's Role

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

## Step 4: Separate Rule From Strategy

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

## Step 5: Handle Non-Orthogonal Dimensions

业务维度通常不是完全正交的。

常见非正交组合：

```text
地区 × 商品类型
渠道 × 支付方式
用户类型 × 优惠券类型
商品类型 × 活动类型
会员等级 × 权益类型
```

不要急着把这些组合做成策略。

先判断非正交关系属于哪一类。

---

### Case 1: Illegal Combination

如果只是非法组合，用 Rule。

Examples:

```text
海外 + 赠品 = 不允许
虚拟商品 + 物流 = 不需要
企业用户 + 个人券 = 不允许
小程序 + PayPal = 不支持
```

Use:

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

Avoid:

```go
type MiniProgramPaypalPaymentStrategy struct{}
```

Reason:

> 它只是“不支持”，不是一种支付算法。

---

### Case 2: Same Formula, Different Values

如果公式相同，只是数值不同，用配置矩阵。

Example:

| 地区 | 商品税类 | 税率 |
|---|---|---|
| 大陆 | 普通商品 | 0.13 |
| 大陆 | 图书 | 0.09 |
| 香港 | 普通商品 | 0 |
| 欧盟 | 数字服务 | 0.2 |

如果算法始终是：

```text
税额 = 金额 × 税率
```

则不要使用策略。

Use:

```go
rate := taxRateConfig.Rate(ctx.Region, ctx.ProductTaxCategory)
tax := ctx.Amount.Mul(rate)
```

---

### Case 3: Multiple Factors Accumulate

如果多个维度分别贡献一部分结果，用 Pipeline。

Example:

```text
最终税费 = 基础税 + 地区附加税 + 商品特殊税 - 主体减免
```

Use:

```go
type TaxStep interface {
    Supports(ctx TaxContext) bool
    Apply(ctx TaxContext, result *TaxResult) error
}
```

Examples:

```go
type BaseTaxStep struct{}

func (s BaseTaxStep) Supports(ctx TaxContext) bool {
    return true
}

func (s BaseTaxStep) Apply(ctx TaxContext, result *TaxResult) error {
    // 基础税计算
    return nil
}

type RegionSurchargeTaxStep struct{}

func (s RegionSurchargeTaxStep) Supports(ctx TaxContext) bool {
    return ctx.Region == RegionUS
}

func (s RegionSurchargeTaxStep) Apply(ctx TaxContext, result *TaxResult) error {
    // 地区附加税
    return nil
}
```

适合：

```text
多个维度分别贡献一部分结果；
不是互斥选择一个算法；
处理过程可组合。
```

---

### Case 4: Multiple Dimensions Decide Mutually Exclusive Algorithm

如果多个维度共同决定一个互斥算法，用：

```text
Decision Table + Strategy
```

Example:

```text
欧盟 + 数字服务 -> 欧盟数字服务 VAT
美国 + 实物商品 -> 美国销售税
大陆 + 普通商品 -> 大陆价内税
香港 + 任意商品 -> 免税
```

不要强行选一个主维度。

应建模为：

```text
多个事实维度 -> 中间业务模型 -> 策略
```

---

# Intermediate Business Model

当多个事实维度共同决定算法时，可以引入中间业务模型。

Example:

```go
type TaxModel string

const (
    TaxModelFree                  TaxModel = "TAX_FREE"
    TaxModelMainlandIncludedTax   TaxModel = "MAINLAND_INCLUDED_TAX"
    TaxModelEUVAT                 TaxModel = "EU_VAT"
    TaxModelEUDigitalServiceVAT   TaxModel = "EU_DIGITAL_SERVICE_VAT"
    TaxModelUSSalesTax            TaxModel = "US_SALES_TAX"
    TaxModelCrossBorderTax        TaxModel = "CROSS_BORDER_TAX"
)
```

Flow:

```text
TaxContext
    ↓
TaxRuleValidator
    ↓
TaxModelResolver
    ↓
TaxPolicyFactory
    ↓
TaxPolicy.Calculate()
```

Example:

```go
type TaxService struct {
    RuleValidator  TaxRuleValidator
    ModelResolver  TaxModelResolver
    PolicyFactory  TaxPolicyFactory
}

func (s TaxService) Calculate(ctx TaxContext) (Tax, error) {
    if err := s.RuleValidator.Validate(ctx); err != nil {
        return Tax{}, err
    }

    model, err := s.ModelResolver.Resolve(ctx)
    if err != nil {
        return Tax{}, err
    }

    policy, err := s.PolicyFactory.Get(model)
    if err != nil {
        return Tax{}, err
    }

    return policy.Calculate(ctx)
}
```

Decision Table Example:

| 优先级 | 地区 | 商品税类 | 主体 | 税务模型 |
|---|---|---|---|---|
| 100 | 任意 | 免税商品 | 任意 | TAX_FREE |
| 90 | 香港 | 任意 | 任意 | TAX_FREE |
| 80 | 欧盟 | 数字服务 | 个人 | EU_DIGITAL_SERVICE_VAT |
| 70 | 欧盟 | 普通商品 | 任意 | EU_VAT |
| 60 | 美国 | 实物商品 | 任意 | US_SALES_TAX |
| 50 | 大陆 | 普通商品 | 任意 | MAINLAND_INCLUDED_TAX |

Strategy:

```go
type TaxPolicy interface {
    Calculate(ctx TaxContext) (Tax, error)
}
```

```go
type EUDigitalServiceVATPolicy struct{}

func (p EUDigitalServiceVATPolicy) Calculate(ctx TaxContext) (Tax, error) {
    // 欧盟数字服务 VAT 算法
    return Tax{}, nil
}
```

This is not blind combination strategy. It is:

```text
真实业务模型策略
```

---

# Combination Strategy Boundary

## Avoid Cartesian Product Strategy

不要为所有组合都建策略类型。

Bad examples:

```go
type MainlandNormalGoodsPersonalNormalInvoiceTaxPolicy struct{}
type MainlandNormalGoodsEnterpriseSpecialInvoiceTaxPolicy struct{}
type EUDigitalServicePersonalNoInvoiceTaxPolicy struct{}
type USPhysicalGoodsEnterpriseNormalInvoiceTaxPolicy struct{}
```

如果维度数量是：

```text
地区 4 种
商品税类 4 种
发票类型 3 种
主体类型 2 种
```

组合数为：

$$
4 \times 4 \times 3 \times 2 = 96
$$

这会导致组合爆炸。

---

## Prefer Real Business Model Strategy

只有当某个组合代表独立算法或独立业务模型时，才建策略。

Examples:

```text
欧盟数字服务 VAT
美国销售税
大陆价内税
跨境进口税
免税模型
```

Good examples:

```go
type EUDigitalServiceVATPolicy struct{}
type USSalesTaxPolicy struct{}
type MainlandIncludedTaxPolicy struct{}
type CrossBorderImportTaxPolicy struct{}
type TaxFreePolicy struct{}
```

判断表：

| 类型 | 特点 | 是否推荐 |
|---|---|---|
| 笛卡尔积组合策略 | 每个组合一个类型 | 不推荐 |
| 真实业务模型策略 | 每种独立算法一个类型 | 推荐 |
| 决策表选择策略 | 多个维度共同决定策略 | 推荐 |
| 配置矩阵 | 公式相同，只是参数不同 | 推荐 |
| 规则 | 判断组合是否合法 | 推荐 |

---

# Detect Oversized Dimension

如果出现这种接口，说明维度太大：

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

它混合了多个决策点：

```text
价格计算
优惠券资格
积分计算
发票判断
物流选择
支付后处理
```

应该拆成：

```go
type PricePolicy interface {
    Calculate(ctx PriceContext) (Money, error)
}

type CouponEligibilityRule interface {
    Validate(ctx CouponContext) error
}

type PointPolicy interface {
    Calculate(ctx PointContext) (Point, error)
}

type InvoiceRule interface {
    Validate(ctx InvoiceContext) error
}

type DeliveryPolicy interface {
    Plan(ctx DeliveryContext) (DeliveryPlan, error)
}
```

原则：

> 策略接口应该围绕一个明确动词或决策点，而不是围绕一个大名词。

Avoid:

```text
UserStrategy
RegionStrategy
ProductStrategy
ChannelStrategy
```

Prefer:

```text
PricePolicy
TaxPolicy
PaymentStrategy
DeliveryPolicy
PromotionStrategy
CouponEligibilityRule
InvoiceRule
PointPolicy
```

---

# Special Guidance: Member Level

会员等级通常是事实维度。

Examples:

```text
普通会员
银卡会员
金卡会员
黑金会员
```

它经常影响：

```text
折扣率
积分倍率
权益资格
免邮门槛
活动准入
```

这些通常是：

```text
值不同
资格不同
```

而不是：

```text
算法完全不同
流程完全不同
外部依赖不同
生命周期不同
```

因此，会员等级一般先建模为：

```text
Context 字段 + Config 参数 + Rule 条件
```

Example:

```go
rate := memberConfig.DiscountRate(ctx.MemberLevel)
finalPrice := ctx.Amount.Mul(rate)
```

或者：

```go
if ctx.MemberLevel.LowerThan(MemberLevelGold) {
    return errors.New("金卡及以上会员才可使用该权益")
}
```

只有当不同会员等级真的有完全不同的权益流程时，才考虑 Strategy。

Example:

```go
type MemberBenefitPolicy interface {
    Calculate(ctx MemberBenefitContext) (BenefitResult, error)
}
```

适合策略化的情况：

```text
普通会员：只计算基础积分；
金卡会员：基础积分 + 品类加成 + 生日权益；
黑金会员：基础积分 + 专属客服 + 线下服务预约 + 人工审批。
```

---

# Go Implementation Guidance

## 1. Prefer Small Interfaces

Go 中接口应该小而清晰。

Good:

```go
type PaymentStrategy interface {
    Pay(ctx PaymentContext) (PayResult, error)
}
```

Bad:

```go
type PaymentStrategy interface {
    Pay(ctx PaymentContext) (PayResult, error)
    Refund(ctx RefundContext) (RefundResult, error)
    Query(ctx QueryContext) (QueryResult, error)
    Close(ctx CloseContext) error
    Reconcile(ctx ReconcileContext) error
}
```

除非这些方法确实属于同一个生命周期模型，并且每种支付方式都必须实现。

---

## 2. Resolver Should Own Selection Logic

策略选择逻辑不要散落在业务流程里。

Good:

```go
type PaymentStrategyResolver struct {
    strategies map[PaymentMethod]PaymentStrategy
}

func (r PaymentStrategyResolver) Resolve(method PaymentMethod) (PaymentStrategy, error) {
    strategy, ok := r.strategies[method]
    if !ok {
        return nil, errors.New("unsupported payment method")
    }

    return strategy, nil
}
```

Usage:

```go
strategy, err := resolver.Resolve(ctx.PaymentMethod)
if err != nil {
    return PayResult{}, err
}

return strategy.Pay(ctx)
```

---

## 3. Use Decision Table For Multi-Dimension Selection

当多个维度共同决定策略时，使用决策表。

```go
type TaxDecisionRule struct {
    Priority           int
    Region             *Region
    ProductTaxCategory *ProductTaxCategory
    AccountType        *AccountType
    Model              TaxModel
}

func (r TaxDecisionRule) Match(ctx TaxContext) bool {
    if r.Region != nil && *r.Region != ctx.Region {
        return false
    }

    if r.ProductTaxCategory != nil && *r.ProductTaxCategory != ctx.ProductTaxCategory {
        return false
    }

    if r.AccountType != nil && *r.AccountType != ctx.AccountType {
        return false
    }

    return true
}
```

Resolver:

```go
type TaxModelResolver struct {
    rules []TaxDecisionRule
}

func (r TaxModelResolver) Resolve(ctx TaxContext) (TaxModel, error) {
    for _, rule := range r.rules {
        if rule.Match(ctx) {
            return rule.Model, nil
        }
    }

    return "", errors.New("no tax model matched")
}
```

要求：

- 决策表需要有优先级；
- 更具体规则优先；
- 默认规则必须显式；
- 未匹配时必须返回错误；
- 不要把大量 if-else 散落在业务服务里。

---

## 4. Use Pipeline For Accumulative Logic

当多个处理步骤可以叠加时，用 Pipeline。

```go
type TaxPipeline struct {
    steps []TaxStep
}

func (p TaxPipeline) Calculate(ctx TaxContext) (TaxResult, error) {
    result := TaxResult{}

    for _, step := range p.steps {
        if !step.Supports(ctx) {
            continue
        }

        if err := step.Apply(ctx, &result); err != nil {
            return TaxResult{}, err
        }
    }

    return result, nil
}
```

适合场景：

```text
基础费用 + 地区附加费 + 商品特殊税 + 企业减免
```

不适合场景：

```text
只能选择一个互斥算法
```

互斥算法应该使用 Strategy 或 Decision Table + Strategy。

---

# Analysis Workflow

使用本 Skill 分析复杂业务时，完整流程应为：

```text
0. 维度建模：
   - 拆出更细的维度；
   - 识别强耦合维度，并派生或封装中间模型；
   - 建立层级维度；
   - 拆分业务上下文。

1. 找业务决策点。

2. 提取事实维度。

3. 判断每个维度在每个决策点中的角色：
   - Context 字段；
   - 参数索引；
   - 规则条件；
   - 策略分发条件；
   - 状态分发条件。

4. 判断哪些逻辑只是“能不能”，应该用 Rule。

5. 判断哪些逻辑只是“值不同”，应该用 Config / Matrix。

6. 判断哪些逻辑是“算法 / 流程不同”，才应该用 Strategy。

7. 如果多个维度共同决定算法，判断是否需要：
   - 中间业务模型；
   - Decision Table + Strategy；
   - Pipeline；
   - Config Matrix。

8. 明确指出哪些设计不应该做，避免组合爆炸。

9. 最后输出推荐结构、模块边界和 Go 代码骨架。
```

---

# Required Output Format

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

| 规则 | 条件 | 结果 | 建模方式 |
|---|---|---|---|
| xxx | xxx | allow / deny / error | Rule |

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

# Final Summary Template

分析结束时，用以下格式总结：

```text
本需求中：

- xxx 是事实维度；
- xxx 是参数；
- xxx 是规则条件；
- xxx 是策略分发条件；
- xxx 是非正交关系；
- 非正交关系应该用 xxx 处理；
- 不应该为 xxx 建策略；
- 推荐使用 xxx 结构。
```

---

# Golden Rules

## 1. Dimension Modeling Golden Rules

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
## 2. Pattern Selection Golden Rules

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
```
