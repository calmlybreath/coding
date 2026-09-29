# 维度拆分、Context 与非正交

本文件是维度建模（SKILL.md Step 2 / Step 3 / Step 4 的非正交部分）的判据与案例，**优先级高于模式选择**。在做 Rule / Policy / Config / Strategy 判断之前，必须先确认：

```text
这个维度本身拆得是否正确？
是否太粗？是否太散？强耦合概念是否被拆散？
是否应该抽象成层级维度？Context 是否过大？维度之间是否非正交？
```

很多设计问题不是出在 Strategy / Policy / Rule / Config 的选择上，而是出在维度边界、上下文边界或强耦合维度上。

---

## 1. 拆出更细的维度

一个维度里混合了多个业务含义、且这些含义独立变化时，应拆细。

判断信号：

```text
1. 一个字段被大量 if/else 解析；
2. 一个枚举值名字越来越长；
3. 一个维度同时影响多个不相关决策点；
4. 不同业务方对同一个字段有不同解释；
5. 一个维度既表达类型，又表达状态，又表达来源；
6. 增加新业务时，总是在原枚举上追加复杂组合值。
```

判别方法：能被拆成多个稳定问题，就拆。每个问题对应一个维度。

```text
它是什么形态？ 它怎么销售？ 它怎么履约？
它属于哪个贸易模式？ 它处于什么状态？ 它来自哪个渠道？ 它适用于哪个主体？
```

**Bad：维度过粗**

```go
type ProductType string

const (
    ProductTypeNormalPhysicalDomestic     ProductType = "NORMAL_PHYSICAL_DOMESTIC"
    ProductTypeNormalPhysicalCrossBorder  ProductType = "NORMAL_PHYSICAL_CROSS_BORDER"
    ProductTypeVirtualOverseas            ProductType = "VIRTUAL_OVERSEAS"
    ProductTypePresalePhysicalDomestic    ProductType = "PRESALE_PHYSICAL_DOMESTIC"
)
```

一个字段混合了商品形态、销售模式、贸易模式、履约模式四层含义。

**Good：拆成独立事实维度**

```go
type ProductForm string   // PHYSICAL / VIRTUAL
type SaleMode string      // NORMAL / PRESALE / GROUP_BUY
type TradeMode string     // DOMESTIC / CROSS_BORDER
type FulfillmentMode string // DELIVERY / BENEFIT
```

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

拆细后每个维度独立参与不同决策点：

| 决策点 | 相关维度 |
|---|---|
| 购买流程 | SaleMode, ProductForm |
| 是否需要物流 | FulfillmentMode, ProductForm |
| 是否需要报关 | TradeMode |
| 税费计算 | TradeMode, Region, ProductTaxCategory |
| 库存扣减 | ProductForm, SaleMode |
| 权益发放 | FulfillmentMode |

---

## 2. 合并强耦合维度：派生中间模型

拆维度不是越细越好。多个维度总是一起变化、一起判断、共同决定同一个业务语义时，保留原始事实并派生一个中间业务模型。

判断信号：

```text
1. 两个维度几乎总是同时出现；
2. 一个维度的取值依赖另一个维度；
3. 单独看任意一个维度都无法表达真实业务语义；
4. 大量规则都是 A + B + C 的固定组合；
5. 多个维度共同决定一个稳定业务模型；
6. 代码里到处出现同一组组合判断；
7. 一个字段只是另一个字段的派生视图。
```

第 7 条最容易被忽略，因为它是"拆过头"而不是"拆得不够"：`MemberLevel` 已经表达了等级，却又加一个 `IsVip bool`（由等级推出）。`IsVip` 不是独立维度，是**派生视图**——它会和 `MemberLevel` 漂移，各决策点一会读等级、一会读 `IsVip`。应删掉冗余字段，只保留原始维度。

**Bad：强耦合维度被拆散**

```go
if ctx.SaleMode == SaleModePresale && ctx.PaymentStage == PaymentStageDeposit { /* 预售定金 */ }
if ctx.SaleMode == SaleModePresale && ctx.PaymentStage == PaymentStageFinal   { /* 预售尾款 */ }
if ctx.SaleMode == SaleModeNormal  && ctx.PaymentStage == PaymentStageFull    { /* 普通全款 */ }
```

**Good：派生中间模型 + Resolver**

