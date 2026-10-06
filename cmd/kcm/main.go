// kcm 是多文件 kubeconfig 合并与上下文切换命令行工具。
//
// 用法：
//
//	kcm merge  -o merged.yaml file1 file2 ...   按顺序合并（后者覆盖前者）
//	kcm use-context NAME [-o path] file1 ...    合并并原子切换上下文
//	kcm current-context file1 ...               打印合并后的当前上下文
//	kcm explain KIND NAME file1 ...             解释条目生效值来源
//
// 文件列表也可用 -k/--kubeconfig 以冒号分隔指定，或读取 KUBECONFIG 环境变量。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/huchangjun118/kubeconfig-context-manager-go/pkg/kubeconfig"
	"gopkg.in/yaml.v3"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "merge":
		err = cmdMerge(os.Args[2:])
	case "use-context":
		err = cmdUseContext(os.Args[2:])
	case "current-context":
		err = cmdCurrentContext(os.Args[2:])
	case "explain":
		err = cmdExplain(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "kcm: "+err.Error())
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  kcm merge -o out.yaml [-k a:b:c] [files...]
  kcm use-context NAME [-o path] [-k a:b:c] [files...]
  kcm current-context [-k a:b:c] [files...]
  kcm explain KIND NAME [-k a:b:c] [files...]   KIND ∈ cluster|user|context`)
}

// resolveFiles 汇总命令行文件、-k 选项与 KUBECONFIG 环境变量，保持给定顺序。
func resolveFiles(kflag string, args []string) ([]string, error) {
	var files []string
	files = append(files, args...)
	if kflag != "" {
		files = append(files, splitList(kflag)...)
	}
	if len(files) == 0 {
		if env := os.Getenv("KUBECONFIG"); env != "" {
			files = splitList(env)
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			files = []string{filepath.Join(home, ".kube", "config")}
		}
	}
	return files, nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, string(os.PathListSeparator)) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func loadAndMerge(files []string) (*kubeconfig.Manager, *kubeconfig.MergeResult, error) {
	configs, err := kubeconfig.LoadFiles(files)
	if err != nil {
		return nil, nil, err
	}
	sources := make([]kubeconfig.Source, len(files))
	for i, f := range files {
		sources[i] = kubeconfig.Source{Name: f, Config: configs[i]}
	}
	res, err := kubeconfig.MergeSources(sources...)
	if err != nil {
		return nil, nil, err
	}
	return kubeconfig.NewManager(res.Config), res, nil
}

func cmdMerge(args []string) error {
	fs := flag.NewFlagSet("merge", flag.ContinueOnError)
	out := fs.String("o", "", "output file (required)")
	kflag := fs.String("k", "", "path-list of kubeconfig files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return fmt.Errorf("merge requires -o output file")
	}
	files, err := resolveFiles(*kflag, fs.Args())
	if err != nil {
		return err
	}
	mgr, _, err := loadAndMerge(files)
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(mgr.Current())
	if err != nil {
		return err
	}
	return kubeconfig.AtomicWriteFile(*out, data, 0o600)
}

// splitFlags 把混排的 flag 与位置参数分开，使 -o/-k 可以出现在上下文名之后。
// valueFlags 列出需要带值的 flag 名。
func splitFlags(args []string, valueFlags ...string) (flagArgs, positional []string) {
	takesValue := map[string]bool{}
	for _, f := range valueFlags {
		takesValue[f] = true
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			flagArgs = append(flagArgs, a)
			name := strings.TrimLeft(a, "-")
			if !strings.Contains(name, "=") && takesValue[name] && i+1 < len(args) {
				i++
				flagArgs = append(flagArgs, args[i])
			}
		} else {
			positional = append(positional, a)
		}
	}
	return flagArgs, positional
}

func cmdUseContext(args []string) error {
	fs := flag.NewFlagSet("use-context", flag.ContinueOnError)
	out := fs.String("o", "", "persist merged result to this file (default: in-memory only)")
	kflag := fs.String("k", "", "path-list of kubeconfig files")
	flagArgs, positional := splitFlags(args, "o", "k")
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(positional) < 1 {
		return fmt.Errorf("use-context requires a context name")
	}
	name := positional[0]
	files, err := resolveFiles(*kflag, positional[1:])
	if err != nil {
		return err
	}
	mgr, _, err := loadAndMerge(files)
	if err != nil {
		return err
	}
	target := *out
	if target == "" {
		target = os.DevNull // 仅校验切换，不落盘
	}
	if err := mgr.Switch(name, target); err != nil {
		return err
	}
	fmt.Printf("switched to context %q\n", name)
	return nil
}

func cmdCurrentContext(args []string) error {
	fs := flag.NewFlagSet("current-context", flag.ContinueOnError)
	kflag := fs.String("k", "", "path-list of kubeconfig files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	files, err := resolveFiles(*kflag, fs.Args())
	if err != nil {
		return err
	}
	mgr, _, err := loadAndMerge(files)
	if err != nil {
		return err
	}
	ctx, err := mgr.CurrentContext()
	if err != nil {
		return err
	}
	fmt.Println(ctx.Name)
	return nil
}

func cmdExplain(args []string) error {
	fs := flag.NewFlagSet("explain", flag.ContinueOnError)
	kflag := fs.String("k", "", "path-list of kubeconfig files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 2 {
		return fmt.Errorf("explain requires KIND and NAME")
	}
	kind, name := fs.Arg(0), fs.Arg(1)
	files, err := resolveFiles(*kflag, fs.Args()[2:])
	if err != nil {
		return err
	}
	_, res, err := loadAndMerge(files)
	if err != nil {
		return err
	}
	fmt.Println(res.Explain(kind, name))
	return nil
}
