package flpdiff

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// DriverName is the diff=<name> token used in .gitattributes and [diff "<name>"].
const DriverName = "flp"

var (
	GitAttributeLine    = "*.flp diff=" + DriverName
	GitLfsAttributeLine = "*.flp filter=lfs diff=" + DriverName + " merge=lfs -text"
)

var lineSplitRe = regexp.MustCompile(`\r?\n`)
var flpRuleRe = regexp.MustCompile(`^\*\.flp\s+.*\bdiff=flp\b`)

func splitLines(s string) []string { return lineSplitRe.Split(s, -1) }

// GitDriverMain is the entry for `flp git-driver <args>` (git external-diff convention).
// stdout/stderr receive output; returns an exit code.
func GitDriverMain(argv []string, stdout, stderr *os.File) int {
	if len(argv) != 7 && len(argv) != 9 {
		fmt.Fprintf(stderr, "flp git-driver: expected 7 or 9 args (git external-diff convention), got %d: %s\n", len(argv), jsonStringArray(argv))
		return 2
	}
	path := argv[0]
	oldFile := argv[1]
	newFile := argv[4]

	oldBuf, err := os.ReadFile(oldFile)
	var newBuf []byte
	if err == nil {
		newBuf, err = os.ReadFile(newFile)
	}
	var oldP, newP *FLPProject
	if err == nil {
		oldP, err = ParseFLPFile(oldBuf)
	}
	if err == nil {
		newP, err = ParseFLPFile(newBuf)
	}
	if err != nil {
		fmt.Fprintf(stderr, "flp git-driver: failed to parse (%s): %s\n", path, err.Error())
		return 2
	}
	result := CompareProjects(oldP, newP)
	fmt.Fprintln(stdout, RenderSummary(result, RenderSummaryOptions{}))
	return 0
}

func jsonStringArray(a []string) string {
	parts := make([]string, len(a))
	for i, s := range a {
		parts[i] = jsonString(s)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// Scope is "local" | "global".
type Scope string

// DriverMode is "command" | "textconv".
type DriverMode string

const (
	ScopeLocal      Scope      = "local"
	ScopeGlobal     Scope      = "global"
	ModeCommand     DriverMode = "command"
	ModeTextconv    DriverMode = "textconv"
)

// SetupOptions configures SetupGit. Zero values default to local/command/no-lfs.
type SetupOptions struct {
	Scope                Scope
	Mode                 DriverMode
	Lfs                  bool
	RepoRoot             string
	GlobalAttributesPath string
	Runner               func(cmd []string) int
	ExecutablePath       string
}

// SetupResult describes what SetupGit did.
type SetupResult struct {
	Scope                Scope
	Mode                 DriverMode
	Lfs                  bool
	GitattributesPath    string
	HasGitattributesPath bool
	GitattributesTouched bool
	ConfigCommands       [][]string
	Notes                []string
	ExecutablePath       string
	Verified             bool
}

// SetupGit configures git for FLP semantic diff.
func SetupGit(opts SetupOptions) (SetupResult, error) {
	scope := opts.Scope
	if scope == "" {
		scope = ScopeLocal
	}
	mode := opts.Mode
	if mode == "" {
		mode = ModeCommand
	}
	repoRoot := opts.RepoRoot
	if repoRoot == "" {
		wd, err := os.Getwd()
		if err != nil {
			return SetupResult{}, err
		}
		repoRoot = wd
	}
	runner := opts.Runner
	if runner == nil {
		runner = defaultRunner
	}

	notes := []string{}
	exe := opts.ExecutablePath
	if exe == "" {
		exe = resolveflpExecutable(func(w string) { notes = append(notes, w) })
	}

	scopeFlag := "--local"
	if scope == ScopeGlobal {
		scopeFlag = "--global"
	}
	base := []string{"git", "config", scopeFlag}
	cc := func(extra ...string) []string {
		c := append([]string{}, base...)
		return append(c, extra...)
	}
	configCommands := [][]string{}
	if mode == ModeCommand {
		configCommands = append(configCommands, cc("diff."+DriverName+".command", exe+" git-driver"))
		configCommands = append(configCommands, cc("--unset", "diff."+DriverName+".textconv"))
	} else {
		configCommands = append(configCommands, cc("diff."+DriverName+".textconv", exe+" info --format=canonical"))
		configCommands = append(configCommands, cc("diff."+DriverName+".cachetextconv", "true"))
		configCommands = append(configCommands, cc("--unset", "diff."+DriverName+".command"))
	}

	gitattributesPath := ""
	touched := false
	if scope == ScopeLocal {
		gitattributesPath = filepath.Join(repoRoot, ".gitattributes")
		t, err := EnsureGitattributes(gitattributesPath, opts.Lfs)
		if err != nil {
			return SetupResult{}, err
		}
		touched = t
	} else {
		gitattributesPath = resolveGlobalAttributesPath(opts.GlobalAttributesPath)
		if err := os.MkdirAll(filepath.Dir(gitattributesPath), 0o755); err != nil {
			return SetupResult{}, err
		}
		t, err := EnsureGitattributes(gitattributesPath, false)
		if err != nil {
			return SetupResult{}, err
		}
		touched = t
		configCommands = append(configCommands, cc("core.attributesfile", gitattributesPath))
		if opts.Lfs {
			notes = append(notes, "LFS tracking is per-repo and cannot be configured in --global scope; rerun `flp git-setup --lfs` inside the repo.")
		}
	}

	for _, cmd := range configCommands {
		rc := runner(cmd)
		if rc != 0 && cmd[3] != "--unset" {
			notes = append(notes, fmt.Sprintf("git config failed: %s (exit %d)", strings.Join(cmd, " "), rc))
		}
	}

	if opts.Lfs && scope == ScopeLocal {
		if runner([]string{"git", "lfs", "install", "--local"}) == 0 {
			runner([]string{"git", "lfs", "track", "*.flp"})
		} else {
			notes = append(notes, "git-lfs not available — install https://git-lfs.com and rerun `flp git-setup --lfs`.")
		}
	}

	keyName := "textconv"
	if mode == ModeCommand {
		keyName = "command"
	}
	verificationKey := "diff." + DriverName + "." + keyName
	verified := verifyConfigKey(verificationKey, scopeFlag, repoRoot)
	if !verified {
		notes = append(notes, fmt.Sprintf("git-setup verification failed: `git config %s --get %s` returned nothing. "+
			"Likely causes: (a) the target directory (%s) isn't inside a git repo, "+
			"(b) git is not installed or not on PATH, or "+
			"(c) one of the git config commands above returned non-zero. "+
			"Run `flp git-verify` to diagnose.", scopeFlag, verificationKey, repoRoot))
	}

	return SetupResult{
		Scope: scope, Mode: mode, Lfs: opts.Lfs, GitattributesPath: gitattributesPath,
		HasGitattributesPath: gitattributesPath != "", GitattributesTouched: touched,
		ConfigCommands: configCommands, Notes: notes, ExecutablePath: exe, Verified: verified,
	}, nil
}

func gitOutput(dir string, args ...string) (string, int) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return out.String(), ee.ExitCode()
		}
		return "", 127
	}
	return out.String(), 0
}

