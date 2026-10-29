// console.go holds the commands that share the console code: `regulus`/`console`
// and `status` (consoleCommand), and `config` (configCommand), with the terminal
// detection, flags and environment they share, and the helpers that reset the
// administrator from the console. The `hub` and `agent` commands are in
// main.go and agent.go. See docs/code-map.md.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"golang.org/x/term"

	"github.com/CodexCircle/Regulus/internal/config"
	"github.com/CodexCircle/Regulus/internal/console"
)

// Exit codes of `regulus status`, as `systemctl status` does: 0 when the hub
// runs, 3 when it does not.
const exitNotRunning = 3

// terminalInfo describes stdin and stdout.
type terminalInfo struct {
	interactive bool
	width       int
	height      int
}

func detectTerminal() terminalInfo {
	in, out := int(os.Stdin.Fd()), int(os.Stdout.Fd())
	if !term.IsTerminal(in) || !term.IsTerminal(out) {
		return terminalInfo{}
	}
	w, h, err := term.GetSize(out)
	if err != nil {
		return terminalInfo{interactive: true}
	}
	return terminalInfo{interactive: true, width: w, height: h}
}

// env is what the commands need from the machine; tests replace it.
type env struct {
	getenv   func(string) string
	stdout   io.Writer
	stderr   io.Writer
	terminal terminalInfo
	system   console.System
	// run opens the full-screen console.
	run func(console.Options) error
	// stderrIsTerminal tells whether it is worth explaining a fallback.
	stderrIsTerminal bool
}

func hostEnv() env {
	return env{
		getenv:           os.Getenv,
		stdout:           os.Stdout,
		stderr:           os.Stderr,
		terminal:         detectTerminal(),
		system:           console.HostSystem(),
		run:              console.Run,
		stderrIsTerminal: term.IsTerminal(int(os.Stderr.Fd())),
	}
}

// commonFlags are the flags of the commands that show the console.
type commonFlags struct {
	config         string
	tui, noTUI     bool
	noColor        bool
	plain, jsonOut bool
	noInput        bool
}

func (c *commonFlags) register(fs *flag.FlagSet, withUI bool) {
	fs.StringVar(&c.config, "config", "", "path to the YAML configuration file (env REGULUS_CONFIG; default /etc/regulus/config.yaml when it exists)")
	fs.BoolVar(&c.plain, "plain", false, "print stable key=value lines, for grep and awk")
	fs.BoolVar(&c.jsonOut, "json", false, "print JSON")
	fs.BoolVar(&c.noColor, "no-color", false, "no colours (also: NO_COLOR)")
	if withUI {
		fs.BoolVar(&c.tui, "tui", false, "open the full-screen console even if it would not open by itself")
		fs.BoolVar(&c.noTUI, "no-tui", false, "never open the full-screen console; print text instead (env REGULUS_NO_TUI=1, configuration console.tui: false)")
		fs.BoolVar(&c.noTUI, "notui", false, "same as --no-tui")
		fs.BoolVar(&c.noInput, "no-input", false, "never ask anything: no full-screen console, and fail if a value is missing")
		fs.BoolVar(&c.noInput, "non-interactive", false, "same as --no-input")
	}
}

func (c commonFlags) format() console.Format {
	switch {
	case c.jsonOut:
		return console.FormatJSON
	case c.plain:
		return console.FormatPlain
	}
	return console.FormatText
}

func (c commonFlags) choice() (console.Choice, error) {
	ch := console.Choice{NoInput: c.noInput, Plain: c.plain, JSON: c.jsonOut}
	switch {
	case c.tui && c.noTUI:
		return ch, errors.New("--tui and --no-tui contradict each other")
	case c.tui:
		t := true
		ch.TUI = &t
	case c.noTUI:
		f := false
		ch.TUI = &f
	}
	return ch, nil
}

func (c commonFlags) applyColor() {
	if c.noColor {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
}

// parseInterspersed parses flags that may come before or after the other
// arguments, and returns those arguments.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var rest []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return rest, nil
		}
		rest = append(rest, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func newFlagSet(name string, e env, usage string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "%s\n\nFlags:\n", usage)
		fs.PrintDefaults()
	}
	return fs
}

// runConsole is `regulus` and `regulus console`.
func runConsole(args []string) int { return consoleCommand(args, hostEnv(), true) }

// runStatus is `regulus status`.
func runStatus(args []string) int { return consoleCommand(args, hostEnv(), false) }

