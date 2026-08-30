// Command ctos is a terminal control plane: dashboards of widgets defined in
// YAML. See https://github.com/0xquark/ctos.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/0xquark/ctos/internal/config"
	"github.com/0xquark/ctos/internal/tui"
	"github.com/0xquark/ctos/internal/widget"
	tea "github.com/charmbracelet/bubbletea"

	// Widget types register themselves on import.
	_ "github.com/0xquark/ctos/internal/widgets/clock"
	_ "github.com/0xquark/ctos/internal/widgets/git"
	_ "github.com/0xquark/ctos/internal/widgets/hackernews"
	_ "github.com/0xquark/ctos/internal/widgets/notes"
	_ "github.com/0xquark/ctos/internal/widgets/processes"
	_ "github.com/0xquark/ctos/internal/widgets/system"
)

// version is overridden at release time with -ldflags.
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ctos: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	var (
		configDir  = flag.String("config-dir", "", "config directory (default: $XDG_CONFIG_HOME/ctos or ~/.config/ctos)")
		homeConfig = flag.Bool("home-config", false, "use ~/.ctos instead of the XDG config directory")
		dashboard  = flag.String("dashboard", "", "dashboard to open (default: default_dashboard, else the first one)")
		showVer    = flag.Bool("version", false, "print version and exit")
	)
	flag.Usage = usage
	flag.Parse()

	if *showVer {
		fmt.Printf("ctos %s\n", version)
		return nil
	}

	dir, err := config.ResolveDir(config.Options{Dir: *configDir, HomeConfig: *homeConfig})
	if err != nil {
		return err
	}

	switch flag.Arg(0) {
	case "":
		return start(dir, *dashboard)
	case "init":
		return initConfig(dir)
	case "dashboards":
		return listDashboards(dir)
	case "widgets":
		return listWidgets(flag.Arg(1))
	default:
		return fmt.Errorf("unknown command %q\n\nRun `ctos -h` for usage", flag.Arg(0))
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `ctOS — your terminal's central operating system

Usage:
  ctos [flags]              open the default dashboard
  ctos init                 write a starter config
  ctos dashboards           list available dashboards
  ctos widgets              list available widget types
  ctos widgets <type>       show a widget's configuration

Flags:
`)
	flag.PrintDefaults()
}

// start loads config and runs the dashboard.
func start(dir, want string) error {
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}

	path, err := config.PickDashboard(dir, want, cfg.DefaultDashboard)
	if err != nil {
		return err
	}

	dash, err := config.LoadDashboard(path)
	if err != nil {
		return err
	}

	model, err := tui.New(cfg, dash)
	if err != nil {
		return err
	}

	_, err = tea.NewProgram(model, tea.WithAltScreen()).Run()
	return err
}

// initConfig writes the starter files, creating the sample notes directory so
// a fresh install has something to render.
func initConfig(dir string) error {
	written, skipped, err := config.Scaffold(dir)
	if err != nil {
		return err
	}

	home, err := os.UserHomeDir()
	if err == nil {
		if err := config.EnsureNotesDir(filepath.Join(home, "notes")); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not create ~/notes: %v\n", err)
		}
	}

	for _, p := range written {
		fmt.Println("created " + p)
	}
	for _, p := range skipped {
		fmt.Println("kept    " + p + " (already exists)")
	}
	if len(written) > 0 {
		fmt.Println("\nRun `ctos` to open your dashboard.")
	}
	return nil
}

// listWidgets prints the widget catalogue: every type with its summary, or a
// single type's example configuration, ready to paste under a dashboard's
// "widgets:" key.
func listWidgets(typeName string) error {
	if typeName == "" {
		var b strings.Builder
		for _, spec := range widget.Specs() {
			fmt.Fprintf(&b, "  %-12s %s\n", spec.Name, spec.Summary)
		}
		fmt.Printf("Widget types:\n\n%s\nRun `ctos widgets <type>` for one widget's configuration.\n", b.String())
		return nil
	}

	spec, ok := widget.Lookup(typeName)
	if !ok {
		return fmt.Errorf("unknown widget type %q (known types: %s)", typeName, strings.Join(widget.Types(), ", "))
	}

	fmt.Printf("%s — %s\n", spec.Name, spec.Summary)
	if spec.Example == "" {
		return nil
	}
	fmt.Printf("\nIn a dashboard, under \"widgets:\":\n\n  my-%s:\n", spec.Name)
	for _, line := range strings.Split(spec.Example, "\n") {
		fmt.Println("    " + line)
	}
	return nil
}

func listDashboards(dir string) error {
	paths, err := config.ListDashboards(dir)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no dashboards in %s\n\nRun `ctos init` to create one", config.DashboardsDir(dir))
	}
	for _, p := range paths {
		fmt.Printf("%-16s %s\n", strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)), p)
	}
	return nil
}