func verifyConfigKey(key, scope, cwd string) bool {
	out, rc := gitOutput(cwd, "config", scope, "--get", key)
	if rc != 0 {
		return false
	}
	return len(strings.TrimSpace(out)) > 0
}

func isFlpRule(line string) bool {
	s := strings.TrimSpace(line)
	return strings.HasPrefix(s, "*.flp ") || strings.HasPrefix(s, "*.flp\t")
}

// EnsureGitattributes appends the FLP rule if absent. Returns true if the file was created/modified.
func EnsureGitattributes(path string, lfs bool) (bool, error) {
	target := GitAttributeLine
	if lfs {
		target = GitLfsAttributeLine
	}
	existing := ""
	if b, err := os.ReadFile(path); err == nil {
		existing = string(b)
	}
	existingLines := splitLines(existing)
	for _, ln := range existingLines {
		if strings.TrimSpace(ln) == target {
			return false, nil
		}
	}
	filtered := []string{}
	for _, ln := range existingLines {
		if !isFlpRule(ln) {
			filtered = append(filtered, ln)
		}
	}
	for len(filtered) > 0 && strings.TrimSpace(filtered[len(filtered)-1]) == "" {
		filtered = filtered[:len(filtered)-1]
	}
	if len(filtered) > 0 {
		filtered = append(filtered, "")
	}
	filtered = append(filtered, target)
	if err := os.WriteFile(path, []byte(strings.Join(filtered, "\n")+"\n"), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func resolveflpExecutable(warn func(string)) string {
	if p, err := exec.LookPath("flp"); err == nil && p != "" {
		if abs, err2 := filepath.Abs(p); err2 == nil {
			return abs
		}
		return p
	}
	if self, err := os.Executable(); err == nil && strings.HasSuffix(self, "/flp") {
		if _, err := os.Stat(self); err == nil {
			warn(fmt.Sprintf("`flp` is not on PATH. Git config will point at the running binary (%s). "+
				"If you move or delete that file, re-run `flp git-setup`. "+
				"Recommended: symlink it onto your PATH, e.g. `ln -s \"%s\" ~/.local/bin/flp`.", self, self))
			return self
		}
	}
	warn("`flp` is not on PATH and no usable absolute path could be resolved. " +
		"Git config will be written with the bare name `flp`, which git will " +
		"probably fail to execute at diff time — leading to empty `git diff` output " +
		"with exit 0. Install flp on PATH (e.g. `ln -s /path/to/flp ~/.local/bin/`) " +
		"and re-run `flp git-setup`.")
	return "flp"
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(homeDir(), p[2:])
	}
	if p == "~" {
		return homeDir()
	}
	return p
}

func resolveGlobalAttributesPath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if out, rc := gitOutput("", "config", "--global", "--get", "core.attributesfile"); rc == 0 && strings.TrimSpace(out) != "" {
		return expandHome(strings.TrimSpace(out))
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(homeDir(), ".config")
	}
	return filepath.Join(base, "git", "attributes")
}

