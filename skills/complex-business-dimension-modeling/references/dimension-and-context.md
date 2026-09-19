# 维度拆分与 Context 建模

## 目录

- 拆出更细的维度
- 合并或派生强耦合维度
- 建立层级维度
- 拆分业务 Context
- 维度拆分流程、判断表与口诀

以下内容优先级高于后文中的模式选择分析。
在执行 Rule / Policy / Config / Strategy 分析之前，必须先执行本节的维度建模分析。

在判断一个维度应该建模为 Context、Rule、Policy、Config、Strategy、Pipeline 之前，必须先判断：

```text
这个维度本身拆得是否正确？
是否太粗？
是否太细？
是否把强耦合概念拆散了？
是否应该抽象成层级维度？
是否属于当前业务上下文？
```

很多设计问题不是出在 Strategy、Policy、Rule、Config 的选择上，而是出在：

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
type FulfillmentEligibilityPolicy struct{}

func (p FulfillmentEligibilityPolicy) Decide(ctx FulfillmentContext) error {
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
| Rule | 判断条件的业务语义 |
| Policy | 承载同一决策点的规则 |
| Strategy | 执行怎么做 |

不要把数据库实体直接当所有策略的上下文。

Bad:

```go
type PriceStrategy interface {
    Calculate(order Order) (Money, error)
}
```

Good:

```go
type PriceStrategy interface {
    Calculate(ctx PriceContext) (Money, error)
}
```

---

## 5. 维度拆分分析流程

在做 Rule / Policy / Config / Strategy 判断之前，先执行维度拆分分析。

### Step 0: Dimension Modeling

必须先回答以下问题：

```text
1. 哪些字段参与决策或变化？只被读取、透传或展示的普通数据字段不进入后续维度分析。
2. 哪些字段其实混合了多个业务含义，需要拆细？
3. 哪些字段强耦合，总是一起表达一个业务模型，需要合并？
4. 哪些字段有天然层级，需要建立父子维度？
5. 哪些字段只属于某个决策点，不应该进入全局 Context？
6. 哪些字段是数据库属性，哪些字段是决策上下文？
7. 是否需要引入中间业务模型？
```

然后再进入：

```text
Rule / Policy / Config / Strategy / Pipeline / State Machine
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
| 同一判断复制到多个策略 | 缺少共享规则 | 抽 Rule |
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
