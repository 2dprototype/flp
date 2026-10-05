// Command flp provides the command-line interface for the flp library:
// semantic diff, inspection, git integration, and a JSON-RPC bridge
// for FL Studio (.flp) project files.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	flp "github.com/2dprototype/flp/flpdiff"
	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
)

// version is reported by `flp --version`.
const version = "v0.0.2"

// Exit codes returned by the diff command.
const (
	exitIdentical   = 0
	exitDifferences = 1
	exitError       = 2
)

// ───────────────────────── styling ─────────────────────────

// painter wraps strings in ANSI SGR sequences. When disabled (piped output,
// NO_COLOR, TERM=dumb, or --no-color), every method returns the input
// unchanged so nothing leaks into files or other programs.
//
// All CLI output flows through colorable writers, which translate these
// codes into native Win32 console calls on Windows and pass them through
// unchanged on Unix.
type painter struct{ enabled bool }

func newPainter(enabled bool) painter { return painter{enabled: enabled} }

func (p painter) wrap(code, s string) string {
	if !p.enabled || s == "" {
		return s
	}
	return code + s + "\x1b[0m"
}

func (p painter) bold(s string) string   { return p.wrap("\x1b[1m", s) }
func (p painter) dim(s string) string    { return p.wrap("\x1b[2m", s) }
func (p painter) red(s string) string    { return p.wrap("\x1b[31m", s) }
func (p painter) green(s string) string  { return p.wrap("\x1b[32m", s) }
func (p painter) yellow(s string) string { return p.wrap("\x1b[33m", s) }
func (p painter) cyan(s string) string   { return p.wrap("\x1b[36m", s) }

// ───────────────────────── help ─────────────────────────

type helpRow struct {
	left  string // command / flag, ASCII only
	right string // description (may be empty)
}

type helpSection struct {
	title string
	rows  []helpRow
}

var helpSections = []helpSection{
	{"USAGE", []helpRow{
		{"flp [options] <A.flp> <B.flp>", "Compare two projects"},
		{"flp info <file.flp> [options]", "Inspect a single project"},
		{"flp git-setup [options]", "Configure git's FLP diff driver"},
		{"flp git-verify", "Check the current git setup"},
		{"flp bridge", "JSON-RPC (one request on stdin)"},
	}},
	{"COMMANDS", []helpRow{
		{"diff          (default)", "Compare two .flp files semantically"},
		{"info", "Show metadata, channels, patterns and mixer"},
		{"git-setup", "Register flp as git's *.flp diff driver"},
		{"git-verify", "Diagnose git integration issues"},
		{"git-driver", "Internal: invoked by git's external-diff protocol"},
		{"bridge", "Read one JSON request from stdin, write one response"},
	}},
	{"DIFF OPTIONS", []helpRow{
		{"-v, --verbose", "Show every individual clip change (not collapsed)"},
		{"    --color", "Force colored output"},
		{"    --no-color", "Disable colored output"},
	}},
	{"INFO OPTIONS", []helpRow{
		{"-f, --format F", "Output format: text (default), json, canonical"},
	}},
	{"GIT-SETUP OPTIONS", []helpRow{
		{"    --global", "Configure for all repositories (user scope)"},
		{"    --textconv", "Use git's native textconv instead of external diff"},
		{"    --lfs", "Also track *.flp with Git LFS"},
	}},
	{"GENERAL", []helpRow{
		{"-h, --help", "Show this help"},
		{"-V, --version", "Show version"},
	}},
	{"ENVIRONMENT", []helpRow{
		{"NO_COLOR", "If set and non-empty, disables colored output"},
		{"TERM=dumb", "Also disables colored output"},
	}},
	{"EXIT CODES", []helpRow{
		{"0", "Projects identical (diff), or command succeeded"},
		{"1", "Projects differ (diff)"},
		{"2", "Invalid arguments, parse error, or I/O error"},
	}},
	{"EXAMPLES", []helpRow{
		{"flp before.flp after.flp", ""},
		{"flp -v mix-v1.flp mix-v2.flp", ""},
		{"flp info song.flp", ""},
		{"flp info song.flp --format json", ""},
		{"flp git-setup", ""},
	}},
}

// renderHelp builds the help text with aligned columns and styling.
// Column widths are computed per section so each block reads cleanly.
func renderHelp(p painter) string {
	var b strings.Builder
	b.WriteString("FLP Studio project diff and inspection.\n")
	for _, sec := range helpSections {
		b.WriteString("\n")
		b.WriteString(p.bold(sec.title))
		b.WriteString("\n")

		maxLeft := 0
		for _, r := range sec.rows {
			if n := len(r.left); n > maxLeft {
				maxLeft = n
			}
		}
		hasRight := false
		for _, r := range sec.rows {
			if r.right != "" {
				hasRight = true
				break
			}
		}
		for _, r := range sec.rows {
			line := "  " + p.cyan(r.left)
			if hasRight {
				// Pad based on raw width, not the ANSI-wrapped string.
				line += strings.Repeat(" ", maxLeft-len(r.left))
				line += "  " + p.dim(r.right)
			}
			b.WriteString(strings.TrimRight(line, " "))
			b.WriteString("\n")
		}
	}
	return b.String()
}

