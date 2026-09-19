# 非正交维度与组合爆炸

## 目录

- 非法组合
- 同公式不同参数
- 多因素叠加
- 多维选择互斥算法
- 中间业务模型
- 笛卡尔积策略边界

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

如果只是非法组合，它属于 Rule 语义。简单判断直接放进对应决策点的 Policy，不要每条规则建一个类型。

Examples:

```text
海外 + 赠品 = 不允许
虚拟商品 + 物流 = 不需要
企业用户 + 个人券 = 不允许
小程序 + PayPal = 不支持
```

Use:

```go
type PaymentAvailabilityPolicy struct{}

func (p PaymentAvailabilityPolicy) Decide(ctx PaymentContext) error {
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

## Intermediate Business Model

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
TaxEligibilityPolicy
    ↓
TaxModelResolver
    ↓
TaxStrategyFactory
    ↓
TaxStrategy.Calculate()
```

Example:

```go
type TaxService struct {
    EligibilityPolicy TaxEligibilityPolicy
    ModelResolver     TaxModelResolver
    StrategyFactory   TaxStrategyFactory
}

func (s TaxService) Calculate(ctx TaxContext) (Tax, error) {
    if err := s.EligibilityPolicy.Decide(ctx); err != nil {
        return Tax{}, err
    }

    model, err := s.ModelResolver.Resolve(ctx)
    if err != nil {
        return Tax{}, err
    }

    strategy, err := s.StrategyFactory.Get(model)
    if err != nil {
        return Tax{}, err
    }

    return strategy.Calculate(ctx)
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
type TaxStrategy interface {
    Calculate(ctx TaxContext) (Tax, error)
}
```

```go
type EUDigitalServiceVATStrategy struct{}

func (s EUDigitalServiceVATStrategy) Calculate(ctx TaxContext) (Tax, error) {
    // 欧盟数字服务 VAT 算法
    return Tax{}, nil
}
```

This is not blind combination strategy. It is:

```text
真实业务模型策略
```

---

## Combination Strategy Boundary

### Avoid Cartesian Product Strategy

不要为所有组合都建策略类型。

Bad examples:

```go
type MainlandNormalGoodsPersonalNormalInvoiceTaxStrategy struct{}
type MainlandNormalGoodsEnterpriseSpecialInvoiceTaxStrategy struct{}
type EUDigitalServicePersonalNoInvoiceTaxStrategy struct{}
type USPhysicalGoodsEnterpriseNormalInvoiceTaxStrategy struct{}
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

### Prefer Real Business Model Strategy

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
type EUDigitalServiceVATStrategy struct{}
type USSalesTaxStrategy struct{}
type MainlandIncludedTaxStrategy struct{}
type CrossBorderImportTaxStrategy struct{}
type TaxFreeStrategy struct{}
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