```go
type PurchasePhase string

const (
    PurchasePhaseNormalFullPay    PurchasePhase = "NORMAL_FULL_PAY"
    PurchasePhasePresaleDeposit   PurchasePhase = "PRESALE_DEPOSIT"
    PurchasePhasePresaleFinalPay  PurchasePhase = "PRESALE_FINAL_PAY"
)

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

后续逻辑基于 `PurchasePhase`，不再反复拼 `SaleMode + PaymentStage`。

常见「维度组合 → 中间业务模型」：

| 原始维度组合 | 中间业务模型 |
|---|---|
| SaleMode + PaymentStage | PurchasePhase |
| Region + ProductTaxCategory + AccountType | TaxModel |
| ProductForm + FulfillmentType | FulfillmentModel |
| PromotionType + CouponType + DiscountMode | PromotionModel |
| OrderStatus + PaymentStatus + DeliveryStatus | OrderPhase |

---

## 3. 建立层级维度

维度有明显父子分类时，直接平铺会导致枚举爆炸、规则重复、策略选择困难。

判断信号：

```text
1. 维度有明显父子分类；
2. 上层分类决定大流程，下层分类决定细节；
3. 多个子类型共享同一类规则；
4. 一些规则只关心大类，一些策略关心小类；
5. 枚举值出现大量前缀重复。
```

**Bad**：`ProductCategory` 平铺成 `PHYSICAL_BOOK` / `PHYSICAL_FOOD` / `VIRTUAL_COUPON`……

**Good**：上层 `ProductForm`（PHYSICAL / VIRTUAL）+ 下层 `ProductCategory`（BOOK / FOOD / MEDICINE / COUPON / MEMBERSHIP / COURSE）。

使用原则：

```text
上层维度用于大流程分发；下层维度用于细规则、参数、配置。
```

```go
type FulfillmentStrategyFactory struct {
    strategies map[ProductForm]FulfillmentStrategy
}
```

上层选大策略，下层在策略 / 规则内部处理细节（如「药品需资质」写在 `FulfillmentEligibilityPolicy`）。

---

## 4. 拆业务 Context

不要把所有字段放进一个巨型 `Context`。Context 围绕具体决策点定义。

**Bad：巨型上下文**

```go
type OrderContext struct {
    UserID, MemberLevel, Region, Channel, ProductID string
    ProductForm, ProductCategory, ProductTaxCategory, SaleMode, TradeMode string
    PaymentMethod, PaymentStage, InvoiceType, Address, CouponID string
    PromotionType, InventoryType, OrderStatus, DeliveryStatus, Amount string
}
```

问题：决策点边界不清；每个策略能访问过多字段；隐式耦合；测试数据难构造；新字段污染所有逻辑。

**Good：按决策点拆 Context**

```go
type BuyContext struct {        // UserID, MemberLevel, Region, Channel, ProductForm, SaleMode, TradeMode, Amount }
type PriceContext struct {      // UserID, MemberLevel, Channel, ProductForm, SaleMode, PromotionType, Amount }
type TaxContext struct {        // Region, TradeMode, ProductTaxCategory, AccountType, InvoiceType, Amount }
type PaymentContext struct {    // UserID, Channel, Region, PaymentMethod, PaymentStage, Amount }
type FulfillmentContext struct { // OrderID, ProductForm, ProductCategory, FulfillmentMode, Address }
```

拆分原则：

```text
一个 Context 服务一个决策点；只放该决策点需要的事实；
不要为了省事传全量 Order；不要让策略能访问无关字段；
Context 是业务输入，不是数据库实体；应该稳定、明确、可测试。
```

Context 与 Entity 的区别：

| 类型 | 作用 |
|---|---|
| Entity | 表达领域对象生命周期，例如 Order、Product、User |
| Context | 表达某个决策点需要的事实快照 |

不要把数据库实体直接当所有策略的上下文。上游数据库或协议给的形状若与业务含义不符，在**建模层**翻译（映射 / 派生），不要求改上游；信息确实没存的，列为待确认问题，不要凭空补齐。

```go
// Bad
type PriceStrategy interface { Calculate(order Order) (Money, error) }
// Good
type PriceStrategy interface { Calculate(ctx PriceContext) (Money, error) }
```

---

## 5. 非正交维度与组合爆炸

维度通常不完全正交。常见组合：`地区 × 商品类型`、`渠道 × 支付方式`、`会员等级 × 权益类型`。不要急着把组合做成策略，先判定关系属于哪一类。

### Case 1：非法或不支持组合 → Rule

```text
海外 + 赠品 = 不允许     虚拟商品 + 物流 = 不需要
企业用户 + 个人券 = 不允许   小程序 + PayPal = 不支持
```

写进对应决策点的 Policy，不要每条规则建一个类型：

```go
type PaymentAvailabilityPolicy struct{}

func (p PaymentAvailabilityPolicy) Decide(ctx PaymentContext) error {
    if ctx.Channel == ChannelMiniProgram && ctx.PaymentMethod == PaymentMethodPaypal {
        return errors.New("小程序不支持 PayPal")
    }
    return nil
}
```

不要建 `MiniProgramPaypalPaymentStrategy`——它只是"不支持"，不是一种支付算法。

### Case 2：公式相同，仅参数不同 → Config Matrix

| 地区 | 商品税类 | 税率 |
|---|---|---|
| 大陆 | 普通商品 | 0.13 |
| 大陆 | 图书 | 0.09 |
| 香港 | 普通商品 | 0 |
| 欧盟 | 数字服务 | 0.2 |

算法始终是 `税额 = 金额 × 税率`，就不要用策略：

```go
rate := taxRateConfig.Rate(ctx.Region, ctx.ProductTaxCategory)
tax := ctx.Amount.Mul(rate)
```

### Case 3：多因素分别贡献结果 → Pipeline

```text
最终税费 = 基础税 + 地区附加税 + 商品特殊税 - 主体减免
```

```go
type TaxStep interface {
    Supports(ctx TaxContext) bool
    Apply(ctx TaxContext, result *TaxResult) error
}
```

适合：多个维度分别贡献一部分结果，不是互斥选择一个算法，处理过程可组合。互斥算法不要用 Pipeline。

### Case 4：多维共同选择互斥算法 → Decision Table + Strategy

```text
欧盟 + 数字服务 -> 欧盟数字服务 VAT    美国 + 实物商品 -> 美国销售税
大陆 + 普通商品 -> 大陆价内税          香港 + 任意商品 -> 免税
```

不要强行选一个主维度。建模为：`多个事实维度 -> 中间业务模型 -> 策略`。

### Case 5：组合形成稳定业务概念 → Intermediate Model + Resolver

当多个事实维度共同决定一个稳定业务概念时，先派生中间模型，再据其分发策略。

```go
type TaxModel string

const (
    TaxModelFree                TaxModel = "TAX_FREE"
    TaxModelMainlandIncludedTax TaxModel = "MAINLAND_INCLUDED_TAX"
    TaxModelEUVAT               TaxModel = "EU_VAT"
    TaxModelEUDigitalServiceVAT TaxModel = "EU_DIGITAL_SERVICE_VAT"
    TaxModelUSSalesTax          TaxModel = "US_SALES_TAX"
)
```

```text
TaxContext -> TaxEligibilityPolicy -> TaxModelResolver -> TaxStrategyFactory -> TaxStrategy.Calculate()
```

决策表（更具体规则优先，默认规则显式，未匹配必须报错）：

| 优先级 | 地区 | 商品税类 | 主体 | 税务模型 |
|---|---|---|---|---|
| 100 | 任意 | 免税商品 | 任意 | TAX_FREE |
| 90 | 香港 | 任意 | 任意 | TAX_FREE |
| 80 | 欧盟 | 数字服务 | 个人 | EU_DIGITAL_SERVICE_VAT |
| 70 | 欧盟 | 普通商品 | 任意 | EU_VAT |
| 60 | 美国 | 实物商品 | 任意 | US_SALES_TAX |
| 50 | 大陆 | 普通商品 | 任意 | MAINLAND_INCLUDED_TAX |

这是**真实业务模型策略**，不是盲目组合策略。只有当一个组合代表独立算法或独立业务模型时才建策略：

```go
type EUDigitalServiceVATStrategy struct{}
type USSalesTaxStrategy struct{}
type MainlandIncludedTaxStrategy struct{}
type TaxFreeStrategy struct{}
```

### 笛卡尔积边界

不要为所有组合建策略类型：

```go
type MainlandNormalGoodsPersonalNormalInvoiceTaxStrategy struct{}
type EUDigitalServicePersonalNoInvoiceTaxStrategy struct{}
```

若维度为 地区 4 × 商品税类 4 × 发票类型 3 × 主体 2 = 96 种组合，会爆炸。

| 类型 | 特点 | 是否推荐 |
|---|---|---|
| 笛卡尔积组合策略 | 每个组合一个类型 | 不推荐 |
| 真实业务模型策略 | 每种独立算法一个类型 | 推荐 |
| 决策表选择策略 | 多个维度共同决定策略 | 推荐 |
| 配置矩阵 | 公式相同，只是参数不同 | 推荐 |
| 规则 | 判断组合是否合法 | 推荐 |

---

## 6. 判断表

| 现象 | 问题 | 处理方式 |
|---|---|---|
| 一个枚举值越来越长 | 维度过粗 | 拆细维度 |
| 一个字段同时表达类型、状态、来源 | 语义混杂 | 拆细维度 |
| 大量 if 判断同一组字段组合 | 强耦合组合 | 合并成中间业务模型 |
| 多个字段单独看没有业务意义 | 维度过细 | 合并 |
| 字段是另一个字段的派生视图（如 IsVip 由等级推出） | 冗余维度 | 删除冗余字段，只留原始维度 |
| 枚举值有明显父子分类 | 层级缺失 | 建层级维度 |
| 上层决定流程，下层决定参数 | 平铺导致复杂 | 建层级维度 |
| 所有策略都传巨大 Context | 上下文污染 | 按决策点拆 Context |
| 策略访问了无关字段 | 职责泄漏 | 缩小 Context |
| 同一判断复制到多个策略 | 缺少共享规则 | 抽 Rule |
| 新增组合导致策略类暴涨 | 组合爆炸 | 中间模型 / 决策表 |