// ───────────────────────── streams ─────────────────────────

// streams bundles the process streams so the CLI is testable.
type streams struct {
	stdin       io.Reader
	stdout      io.Writer
	stderr      io.Writer
	stdoutFile  *os.File // raw handle, needed by git-driver
	stderrFile  *os.File
	stdoutIsTTY bool
}

func main() {
	// colorable wraps ANSI to native Win32 console calls on Windows and is a
	// no-op on Unix. All CLI output flows through these writers.
	stdout := colorable.NewColorableStdout()
	stderr := colorable.NewColorableStderr()
	os.Exit(run(os.Args[1:], streams{
		stdin:       os.Stdin,
		stdout:      stdout,
		stderr:      stderr,
		stdoutFile:  os.Stdout, // git-driver needs the raw file
		stderrFile:  os.Stderr,
		stdoutIsTTY: isTerminal(os.Stdout),
	}))
}

// isTerminal reports whether f is attached to a terminal. Handles native
// Windows consoles and Cygwin/MSYS ptys.
func isTerminal(f *os.File) bool {
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}

// ───────────────────────── dispatcher ─────────────────────────

// run dispatches the CLI and returns the process exit code.
func run(argv []string, s streams) int {
	if len(argv) == 0 {
		p := newPainter(useColor(s, nil))
		fmt.Fprint(s.stderr, renderHelp(p))
		return exitError
	}

	p := newPainter(useColor(s, scanColorOverride(argv)))

	// Help/version can appear anywhere among the flags: `flp --color --help`.
	if argv[0] == "help" {
		fmt.Fprint(s.stdout, renderHelp(p))
		return exitIdentical
	}
	if argv[0] == "version" {
		fmt.Fprintf(s.stdout, "%s %s\n", p.bold("flp"), version)
		return exitIdentical
	}
	for _, a := range argv {
		switch a {
		case "--help", "-h":
			fmt.Fprint(s.stdout, renderHelp(p))
			return exitIdentical
		case "--version", "-V":
			fmt.Fprintf(s.stdout, "%s %s\n", p.bold("flp"), version)
			return exitIdentical
		}
	}

	switch argv[0] {
	case "info":
		return runInfo(argv[1:], s, p)
	case "git-setup":
		return runGitSetup(argv[1:], s, p)
	case "git-verify":
		return runGitVerify(argv[1:], s, p)
	case "git-driver":
		return runGitDriver(argv[1:], s)
	case "bridge":
		return flp.BridgeMain(s.stdin, s.stdout, s.stderr)
	}
	return runDiff(argv, s, p)
}

// scanColorOverride finds the last --color / --no-color flag anywhere in argv.
func scanColorOverride(argv []string) *bool {
	var result *bool
	for _, a := range argv {
		switch a {
		case "--color":
			t := true
			result = &t
		case "--no-color":
			f := false
			result = &f
		}
	}
	return result
}

// useColor decides whether to emit ANSI codes, honouring an explicit
// override, TTY detection, NO_COLOR, and TERM=dumb.
func useColor(s streams, override *bool) bool {
	if override != nil {
		return *override
	}
	if !s.stdoutIsTTY {
		return false
	}
	if v, ok := os.LookupEnv("NO_COLOR"); ok && v != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	return true
}

// ───────────────────────── diff ─────────────────────────

func runDiff(argv []string, s streams, p painter) int {
	args := []string{}
	verbose := false
	for _, a := range argv {
		switch a {
		case "--verbose", "-v":
			verbose = true
		case "--color", "--no-color":
			// consumed globally by scanColorOverride
		default:
			args = append(args, a)
		}
	}
	if len(args) != 2 {
		fmt.Fprintf(s.stderr, "%s %s\n", p.bold("Usage:"), "flp [options] <A.flp> <B.flp>")
		fmt.Fprintln(s.stderr, p.dim("Run `flp --help` for the full command reference."))
		return exitError
	}
	pathA, pathB := args[0], args[1]

	projA, err := loadProject(pathA)
	if err != nil {
		return handleError(err, s, p)
	}
	projB, err := loadProject(pathB)
	if err != nil {
		return handleError(err, s, p)
	}

	result := flp.CompareProjects(projA, projB)
	title := fmt.Sprintf("%s vs %s", filepath.Base(pathA), filepath.Base(pathB))
	body := flp.RenderSummary(result, flp.RenderSummaryOptions{Title: title, Verbose: verbose})

	if p.enabled {
		// Marker coloring from the library, then our structural overlay.
		body = flp.ColorizeSummary(body)
		body = decorateDiffBody(body, p)
	}
	fmt.Fprintln(s.stdout, body)

	if !flp.DiffSummaryHasChanges(result.Summary) {
		return exitIdentical
	}
	if !verbose && hasCollapsedGroups(result) {
		fmt.Fprintln(s.stdout, "")
		fmt.Fprintf(s.stdout, "%s %s\n", p.yellow("Hint:"), "run with -v to see every individual clip change.")
	}
	return exitDifferences
}

