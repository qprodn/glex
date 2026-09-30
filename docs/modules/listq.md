# listq 使用指南：List 读的新方式

glex 里所有列表读（List RPC、导出）曾经各带一段 ~35 行的
filter/order/page-token 解析代码，九个模块还各有一份字节级相同的
`repository/list_option.go`（函数式选项 repack）。2026-09 起这些全部退役
（ADR 0024）：解析逻辑收进 `pkg/listq`，一个模块的 list 读只剩下**一个
`Spec` 声明**这一份私有事实；解析出的 `Query` 值原样穿过
application → domain → repo，仓储端四行套用。

本文教新方式怎么用：第 2 章是三层各写什么的标准走法，第 3 章是过滤
语法速查，第 4 章是两个变体，第 5 章是新增一个 list 读的 checklist。
决策背景见 [ADR 0024](../adr/0024-listq-deep-module-for-list-queries.md)；
dict 是迁移的 tracer bullet，全文以它为例。

## 1. 三个名词

| 名词 | 是什么 | 谁拥有 |
|---|---|---|
| `listq.Spec` | 一个 list 的**唯一私有事实**：可过滤的 ident 集合 + 默认排序 | 应用层，包级 var |
| `listq.Query` | 一次 list 读的完整形状：`{Filter, OrderBy, Limit, Offset}` —— 纯数据值 | 由 `Parse` 产出，三层原样传递 |
| `listq.Window` | 页窗口：持有请求自己的 checksum，负责铸造下一页 token | 应用层，铸 `NextPageToken` 用 |

两条主规则：

- **Spec 是唯一的 per-module 事实**。新增一个可过滤字段就是一行
  `DeclareIdent` 声明，解析、默认值、上限、token 都不用碰。
- **Query 是值不是接口**。它不带 ctx、不带 logger、没有任何行为，
  domain 和 repo 的 List 签名收一个 `listq.Query`，不再把四个参数
  repack 成选项结构体。`listq` 是纯函数包：调用方在自己的 operation
  下记那条 ADR 0011 breadcrumb。

## 2. 标准用法：三层各写什么

以 dict 模块的 ListDictTypes 为例（`internal/application/dict/dict.go`）。

### 2.1 应用层：Spec + Parse + NextToken

```go
// The dict module's two list facts (ADR 0024): the filterable idents plus
// the RuoYi listing order each list defaults to.
var dictTypeListSpec = listq.Spec{
    Idents: []filtering.DeclarationOption{
        filtering.DeclareIdent("dict_name", filtering.TypeString),
        filtering.DeclareIdent("dict_type", filtering.TypeString),
    },
    DefaultOrder: []ordering.Field{{Path: "id"}},
}

func (s *DictApplication) ListDictTypes(ctx context.Context, req *dictv1.ListDictTypesRequest) (*dictv1.DictTypeSet, error) {
    q, page, err := listq.Parse(req, dictTypeListSpec)
    if err != nil {
        logx.Propagate(ctx, s.logger, "dict.list_dict_types", err)
        return nil, err
    }
    types, err := s.svc.ListTypes(ctx, q)
    if err != nil {
        logx.Propagate(ctx, s.logger, "dict.list_dict_types", err)
        return nil, err
    }
    set := &dictv1.DictTypeSet{DictTypes: make([]*dictv1.DictType, 0, len(types))}
    for _, t := range types {
        set.DictTypes = append(set.DictTypes, convertDictType(t))
    }
    set.NextPageToken = page.NextToken(len(types))
    return set, nil
}
```

要点：

- **Spec 放应用层**，包级 var，紧挨着 List 方法。`Parse` 会把标准过滤
  函数（`= != < <= > >= :`、AND/OR/NOT、`timestamp()`、`duration()`）
  自动并进声明，`Idents` 里只写字段声明。
