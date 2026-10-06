# kubeconfig-context-manager-go

多份访问配置（kubeconfig）的**确定性合并**与**安全上下文切换**工具。
适用于团队在多套集群、多种身份之间分发并逐层覆盖访问配置的场景。

## 构建与测试

```sh
go build ./...
go test ./... -race
```

## 命令行用法

```sh
# 按顺序合并多份配置（后者覆盖前者），原子写出
kcm merge -o merged.yaml base.yaml team.yaml local.yaml

# 合并并切换上下文，结果原子持久化
kcm use-context prod -o merged.yaml base.yaml team.yaml

# 查看合并后的当前上下文
kcm current-context base.yaml team.yaml

# 解释某个条目最终生效值来自哪份文件
kcm explain cluster c1 base.yaml team.yaml
```

文件列表也可以不写在命令行，而通过 `-k a.yaml:b.yaml` 或 `KUBECONFIG`
环境变量（冒号分隔）指定；都未指定时默认 `~/.kube/config`。

## 合并优先级

- 合并顺序即优先级：**命令行（或 KUBECONFIG）中靠后的文件覆盖靠前的文件**。
- 同名条目（cluster / user / context）后者**整体替换**前者；条目在输出中的
  位置保持首次出现时的顺序，因此合并结果只取决于给定顺序，
  与文件系统遍历顺序、读取先后无关。
- 标量字段（`apiVersion`、`kind`、`current-context`）取最后一个非空值。
- **同一份文件内**出现同名条目属于数据错误，合并直接失败，
  报错包含条目类型、名字与来源文件（`MergeError`），可与跨文件覆盖区分。
- 每个条目的最终生效来源记录在 `MergeResult.Provenance` 中，
  可用 `kcm explain` 或 `MergeResult.Explain` 查询。

## 扩展字段保留

所有层级（顶层、cluster/user/context 条目及其内部）未识别的字段
都会保留在 `Extensions` 中，读取后重新写出**不丢失、不改写**；
多份配置的同名顶层扩展键同样按后者覆盖前者合并。
`*-data` 字段以 base64 字符串形式读写，与 kubeconfig 惯例一致。

## 切换语义

- `Manager` 内部持有原子快照：`Current` / `CurrentContext` 与 `Switch`
  并发进行时，任一时刻读到的都是**自洽的完整配置**——要么是完整的旧值，
  要么是完整的新值，绝不出现一半旧一半新的混合状态。
- `Switch(name, path)` 先校验上下文存在（不存在则报错并列出可选项），
  再把切换后的完整配置**原子持久化**，最后才更新内存快照；
  持久化失败时内存状态保持切换前的完整状态。
- 多个 `Switch` 调用被串行化，保证内存状态与磁盘状态一致。

## 持久化与权限

- 写盘采用「同目录临时文件 + fsync + rename + 目录 fsync」：
  进程中断或磁盘写满时，目标文件要么保留完整的旧内容，
  要么是完整的新内容；崩溃残留的临时文件不影响原文件，清理后可重试。
- 配置文件权限固定不超过 `0600`；已有文件权限更严格时保持更严格，
  敏感内容的权限**绝不放宽**。
- 读取时相对路径的凭据字段（`certificate-authority`、`client-certificate`、
  `client-key`）解析为相对于该配置文件所在目录的绝对路径；
  文件不存在、不可读、越权或引用的凭据文件不可读时，
  返回带文件定位的明确失败原因（`LoadError`）。

## 性能

合并与查找使用以条目名为键的索引（map），条目规模增长时
不会退化为逐个两两比较。

## 库 API 概览

```go
cfgs, _ := kubeconfig.LoadFiles(files)          // 读取 + 路径解析 + 权限校验
res, _ := kubeconfig.MergeSources(sources...)   // 确定性合并 + 来源追踪
mgr := kubeconfig.NewManager(res.Config)        // 并发安全的快照管理
ctx, _ := mgr.CurrentContext()                  // 自洽读取
err := mgr.Switch("prod", "merged.yaml")        // 原子切换 + 持久化
```