// consoleCommand shows the console when it should open and the status as text
// otherwise. interactive is false for `status`, which never opens the screen.
func consoleCommand(args []string, e env, interactive bool) int {
	name, usage := "status", "Usage: regulus status [--json|--plain] [--config FILE]\n\nPrints what the hub is doing. Exit status 0 when the hub is running, 3 when it is not."
	if interactive {
		name, usage = "console", "Usage: regulus [console] [--no-tui] [--json|--plain] [--config FILE]\n\nOpens the interactive console to check and change the hub. It prints the status\nas text instead when it is not run in a terminal, or when --no-tui,\nREGULUS_NO_TUI=1 or console.tui: false in the configuration says so."
	}
	var c commonFlags
	fs := newFlagSet(name, e, usage)
	c.register(fs, interactive)
	rest, err := parseInterspersed(fs, args)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err == nil && len(rest) > 0 {
		err = fmt.Errorf("unexpected argument %q", rest[0])
	}
	if err != nil {
		_, _ = fmt.Fprintf(e.stderr, "regulus: %v\n", err)
		return 2
	}
	c.applyColor()

	file := config.FindFile(c.config, e.getenv)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if interactive {
		choice, err := c.choice()
		if err != nil {
			_, _ = fmt.Fprintf(e.stderr, "regulus: %v\n", err)
			return 2
		}
		tuiConfig := true
		if cfg, _, err := config.Resolve(file, e.getenv); err == nil {
			tuiConfig = cfg.Console.TUI
		}
		use, reason, err := console.UseTUI(choice, tuiConfig, e.getenv, console.Terminal{
			Interactive: e.terminal.interactive, Width: e.terminal.width, Height: e.terminal.height,
		})
		if err != nil {
			_, _ = fmt.Fprintf(e.stderr, "regulus: %v\n", err)
			return 2
		}
		if use {
			opt := console.Options{
				System:     e.system,
				ConfigFile: file,
				Version:    version,
				Hooks:      hostHooks(e, file),
				Unicode:    console.UnicodeTerminal(e.getenv),
				Refresh:    2 * time.Second,
			}
			if err := e.run(opt); err != nil {
				_, _ = fmt.Fprintf(e.stderr, "regulus: the console failed: %v\n", err)
				return 1
			}
			return 0
		}
		if e.stderrIsTerminal && !c.plain && !c.jsonOut {
			_, _ = fmt.Fprintf(e.stderr, "regulus: not opening the console (%s); printing the status instead\n", reason)
		}
	}

	snap := console.Collect(ctx, e.system, file, version)
	if err := console.WriteStatus(e.stdout, snap, c.format(), e.system.Now()); err != nil {
		_, _ = fmt.Fprintf(e.stderr, "regulus: %v\n", err)
		return 1
	}
	switch {
	case snap.Problem != "":
		return 1
	case !snap.Running && !interactive:
		return exitNotRunning
	}
	return 0
}

func hostHooks(e env, file string) console.Hooks {
	return console.Hooks{
		Restart:    restartService,
		ResetAdmin: func(ctx context.Context) (string, error) { return resetAdminLink(ctx, e, file) },
	}
}

// resetAdminLink makes the password reset link of the admin. As root it does
// the work as the user that owns the data directory, so the database files
// the hub reads are never left owned by root.
func resetAdminLink(ctx context.Context, e env, file string) (string, error) {
	cfg, _, err := config.Resolve(file, e.getenv)
	if err != nil {
		return "", err
	}
	if os.Geteuid() == 0 {
		if uid, gid, ok := ownerOf(cfg.DataDir); ok && uid != 0 {
			return resetAdminAs(ctx, uid, gid, cfg.DataDir, file)
		}
	}
	_, link, err := recoverAdmin(ctx, cfg, "")
	return link, err
}

// ownerOf returns the owner of a directory.
func ownerOf(dir string) (uid, gid uint32, ok bool) {
	fi, err := os.Stat(dir)
	if err != nil {
		return 0, 0, false
	}
	st, isUnix := fi.Sys().(*syscall.Stat_t)
	if !isUnix {
		return 0, 0, false
	}
	return st.Uid, st.Gid, true
}

// resetAdminAs runs `regulus hub reset-admin` as another user.
func resetAdminAs(ctx context.Context, uid, gid uint32, dataDir, file string) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	args := []string{"hub", "reset-admin", "--data-dir", dataDir}
	if file != "" {
		args = append(args, "--config", file)
	}
	cmd := exec.CommandContext(ctx, self, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: gid}}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return strings.TrimSpace(string(out)), nil
}

