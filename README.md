# kubeconfig-context-manager-go

多份 kubeconfig 的**确定性合并**与**安全上下文切换**工具，包含 Go 库
（`pkg/kubeconfig`）与命令行工具 `kcm`。

## 构建与测试

```sh
go build ./...
go test ./... -race
```

## 合并语义（优先级）

`Merge` / `kcm merge` 按**调用方给定的顺序**加载多份配置，后者覆盖前者：

- 条目（cluster / user / context）按 **name** 作为键。同名条目由**后出现
  的文件整体替换**（wholesale replacement，不做字段级混合），每次替换都
  记录到 `MergeReport.Overrides`（含 section、条目名、来源文件、胜出文件），
  可用 `kcm explain` 或 `Config.SourceOf(section, name)` 解释最终取值的出处。
- **同一文件内**出现同名条目属于配置错误，返回 `ErrDuplicateEntry`
  （含文件、section、条目名），绝不静默二选一。
- 输出条目**按名称排序**，合并结果只取决于输入顺序，与 map 遍历顺序、
  文件系统遍历顺序无关；同一输入序列重复合并必然得到字节级一致的结果。
- 标量字段（`current-context`、`kind`、`apiVersion`、`preferences`）：
  后出现的**非空值**覆盖先前值；空值不会抹掉已有设置。
- 合并与查找基于哈希索引，复杂度 O(条目总数)，条目规模增长不退化为
  两两比较（见 `TestMergeScalesLinearlyEnough` 与 `BenchmarkMerge`）。

## 扩展字段保留

所有层级（顶层、preferences、cluster、user、context、exec）未识别的字段
以 `yaml.Node` 原样捕获到 `Extensions`（inline map）。读出再写回时：

- 未知字段**逐字保留**（含原始标量风格、多行文本、嵌套结构），不丢失、
  不被重新格式化改写；
- 顶层未知键冲突时后者胜出，并记入 override 报告（section 为
  `extensions`），不产生静默覆盖。

## 路径与凭据处理

- 相对路径字段（`certificate-authority`、`client-certificate`、`client-key`、
  `tokenFile`、exec `command`）在加载时解析为**相对于该文件所在目录**的
  绝对路径（与 kubectl 语义一致）；绝对路径原样保留。
- 写出的配置文件权限为 **0600**；已存在的文件权限不会被放宽，若原文件
  group/other 可读则会被收紧。
- 不可读输入返回 `ErrUnreadableInput`，可通过 `errors.Is` 区分
  `fs.ErrNotExist` 与 `fs.ErrPermission`；YAML 解析失败同样带文件定位。
- `Validate` 检查 context 引用的 cluster/user 是否存在，缺失时返回
  `ErrDanglingReference`（含 context 名、引用类型与目标名）。

## 上下文切换语义

`Store` 以原子快照（`atomic.Pointer`）发布「配置 + 当前上下文」这一
**整体**：

- 任意时刻读者看到的都是某一次切换后的完整自洽状态，不会出现
  “新 current-context 配旧配置” 的混合态；读者无锁、无等待。
- `Switch` 串行化执行，采用写时复制：先构造新快照，持久化成功后才对
  读者可见；切换不存在的上下文返回 `ErrContextNotFound`（附可用列表），
  状态不变。

### 持久化的原子性与可恢复性

`WriteFileAtomic`：同目录临时文件 → `fsync` → `rename` → 目录 `fsync`。
因此进程在写入中途被杀、或磁盘写满时，磁盘上只会是**切换前的完整文件**
或**切换后的完整文件**，绝不留下截断/损坏文件；重启后重新加载即恢复到
二者之一。持久化失败时内存状态不会推进，内存与磁盘始终一致。

## CLI

```sh
# 合并（后者优先），override 报告打印到 stderr
kcm merge -o merged.yaml base.yaml team.yaml user.yaml

# 只查看覆盖关系，不写文件
kcm explain base.yaml team.yaml user.yaml

# 原子切换 merged.yaml 的 current-context
kcm use -c merged.yaml prod

# 查看当前上下文
kcm current -c merged.yaml
```

## 库用法

```go
merged, report, err := kubeconfig.LoadFiles("base.yaml", "overlay.yaml")
if err != nil { /* ErrUnreadableInput / ErrDuplicateEntry */ }
if err := kubeconfig.Validate(merged); err != nil { /* ErrDanglingReference */ }
for _, ov := range report.Overrides { /* 解释每条覆盖 */ }

store := kubeconfig.NewStore(merged, "merged.yaml") // 第二个参数为空则仅内存切换
if err := store.Switch("prod"); err != nil { /* ErrContextNotFound 或持久化错误 */ }
cur, snap := store.Current(), store.Snapshot()      // 始终自洽
```

## 测试覆盖

| 需求 | 测试 |
| --- | --- |
| 多文件覆盖顺序、确定性输出 | `TestMergeLaterFileWins`、`TestMergeOutputSortedAndDeterministic`、`TestLoadFilesMergeOrderAndReport` |
| 扩展字段保留 | `TestRoundTripPreservesUnknownFields`、`TestMergeExtensionCollisionReported` |
| 并发切换自洽 | `TestConcurrentSwitchAndRead`（`-race`） |
| 中断/失败恢复 | `TestSwitchPersistsAtomicallyAndRecovers`、`TestFailedPersistLeavesConsistentState`、`TestWriteFileAtomicFailureLeavesOriginal` |
| 异常输入与定位 | `TestLoadFileUnreadableInputs`、`TestLoadFileDuplicateEntryFails`、`TestValidateDanglingReference`、`TestSwitchUnknownContextFails` |
| 规模不退化 | `TestMergeScalesLinearlyEnough`、`BenchmarkMerge` |
| 路径解析与权限 | `TestLoadFileResolvesRelativePaths`、`TestWriteFileAtomicPermissionsAndContent` |