// decorateDiffBody adds styling to the structural lines of a RenderSummary
// output: the title, the horizontal rule, the Summary: line, and section
// headers. The per-change marker lines are already colored by
// flp.ColorizeSummary and are left alone here.
func decorateDiffBody(body string, p painter) string {
	if !p.enabled {
		return body
	}
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "FLP Diff: "):
			lines[i] = p.bold(line)
		case isRuleLine(line):
			lines[i] = p.dim(line)
		case strings.HasPrefix(line, "Summary: "):
			rest := strings.TrimPrefix(line, "Summary: ")
			value := p.yellow(rest)
			if rest == "No changes" {
				value = p.green(rest)
			}
			lines[i] = p.bold("Summary:") + " " + value
		case isSectionHeader(line):
			lines[i] = p.bold(p.cyan(line))
		}
	}
	return strings.Join(lines, "\n")
}

func isRuleLine(line string) bool {
	if len(line) < 3 {
		return false
	}
	for _, r := range line {
		if r != '-' {
			return false
		}
	}
	return true
}

func isSectionHeader(line string) bool {
	switch line {
	case "Metadata:", "Channels:", "Patterns:", "Mixer:", "Arrangements:", "Opaque blobs:":
		return true
	}
	return false
}

func hasCollapsedGroups(r flp.DiffResult) bool {
	for _, ad := range r.ArrangementChanges {
		for _, td := range ad.TrackChanges {
			if len(td.ClipMoveGroups) > 0 || len(td.ClipBulkGroups) > 0 || len(td.ClipModifyGroups) > 0 {
				return true
			}
		}
	}
	return false
}

// loadProject reads and parses a single .flp file.
func loadProject(path string) (*flp.FLPProject, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return flp.ParseFLPFile(buf)
}

// ───────────────────────── info ─────────────────────────

// infoLabelRe matches a leading "Label:" at the start of a line.
// The label must start with an uppercase ASCII letter, and may contain
// letters and spaces (e.g. "Channels", "File", "Title", "Data path").
var infoLabelRe = regexp.MustCompile(`^([A-Z][A-Za-z ]+:)(.*)$`)

func runInfo(argv []string, s streams, p painter) int {
	args := append([]string{}, argv...)
	format := "text"
	for i := len(args) - 1; i >= 0; i-- {
		if args[i] == "--format" || args[i] == "-f" {
			v := ""
			if i+1 < len(args) {
				v = args[i+1]
			}
			if v == "text" || v == "json" || v == "canonical" {
				format = v
				args = append(args[:i], args[min(i+2, len(args)):]...)
			} else {
				fmt.Fprintf(s.stderr, "%s %s\n", p.red("flp info:"),
					"--format expects one of: text, json, canonical")
				return exitError
			}
		}
	}
	if len(args) != 1 {
		fmt.Fprintf(s.stderr, "%s %s\n", p.bold("Usage:"),
			"flp info <file.flp> [--format text|json|canonical]")
		return exitError
	}
	path := args[0]
	project, err := loadProject(path)
	if err != nil {
		return handleError(err, s, p)
	}
	switch format {
	case "json":
		out, jerr := marshalIndentedJSON(flp.ToFlpInfoJson(project))
		if jerr != nil {
			return handleError(jerr, s, p)
		}
		fmt.Fprintln(s.stdout, out)
	case "canonical":
		fmt.Fprint(s.stdout, flp.RenderCanonical(project))
	default:
		body := flp.RenderInfo(project, path)
		fmt.Fprintln(s.stdout, decorateInfoBody(body, p))
	}
	return exitIdentical
}

// decorateInfoBody colorizes the "Label: value" prefixes produced by
// flp.RenderInfo. The version banner line (which contains no leading
// label) is left untouched.
func decorateInfoBody(body string, p painter) string {
	if !p.enabled {
		return body
	}
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if m := infoLabelRe.FindStringSubmatch(line); m != nil {
			lines[i] = p.cyan(m[1]) + m[2]
		}
	}
	return strings.Join(lines, "\n")
}

// ───────────────────────── git subcommands ─────────────────────────

