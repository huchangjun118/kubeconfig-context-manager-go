// Command kcm merges multiple kubeconfig files into one effective
// config and switches the current context safely.
//
// Usage:
//
//	kcm merge  -o out.yaml file1.yaml file2.yaml ...   # later files win
//	kcm use    -c merged.yaml CONTEXT                  # atomic switch
//	kcm current -c merged.yaml                         # print current context
//	kcm explain -o /dev/null file1.yaml file2.yaml     # show override report
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/huchangjun118/kubeconfig-context-manager-go/pkg/kubeconfig"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "kcm: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("expected a subcommand: merge | use | current | explain")
	}
	switch args[0] {
	case "merge", "explain":
		return cmdMerge(args[0], args[1:])
	case "use":
		return cmdUse(args[1:])
	case "current":
		return cmdCurrent(args[1:])
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func cmdMerge(cmd string, args []string) error {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	out := fs.String("o", "", "output file for the merged config (required for merge)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	files := fs.Args()
	if len(files) == 0 {
		return errors.New("no input kubeconfig files")
	}
	merged, report, err := kubeconfig.LoadFiles(files...)
	if err != nil {
		return err
	}
	if err := kubeconfig.Validate(merged); err != nil {
		return err
	}
	for _, ov := range report.Overrides {
		fmt.Fprintf(os.Stderr, "override: %s %q: %s -> %s\n", ov.Section, ov.Name, ov.FromFile, ov.ToFile)
	}
	if cmd == "explain" {
		return nil
	}
	if *out == "" {
		return errors.New("merge requires -o output file")
	}
	data, err := kubeconfig.Marshal(merged)
	if err != nil {
		return err
	}
	return kubeconfig.WriteFileAtomic(*out, data)
}

func cmdUse(args []string) error {
	fs := flag.NewFlagSet("use", flag.ContinueOnError)
	cfgPath := fs.String("c", "", "merged kubeconfig file to update")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *cfgPath == "" || fs.NArg() != 1 {
		return errors.New("usage: kcm use -c CONFIG CONTEXT")
	}
	cfg, err := kubeconfig.LoadFile(*cfgPath)
	if err != nil {
		return err
	}
	store := kubeconfig.NewStore(cfg, *cfgPath)
	if err := store.Switch(fs.Arg(0)); err != nil {
		return err
	}
	fmt.Printf("switched to context %q\n", fs.Arg(0))
	return nil
}

func cmdCurrent(args []string) error {
	fs := flag.NewFlagSet("current", flag.ContinueOnError)
	cfgPath := fs.String("c", "", "merged kubeconfig file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *cfgPath == "" {
		return errors.New("usage: kcm current -c CONFIG")
	}
	cfg, err := kubeconfig.LoadFile(*cfgPath)
	if err != nil {
		return err
	}
	fmt.Println(cfg.CurrentContext)
	return nil
}