func defaultRunner(cmd []string) int {
	c := exec.Command(cmd[0], cmd[1:]...)
	suppress := len(cmd) > 3 && cmd[3] == "--unset"
	if !suppress {
		c.Stderr = os.Stderr
	}
	if err := c.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		return 127
	}
	return 0
}

// ───────────── git-verify ─────────────

type VerifyStatus string

const (
	VerifyOK    VerifyStatus = "ok"
	VerifyWarn  VerifyStatus = "warn"
	VerifyError VerifyStatus = "error"
)

type VerifyCheck struct {
	Status VerifyStatus
	Label  string
	Detail string
}

type VerifyResult struct {
	Status VerifyStatus
	Checks []VerifyCheck
}

func readGitConfig(key, cwd string) string {
	for _, scope := range []string{"--local", "--global"} {
		out, rc := gitOutput(cwd, "config", scope, "--get", key)
		if rc == 0 && strings.TrimSpace(out) != "" {
			return strings.TrimSpace(out)
		}
	}
	return ""
}

func fileHasFlpRule(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, ln := range splitLines(string(b)) {
		if flpRuleRe.MatchString(strings.TrimSpace(ln)) {
			return true
		}
	}
	return false
}

// VerifyGit inspects the current repo's flp setup.
func VerifyGit(repoRoot string) VerifyResult {
	if repoRoot == "" {
		repoRoot, _ = os.Getwd()
	}
	checks := []VerifyCheck{}

	top, rc := gitOutput(repoRoot, "rev-parse", "--show-toplevel")
	if rc != 0 || len(strings.TrimSpace(top)) == 0 {
		checks = append(checks, VerifyCheck{
			Status: VerifyError, Label: "current directory is inside a git repo",
			Detail: fmt.Sprintf("`git rev-parse --show-toplevel` exited %d from %s. Run `flp git-verify` from inside a repo.", rc, repoRoot),
		})
		return VerifyResult{Status: VerifyError, Checks: checks}
	}
	repoTop := strings.TrimSpace(top)
	checks = append(checks, VerifyCheck{Status: VerifyOK, Label: "current directory is inside a git repo", Detail: repoTop})

	localAttrs := filepath.Join(repoTop, ".gitattributes")
	if fileHasFlpRule(localAttrs) {
		checks = append(checks, VerifyCheck{Status: VerifyOK, Label: ".gitattributes: *.flp diff=flp rule present", Detail: localAttrs})
	} else {
		globalAttrs := ""
		if out, rc := gitOutput(repoRoot, "config", "--global", "--get", "core.attributesfile"); rc == 0 && strings.TrimSpace(out) != "" {
			globalAttrs = expandHome(strings.TrimSpace(out))
		} else {
			base := os.Getenv("XDG_CONFIG_HOME")
			if base == "" {
				base = filepath.Join(homeDir(), ".config")
			}
			cand := filepath.Join(base, "git", "attributes")
			if _, err := os.Stat(cand); err == nil {
				globalAttrs = cand
			}
		}
		if globalAttrs != "" && fileHasFlpRule(globalAttrs) {
			checks = append(checks, VerifyCheck{Status: VerifyOK, Label: ".gitattributes: *.flp diff=flp rule present (global)", Detail: globalAttrs})
		} else {
			extra := ""
			if globalAttrs != "" {
				extra = " and " + globalAttrs
			}
			checks = append(checks, VerifyCheck{
				Status: VerifyError, Label: ".gitattributes: *.flp diff=flp rule",
				Detail: fmt.Sprintf("Missing. Looked in %s%s. Run `flp git-setup` (or `--global`).", localAttrs, extra),
			})
		}
	}

	cmd := readGitConfig("diff.flp.command", repoRoot)
	textconv := readGitConfig("diff.flp.textconv", repoRoot)
	switch {
	case cmd != "":
		checks = append(checks, VerifyCheck{Status: VerifyOK, Label: "git config: diff.flp.command", Detail: cmd})
	case textconv != "":
		checks = append(checks, VerifyCheck{Status: VerifyOK, Label: "git config: diff.flp.textconv (textconv mode)", Detail: textconv})
	default:
		checks = append(checks, VerifyCheck{Status: VerifyError, Label: "git config: diff.flp.command or diff.flp.textconv",
			Detail: "Neither is set. Run `flp git-setup` or `flp git-setup --textconv`."})
	}

	execStr := cmd
	if execStr == "" {
		execStr = textconv
	}
	if execStr != "" {
		fields := strings.Fields(execStr)
		firstToken := ""
		if len(fields) > 0 {
			firstToken = fields[0]
		}
		resolved := firstToken
		if strings.HasPrefix(firstToken, "/") || strings.HasPrefix(firstToken, "~") {
			resolved = expandHome(firstToken)
		}
		missing := false
		if strings.HasPrefix(resolved, "/") {
			if _, err := os.Stat(resolved); err != nil {
				missing = true
			}
		}
		if missing {
			checks = append(checks, VerifyCheck{Status: VerifyError, Label: "flp executable resolves",
				Detail: fmt.Sprintf("Configured path `%s` does not exist. Re-run `flp git-setup` from a terminal where `flp` is on PATH.", resolved)})
		} else {
			c := exec.Command(firstToken, "--version")
			var out bytes.Buffer
			c.Stdout = &out
			err := c.Run()
			code := 0
			if err != nil {
				code = 127
				if ee, ok := err.(*exec.ExitError); ok {
					code = ee.ExitCode()
				}
			}
			if code == 0 && strings.HasPrefix(strings.TrimSpace(out.String()), "flp ") {
				checks = append(checks, VerifyCheck{Status: VerifyOK, Label: "flp executable runs", Detail: strings.TrimSpace(out.String())})
			} else {
				checks = append(checks, VerifyCheck{Status: VerifyWarn, Label: "flp executable smoke test",
					Detail: fmt.Sprintf("`%s --version` exited %d. The configured path may work when git invokes it, but this check couldn't confirm.", firstToken, code)})
			}
		}
	}

	anyErr, anyWarn := false, false
	for _, c := range checks {
		if c.Status == VerifyError {
			anyErr = true
		}
		if c.Status == VerifyWarn {
			anyWarn = true
		}
	}
	status := VerifyOK
	if anyErr {
		status = VerifyError
	} else if anyWarn {
		status = VerifyWarn
	}
	return VerifyResult{Status: status, Checks: checks}
}

