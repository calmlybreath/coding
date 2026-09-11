# Go 实现指南

## 目录

- 小接口
- 高内聚多方法接口
- 组合接口与抽象工厂
- Resolver 选择逻辑
- 多维 Decision Table
- 累加型 Pipeline

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

## 2. Allow Cohesive Multi-Method Policies

“小接口优先”不等于“接口只能有一个方法”。当多个方法属于同一业务能力、同一变化原因和同一生命周期，而且所有实现都必须提供时，可以使用高内聚的多方法接口。

```go
type PaymentLifecyclePolicy interface {
    Authorize(ctx PaymentContext) (Authorization, error)
    Capture(ctx PaymentContext, auth Authorization) (PayResult, error)
    Void(ctx PaymentContext, auth Authorization) error
}
```

如果退款和对账不属于所有支付实现，或者具有独立变化原因，则拆开：

```go
type RefundPolicy interface {
    Refund(ctx RefundContext) (RefundResult, error)
}

type ReconcilePolicy interface {
    Reconcile(ctx ReconcileContext) error
}
```

策略私有且依赖内部实现细节的前置校验可以保留在实现内部：

```go
func (p PaypalPaymentPolicy) Authorize(ctx PaymentContext) (Authorization, error) {
    if err := p.validateAccount(ctx); err != nil {
        return Authorization{}, err
    }

    return p.client.Authorize(ctx)
}
```

跨多个支付策略共用的渠道、地区或资格限制应抽为独立 `Rule` / `Validator`，在 Resolver 或 Strategy 执行前统一调用，避免每个实现复制一份。

---

## 3. Compose Small Interfaces And Provide Complete Modules

调用方应依赖自己真正使用的小接口：

```go
type BuyPolicy interface {
    Buy(ctx BuyContext) (BuyResult, error)
}

type PricePolicy interface {
    CalculatePrice(ctx PriceContext) (Money, error)
}

type InventoryPolicy interface {
    Reserve(ctx InventoryContext) error
}

type FulfillmentPolicy interface {
    Fulfill(ctx FulfillmentContext) error
}
```

如果同一个实现对象必须具备完整能力，可以用组合接口收束：

```go
type CompleteProductPolicy interface {
    BuyPolicy
    PricePolicy
    InventoryPolicy
    FulfillmentPolicy
}

type DigitalProductPolicy struct {
    BuyPolicy
    PricePolicy
    InventoryPolicy
    FulfillmentPolicy
}

var _ CompleteProductPolicy = (*DigitalProductPolicy)(nil)
```

如果这些能力由不同对象实现，使用模块工厂统一提供，而不是分散注册：

```go
type ProductTypeModule interface {
    BuyPolicy() BuyPolicy
    PricePolicy() PricePolicy
    InventoryPolicy() InventoryPolicy
    FulfillmentPolicy() FulfillmentPolicy
}

type ProductPolicyFactory interface {
    Create(productType ProductType) (ProductTypeModule, error)
}

type DefaultProductTypeModule struct {
    buy         BuyPolicy
    price       PricePolicy
    inventory   InventoryPolicy
    fulfillment FulfillmentPolicy
}

func NewProductTypeModule(
    buy BuyPolicy,
    price PricePolicy,
    inventory InventoryPolicy,
    fulfillment FulfillmentPolicy,
) (ProductTypeModule, error) {
    if buy == nil || price == nil || inventory == nil || fulfillment == nil {
        return nil, errors.New("incomplete product policy module")
    }

    return &DefaultProductTypeModule{
        buy:         buy,
        price:       price,
        inventory:   inventory,
        fulfillment: fulfillment,
    }, nil
}

func (m *DefaultProductTypeModule) BuyPolicy() BuyPolicy {
    return m.buy
}

func (m *DefaultProductTypeModule) PricePolicy() PricePolicy {
    return m.price
}

func (m *DefaultProductTypeModule) InventoryPolicy() InventoryPolicy {
    return m.inventory
}

func (m *DefaultProductTypeModule) FulfillmentPolicy() FulfillmentPolicy {
    return m.fulfillment
}

var _ ProductTypeModule = (*DefaultProductTypeModule)(nil)
var _ ProductPolicyFactory = (*DefaultProductPolicyFactory)(nil)
```

注册表只注册完整模块：

```go
type DefaultProductPolicyFactory struct {
    modules map[ProductType]ProductTypeModule
}

func (f DefaultProductPolicyFactory) Create(
    productType ProductType,
) (ProductTypeModule, error) {
    module, ok := f.modules[productType]
    if !ok {
        return nil, errors.New("unsupported product type")
    }

    return module, nil
}
```

注意：

- 编译期接口检查只能保证方法集完整，不能保证返回的 Policy 非空；
- 构造 `ProductTypeModule` 时还需要校验各 Policy 均已提供；
- 新增 `ProductType` 时，应在一个装配入口注册完整模块，并用测试覆盖所有枚举值；
- 不要因为能力必须成套提供，就把不同决策点合并成万能 `ProductStrategy`。

---

## 4. Resolver Should Own Selection Logic

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

## 5. Use Decision Table For Multi-Dimension Selection

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

## 6. Use Pipeline For Accumulative Logic

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