- **ident 名 = proto 字段名 = ent 列名**。这是整条链能泛型的关键约定：
  仓储端 `ents.ApplyFilter` 拿 ident path 直接选列（`sel.C(path)`）。
  本仓 proto 字段与 ent 列同为 snake_case 且同名，写之前对一眼 schema
  即可（如 `phone_number`、`created_at`）。声明一个对不上列的 ident
  **解析期查不出来**：要等 SQL 执行时才报"列不存在"，落地成仓储的
  store-unavailable sentinel——排查时先怀疑这里。
- **`Parse` 是唯一解析点**：坏过滤、坏 token、page size 超上限都在这里
  报错，与应用层其他错误同路——记一条 ADR 0011 breadcrumb 原样上抛。
  `Parse` 不改写请求：生效的 page size 只活在 Query 和 Window 里。
- **`page.NextToken(len(rows))`**：本页拿够 Window 尺寸才铸下一页
  token，短页（最后一页）铸空串。token 绑着请求自己的 checksum
  （对清掉 page_token/page_size 后的请求做 CRC32），所以**翻页中途改
  filter 或 order_by 会让旧 token 校验失败**——这是有意的；改 page_size
  则不影响（它不参与 checksum）。

### 2.2 领域层：签名收一个 Query

domain service 和 repository 接口的 List 签名直接收值，不 repack：

```go
// internal/domain/dict/service/dict.go
func (s *dictImpl) ListTypes(ctx context.Context, q listq.Query) ([]*entity.DictType, error)

// internal/domain/dict/repository/dict_type.go
List(ctx context.Context, q listq.Query) ([]*entity.DictType, error)
```

服务端推导的过滤条件（见 4.2）作为**显式类型化参数**追加在 Query
旁边，绝不改写进 filter 字符串。

### 2.3 基础设施层：四行套用

```go
func (r *dictTypeRepo) List(ctx context.Context, q listq.Query) ([]*dictentity.DictType, error) {
    pos, err := r.data.DB.DictType(ctx).Query().
        Where(
            dicttype.DeletedAtIsNil(),      // 软删谓词照旧手写
            ents.ApplyFilter(q.Filter),      // github.com/go-kratos/aip-go/ents
        ).
        Order(ents.ApplyOrderBy(q.OrderBy)).
        Offset(q.Offset).
        Limit(q.Limit).
        All(ctx)
    if err != nil {
        return nil, dictStoreError(ctx, r.logger, "dict.type.list", err)
    }
    // PO → DO 映射…
}
```

Query 的四个字段正好是 ent 查询要的四样东西：Filter → 谓词、OrderBy →
排序、Offset/Limit → 窗口。仓储永远见不到 page token。需要叠加的固定
谓词（软删、数据权限）继续作为额外 `Where` 写在 `ApplyFilter` 旁边。
错误处理照 ADR 0007：意外故障 log-and-map 成模块 sentinel。

## 3. 过滤与排序语法速查

Spec 的类型声明决定客户端能写什么（`internal/application` 里 14 个
现成 Spec 是全部范例）：

| 声明 | 客户端可写 | 现例 |
|---|---|---|
| `filtering.TypeString` | `dict_name="系统"`、`name:"admin"` | dict、job、user |
| `filtering.TypeInt` | `status=1`、`job_id=42` | user、job_log、audit |
| `filtering.TypeBool` | `enabled=true`、`enabled!=true` | job |
| `filtering.TypeTimestamp` | `created_at>="2026-01-01T00:00:00Z"` | user、job_log |

组合与包含：`AND`/`OR`/`NOT`（或 `&&`/`||`/`!`）、`:` 为包含
（SQL `LIKE %value%`）。比较运算支持 `= != < <= > >=`，左右可以是列
或字面量。这就是 `ents.ApplyFilter` 实际能翻译的集合——文档（proto
注释）里别宣称更多。

排序走 AIP-132：请求 `order_by="dict_sort desc, id"`；不传时落
Spec 的 `DefaultOrder`。**`DefaultOrder` 必须非空**（`Parse` 直接报
错），且要给稳定结果——常见做法是补 `id` 作并列断点（audit、dict
如此），否则同一排序键的行在 offset 翻页间可能跳动。