// RenderVerifyReport formats a VerifyResult.
func RenderVerifyReport(r VerifyResult) string {
	marker := map[VerifyStatus]string{VerifyOK: "✓", VerifyWarn: "!", VerifyError: "✗"}
	head := "problems found"
	if r.Status == VerifyOK {
		head = "OK"
	} else if r.Status == VerifyWarn {
		head = "OK with warnings"
	}
	lines := []string{"flp git-verify: " + head}
	for _, c := range r.Checks {
		lines = append(lines, fmt.Sprintf("  [%s] %s", marker[c.Status], c.Label))
		if c.Detail != "" {
			for _, dl := range strings.Split(c.Detail, "\n") {
				lines = append(lines, "        "+dl)
			}
		}
	}
	if r.Status == VerifyError {
		lines = append(lines, "", "Common fixes:",
			"  - `flp git-setup`                   configure this repo",
			"  - `flp git-setup --global`          configure all your repos",
			"  - `flp git-setup --textconv`        use git's native diff on canonical text",
			"  - `flp git-setup --lfs`             also track *.flp with Git LFS")
	}
	return strings.Join(lines, "\n")
}

// RenderSetupRecap formats a SetupResult.
func RenderSetupRecap(r SetupResult) string {
	tag := "FAILED"
	if r.Verified {
		tag = "OK"
	}
	lfs := ""
	if r.Lfs {
		lfs = ", lfs=on"
	}
	lines := []string{fmt.Sprintf("flp git-setup (%s): scope=%s, mode=%s%s", tag, r.Scope, r.Mode, lfs)}
	if r.HasGitattributesPath {
		verb := "already configured"
		if r.GitattributesTouched {
			verb = "updated"
		}
		lines = append(lines, fmt.Sprintf("  attributes: %s (%s)", r.GitattributesPath, verb))
	}
	lines = append(lines, "  executable: "+r.ExecutablePath)
	for _, c := range r.ConfigCommands {
		lines = append(lines, "  $ "+strings.Join(c, " "))
	}
	for _, n := range r.Notes {
		lines = append(lines, "  note: "+n)
	}
	if r.Verified && r.Scope == ScopeLocal && r.Mode == ModeCommand {
		lines = append(lines, "  verify with: flp git-verify", "  try: git diff <changed.flp>")
	} else if !r.Verified {
		lines = append(lines, "  next: run `flp git-verify` to diagnose")
	}
	return strings.Join(lines, "\n")
}
