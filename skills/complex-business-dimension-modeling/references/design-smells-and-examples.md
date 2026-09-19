# 设计异味与专项示例

## 目录

- 过大的维度与万能 Strategy
- 会员等级的角色分析
- 成套能力分散注册

## Detect Oversized Dimension

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
type PriceStrategy interface {
    Calculate(ctx PriceContext) (Money, error)
}

type CouponEligibilityPolicy interface {
    Validate(ctx CouponContext) error
}

type PointStrategy interface {
    Calculate(ctx PointContext) (Point, error)
}

type InvoicePolicy interface {
    Validate(ctx InvoiceContext) error
}

type DeliveryStrategy interface {
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
PriceStrategy
TaxStrategy
PaymentStrategy
DeliveryStrategy
PromotionStrategy
CouponEligibilityPolicy
InvoicePolicy
PointStrategy
```

---

## Special Guidance: Member Level

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
type MemberBenefitStrategy interface {
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

## Scattered Registration Of Required Capabilities

如果新增商品类型时需要在多个独立注册表中分别注册：

```text
buyStrategies[productType]
priceStrategies[productType]
inventoryStrategies[productType]
fulfillmentStrategies[productType]
```

则很容易遗漏其中一项，而且通常要到运行时特定链路才会暴露。

不要用一个包含 Buy、Price、Inventory、Fulfillment 方法的万能 `ProductStrategy` 修补这个问题。这些方法属于不同决策点，变化原因也可能不同。

推荐保持小接口，并通过 `ProductTypeModule` / `ProductStrategyFactory` 在一个装配入口提供完整能力集：

```text
ProductStrategyFactory
  -> ProductTypeModule
       -> BuyStrategy
       -> PriceStrategy
       -> InventoryStrategy
       -> FulfillmentStrategy
```

需要区分两个目标：

- 小接口解决调用方依赖和能力复用；
- 组合接口和编译期检查保证方法集完整；构造校验、模块工厂和枚举覆盖测试共同检查成套能力完整性。

---