func runGitSetup(argv []string, s streams, p painter) int {
	scope, mode, lfs := flp.ScopeLocal, flp.ModeCommand, false
	rest := []string{}
	for _, a := range argv {
		switch a {
		case "--global":
			scope = flp.ScopeGlobal
		case "--textconv":
			mode = flp.ModeTextconv
		case "--lfs":
			lfs = true
		default:
			rest = append(rest, a)
		}
	}
	if len(rest) > 0 {
		fmt.Fprintf(s.stderr, "%s %s\n", p.red("flp git-setup:"),
			"unexpected argument: "+rest[len(rest)-1])
		fmt.Fprintln(s.stderr, p.dim("Run `flp --help` to see supported options."))
		return exitError
	}
	result, err := flp.SetupGit(flp.SetupOptions{Scope: scope, Mode: mode, Lfs: lfs})
	if err != nil {
		return handleError(err, s, p)
	}
	recap := flp.RenderSetupRecap(result)
	if result.Verified {
		fmt.Fprintln(s.stdout, recap)
		return exitIdentical
	}
	fmt.Fprintln(s.stderr, recap)
	return exitError
}

func runGitVerify(argv []string, s streams, p painter) int {
	if len(argv) > 0 {
		fmt.Fprintf(s.stderr, "%s %s\n", p.red("flp git-verify:"),
			"unexpected argument: "+argv[0])
		fmt.Fprintln(s.stderr, p.dim("Run `flp --help` to see supported options."))
		return exitError
	}
	result := flp.VerifyGit("")
	report := flp.RenderVerifyReport(result)
	if result.Status == flp.VerifyError {
		fmt.Fprintln(s.stderr, report)
		return exitError
	}
	fmt.Fprintln(s.stdout, report)
	return exitIdentical
}

func runGitDriver(argv []string, s streams) int {
	// git-driver output must be plain text: git pipes it through its own pager
	// which handles colouring. We always use the raw file handles here.
	if s.stdoutFile == nil || s.stderrFile == nil {
		return gitDriverInline(argv, s.stdout, s.stderr)
	}
	return flp.GitDriverMain(argv, s.stdoutFile, s.stderrFile)
}

// gitDriverInline mirrors flp.GitDriverMain for non-file streams (used in tests).
func gitDriverInline(argv []string, stdout, stderr io.Writer) int {
	if len(argv) != 7 && len(argv) != 9 {
		fmt.Fprintf(stderr, "flp git-driver: expected 7 or 9 args (git external-diff convention), got %d\n", len(argv))
		return exitError
	}
	path := argv[0]
	oldB, err := os.ReadFile(argv[1])
	var newB []byte
	if err == nil {
		newB, err = os.ReadFile(argv[4])
	}
	var a, b *flp.FLPProject
	if err == nil {
		a, err = flp.ParseFLPFile(oldB)
	}
	if err == nil {
		b, err = flp.ParseFLPFile(newB)
	}
	if err != nil {
		fmt.Fprintf(stderr, "flp git-driver: failed to parse (%s): %s\n", path, err.Error())
		return exitError
	}
	fmt.Fprintln(stdout, flp.RenderSummary(flp.CompareProjects(a, b), flp.RenderSummaryOptions{}))
	return exitIdentical
}

// ───────────────────────── diagnostics ─────────────────────────

// handleError prints a diagnostic to stderr and returns exitError.
// It distinguishes the common failure modes and suggests a next step.
func handleError(e error, s streams, p painter) int {
	var pathErr *os.PathError
	if errors.As(e, &pathErr) {
		switch {
		case os.IsNotExist(e):
			fmt.Fprintln(s.stderr, p.red("flp: file not found"))
			fmt.Fprintf(s.stderr, "  %s\n", p.dim(pathErr.Path))
			fmt.Fprintln(s.stderr, "Check the path and try again.")
			return exitError
		case os.IsPermission(e):
			fmt.Fprintln(s.stderr, p.red("flp: permission denied"))
			fmt.Fprintf(s.stderr, "  %s\n", p.dim(pathErr.Path))
			return exitError
		}
	}

	if pe, ok := e.(*flp.FLPParseError); ok {
		fmt.Fprintln(s.stderr, p.red("flp: parse error"))
		fmt.Fprintf(s.stderr, "  %s\n", pe.Error())
		fmt.Fprintln(s.stderr, "")
		fmt.Fprintln(s.stderr, "This FLP variant is not yet supported.")
		fmt.Fprintln(s.stderr, "Make sure the file is a complete, unmodified .flp exported by FL Studio.")
		return exitError
	}

	fmt.Fprintf(s.stderr, "%s %s\n", p.red("flp:"), e.Error())
	return exitError
}

// ───────────────────────── JSON ─────────────────────────

// marshalIndentedJSON renders v as JSON with 2-space indentation. Map keys
// are emitted in sorted order by encoding/json, so output is deterministic
// across runs.
func marshalIndentedJSON(v interface{}) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}