// restartService restarts the hub's systemd unit, with sudo when this is not
// root and sudo needs no password.
func restartService(ctx context.Context) error {
	args := []string{"systemctl", "restart", console.ServiceUnit}
	if os.Geteuid() != 0 {
		if sudo, err := exec.LookPath("sudo"); err == nil {
			args = append([]string{sudo, "-n"}, args...)
		}
	}
	out, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	return nil
}

// runConfig is `regulus config`.
func runConfig(args []string) int { return configCommand(args, hostEnv()) }

const configUsage = `Usage: regulus config <command> [--config FILE]

Commands:
  list [--json|--plain]   every setting with its value and where the value comes from
  get KEY                 print the value of a setting
  set KEY VALUE           change a setting in the configuration file
  path                    print the configuration file in use

The file is --config, else REGULUS_CONFIG, else /etc/regulus/config.yaml. The
hub reads it at start, so restart the service after 'set' (sudo systemctl
restart regulus). The settings are: `

func configCommand(args []string, e env) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		_, _ = fmt.Fprintln(e.stderr, configUsage+strings.Join(config.Keys(), ", ")+".")
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	sub := args[0]
	var c commonFlags
	fs := newFlagSet("config "+sub, e, configUsage+strings.Join(config.Keys(), ", ")+".")
	c.register(fs, false)
	rest, err := parseInterspersed(fs, args[1:])
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintf(e.stderr, "regulus: %v\n", err)
		return 2
	}
	fail := func(format string, a ...any) int {
		_, _ = fmt.Fprintf(e.stderr, "regulus: "+format+"\n", a...)
		return 1
	}
	usageErr := func(format string, a ...any) int {
		_, _ = fmt.Fprintf(e.stderr, "regulus: "+format+"\n", a...)
		return 2
	}

	file := config.FindFile(c.config, e.getenv)
	switch sub {
	case "path":
		if len(rest) != 0 {
			return usageErr("config path takes no arguments")
		}
		if file == "" {
			return fail("no configuration file: pass --config FILE or set REGULUS_CONFIG (the package installs %s)", config.DefaultFile)
		}
		_, _ = fmt.Fprintln(e.stdout, file)
	case "list":
		if len(rest) != 0 {
			return usageErr("config list takes no arguments")
		}
		_, entries, err := config.Resolve(file, e.getenv)
		if err != nil {
			return fail("%v", err)
		}
		if err := console.WriteSettings(e.stdout, entries, c.format()); err != nil {
			return fail("%v", err)
		}
	case "get":
		if len(rest) != 1 {
			return usageErr("usage: regulus config get KEY")
		}
		if _, ok := config.Lookup(rest[0]); !ok {
			return usageErr("unknown setting %q (known: %s)", rest[0], strings.Join(config.Keys(), ", "))
		}
		cfg, _, err := config.Resolve(file, e.getenv)
		if err != nil {
			return fail("%v", err)
		}
		v, _ := cfg.Get(rest[0])
		_, _ = fmt.Fprintln(e.stdout, v)
	case "set":
		if len(rest) != 2 {
			return usageErr("usage: regulus config set KEY VALUE")
		}
		key, raw := rest[0], rest[1]
		setting, ok := config.Lookup(key)
		if !ok {
			return usageErr("unknown setting %q (known: %s)", key, strings.Join(config.Keys(), ", "))
		}
		if file == "" {
			return fail("no configuration file to change: pass --config FILE or set REGULUS_CONFIG (the package installs %s)", config.DefaultFile)
		}
		value, err := config.SetFile(file, key, raw)
		if err != nil {
			return fail("%v", err)
		}
		_, _ = fmt.Fprintf(e.stdout, "%s = %s\n", key, value)
		if setting.Env != "" && e.getenv(setting.Env) != "" {
			_, _ = fmt.Fprintf(e.stderr, "note: %s is set in the environment and overrides the file\n", setting.Env)
		}
		if setting.Restart {
			_, _ = fmt.Fprintf(e.stderr, "Saved in %s. The hub reads it at start: sudo systemctl restart %s\n", file, console.ServiceUnit)
		} else {
			_, _ = fmt.Fprintf(e.stderr, "Saved in %s.\n", file)
		}
	default:
		return usageErr("unknown config command %q\n\n%s%s.", sub, configUsage, strings.Join(config.Keys(), ", "))
	}
	return 0
}
