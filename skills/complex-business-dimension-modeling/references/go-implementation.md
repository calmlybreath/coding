# Go 实现指南

只给关键接口、分发点和结构体形状；代码为示意，按需调整。

## 1. 小接口优先

```go
// Good
type PaymentStrategy interface {
    Pay(ctx PaymentContext) (PayResult, error)
}

// Bad：把不同决策点塞进一个接口
type PaymentStrategy interface {
    Pay(ctx PaymentContext) (PayResult, error)
    Refund(ctx RefundContext) (RefundResult, error)
    Query(ctx QueryContext) (QueryResult, error)
    Close(ctx CloseContext) error
    Reconcile(ctx ReconcileContext) error
}
```

除非这些方法确实属于同一生命周期，且每种实现都必须提供。

## 2. 允许高内聚的多方法接口

"小接口优先"不等于"只能有一个方法"。下述方法属于同一能力、同一变化原因、同一生命周期，且所有实现都必须提供：

```go
type PaymentLifecycleStrategy interface {
    Authorize(ctx PaymentContext) (Authorization, error)
    Capture(ctx PaymentContext, auth Authorization) (PayResult, error)
    Void(ctx PaymentContext, auth Authorization) error
}
```

若退款、对账不由所有实现提供、或变化原因独立，则拆开：

```go
type RefundStrategy interface { Refund(ctx RefundContext) (RefundResult, error) }
type ReconcileStrategy interface { Reconcile(ctx ReconcileContext) error }
```

依赖实现细节的前置校验留在实现内部；跨多个 Strategy 共用的渠道、地区、资格限制抽为 Rule，多个 Rule 组成同一决策时由 Policy 组合。

## 3. 组合接口与抽象工厂

调用方依赖自己真正使用的小接口：

```go
type BuyStrategy interface { Buy(ctx BuyContext) (BuyResult, error) }
type PriceStrategy interface { CalculatePrice(ctx PriceContext) (Money, error) }
type InventoryStrategy interface { Reserve(ctx InventoryContext) error }
type FulfillmentStrategy interface { Fulfill(ctx FulfillmentContext) error }
```

若同一实现对象必须具备完整能力，用组合接口收束并做编译期检查：

```go
type CompleteProductCapabilities interface {
    BuyStrategy
    PriceStrategy
    InventoryStrategy
    FulfillmentStrategy
}

type DigitalProductCapabilities struct {
    BuyStrategy
    PriceStrategy
    InventoryStrategy
    FulfillmentStrategy
}

var _ CompleteProductCapabilities = (*DigitalProductCapabilities)(nil)
```

若这些能力由不同对象实现，用模块工厂统一提供，不要分散注册：

```go
type ProductTypeModule interface {
    BuyStrategy() BuyStrategy
    PriceStrategy() PriceStrategy
    InventoryStrategy() InventoryStrategy
    FulfillmentStrategy() FulfillmentStrategy
}

type ProductStrategyFactory interface {
    Create(productType ProductType) (ProductTypeModule, error)
}

type DefaultProductTypeModule struct {
    buy         BuyStrategy
    price       PriceStrategy
    inventory   InventoryStrategy
    fulfillment FulfillmentStrategy
}

func NewProductTypeModule(
    buy BuyStrategy, price PriceStrategy,
    inventory InventoryStrategy, fulfillment FulfillmentStrategy,
) (ProductTypeModule, error) {
    if buy == nil || price == nil || inventory == nil || fulfillment == nil {
        return nil, errors.New("incomplete product strategy module")
    }
    return &DefaultProductTypeModule{buy, price, inventory, fulfillment}, nil
}

// 每个能力一个 getter：BuyStrategy() / PriceStrategy() / InventoryStrategy() / FulfillmentStrategy()
// 均直接返回对应字段，此处省略。
```

注册表只注册完整模块：

```go
type DefaultProductStrategyFactory struct {
    modules map[ProductType]ProductTypeModule
}

func (f DefaultProductStrategyFactory) Create(productType ProductType) (ProductTypeModule, error) {
    module, ok := f.modules[productType]
    if !ok {
        return nil, errors.New("unsupported product type")
    }
    return module, nil
}

var _ ProductTypeModule = (*DefaultProductTypeModule)(nil)
```

注意：

- 编译期接口检查只能保证方法集完整，不能保证返回的 Strategy 非空；构造 `ProductTypeModule` 时还要校验各 Strategy 均已提供；
- 新增 `ProductType` 时在一个装配入口注册完整模块，并用测试覆盖所有枚举值；
- 不要因为能力必须成套提供，就把不同决策点合并成万能 `ProductStrategy`。

## 4. Resolver 拥有选择逻辑

策略选择逻辑不要散落在业务流程里。一维分发通常就是一个 map 查找：

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

```go
strategy, err := resolver.Resolve(ctx.PaymentMethod)
if err != nil {
    return PayResult{}, err
}
return strategy.Pay(ctx)
```

只有一个实现、或只调用一次时，不要为它单独建 Resolver。

## 5. 多维选择用 Decision Table

多个维度共同决定策略时用决策表。

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

- 决策表要有优先级，更具体规则优先；
- 默认规则必须显式；
- 未匹配必须返回错误；
- 不要把大量 if-else 散落在业务服务里。

## 6. 累加逻辑用 Pipeline

多个处理步骤可以叠加时用 Pipeline。

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

适合：`基础费用 + 地区附加费 + 商品特殊税 + 企业减免`。
不适合：只能选择一个互斥算法——互斥算法用 Strategy 或 Decision Table + Strategy。