分页语义：不传 `page_size` 默认 20（`DefaultPageSize`）；上限 500
（`MaxPageSize`），超过按解析失败处理，与坏过滤同路。

## 4. 两个变体

### 4.1 没有 wire 请求的读：手工构造 Query

Query 是纯数据，来源不必是 `Parse`。user 的 Excel 导出（ADR 0017）
不翻页，直接手搓一个超限即拒的 Query（`internal/application/user/excel.go`）：

```go
users, err := s.svc.List(ctx, listq.Query{Filter: filter, OrderBy: orderBy, Limit: exportRowCap + 1}, deptIDs)
```

注意手搓路径**不走 `Parse`，也就不受默认 page size 和 500 上限保护**，
Limit 由调用方自己负责（这里多取一行正是"超过 5 万行就拒绝导出"的
判定手段）。

### 4.2 服务端推导的过滤：显式类型化参数

user 列表的部门下拉按 `dept_id` 过滤，但 `dept_id` 不是声明的
ident——应用层先把部门子树解析成 ID 集，作为独立参数随 Query 下传
（`internal/application/user/user.go`）：

```go
q, page, err := listq.Parse(req, userListSpec)
// …
var deptIDs []int64
if req.GetDeptId() != 0 {
    deptIDs, err = s.depts.ResolveSubtreeIDs(ctx, req.GetDeptId())
}
users, err := s.svc.List(ctx, q, deptIDs)
```

仓储端 `if len(deptIDs) > 0 { query = query.Where(user.DeptIDIn(deptIDs...)) }`。
规则（ADR 0024）：AIP filter 是调用方的线协议，服务端改写它会把手
"这个 filter 支持什么"撕成两处——需要服务端推导的条件一律走显式参数。

## 5. 新增一个 list 读的 checklist

1. **proto**：请求消息带 `filter` / `order_by` / `page_size` /
   `page_token`，响应 Set 带 `next_page_token`。生成代码自动满足
   `listq.Request` 接口，无需额外工作。
2. **应用层**：声明 `listq.Spec`（idents 对着 ent schema 的列名核），
   `listq.Parse` 一句 + 失败 breadcrumb，取数后
   `set.NextPageToken = page.NextToken(len(rows))`。
3. **领域层**：service 与 repository 接口的 List 签名收
   `listq.Query`（服务端推导的条件加显式参数）。
4. **基础设施层**：`Where(软删, ents.ApplyFilter(q.Filter))` +
   `Order(ents.ApplyOrderBy(q.OrderBy)).Offset(q.Offset).Limit(q.Limit)`，
   log-and-map 照旧。
5. **测试**：模块测试直接构造带 filter 的请求打真实解析路径即可
   （dict、pay 均如此）；`pkg/listq` 自带表驱动测试，不需要各模块
   重复测解析本身。

已知的例外只有一个：authn 的在线会话列表保持自己的形状（ADR 0024
豁免——它的 Redis List 契约还没收窄，等收窄后再迁）。

## 6. 附录：关键文件索引

| 文件 | 内容 |
|---|---|
| `docs/adr/0024-listq-deep-module-for-list-queries.md` | 决策全文（为什么收进 pkg、哪些规则守住深模块） |
| `pkg/listq/listq.go` | Parse / Spec / Query / Window / NextToken —— 全部 API |
| `pkg/listq/listq_test.go` | 表驱动测试 |
| `internal/application/dict/dict.go` | tracer bullet：应用层标准用法 |
| `internal/infra/dict/dict_type.go` | tracer bullet：仓储四行套用 |
| `internal/application/user/user.go`、`excel.go` | 变体：deptIDs 显式参数、手工构造 Query |
| `github.com/go-kratos/aip-go/ents` | Filter/OrderBy → ent 谓词/排序的翻译器（ident 按列名取列） |
