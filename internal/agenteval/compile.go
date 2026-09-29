package agenteval

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CompileResult is the outcome of compiling a task's sources.
type CompileResult struct {
	Pass    bool     `json:"pass"`
	Errors  int      `json:"errors"`
	Files   int      `json:"files"`
	Missing []string `json:"missing,omitempty"`
	// Output holds the first compiler diagnostics (truncated).
	Output string `json:"output,omitempty"`
	Millis int64  `json:"ms"`
}

var errLineRe = regexp.MustCompile(`(?m)^\S.*\.java:\d+: error: `)

// Compile runs javac --release over every .java file under root/src/main/java
// with the season classpath and annotation processors, like GradleRIO's
// compileJava. required lists files the task must produce.
func Compile(ctx context.Context, javac string, release int, classpath, processors []string, root string, required []string) CompileResult {
	t0 := time.Now()
	res := CompileResult{}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs // javac runs in root; source paths must not be relative to the caller
	}
	for _, f := range required {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(f))); err != nil {
			res.Missing = append(res.Missing, f)
		}
	}
	var srcs []string
	_ = filepath.WalkDir(filepath.Join(root, "src", "main", "java"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".java") {
			srcs = append(srcs, p)
		}
		return nil
	})
	res.Files = len(srcs)
	if len(srcs) == 0 {
		res.Output = "no Java sources"
		res.Millis = time.Since(t0).Milliseconds()
		return res
	}
	out, err := os.MkdirTemp("", "agenteval-classes-")
	if err != nil {
		res.Output = err.Error()
		return res
	}
	defer func() { _ = os.RemoveAll(out) }()
	sep := string(os.PathListSeparator)
	args := []string{"--release", strconv.Itoa(release), "-encoding", "UTF-8", "-nowarn", "-Xmaxerrs", "500",
		"-XDstringConcat=inline", "-d", out, "-cp", strings.Join(classpath, sep)}
	if len(processors) > 0 {
		args = append(args, "-processorpath", strings.Join(append(append([]string{}, processors...), classpath...), sep))
	} else {
		args = append(args, "-proc:none")
	}
	args = append(args, srcs...)
	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, javac, args...) //nolint:gosec // G204: javac path is the operator's; args are file paths
	cmd.Dir = root
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err = cmd.Run()
	text := strings.ReplaceAll(buf.String(), root+string(os.PathSeparator), "")
	res.Errors = len(errLineRe.FindAllString(text, -1))
	res.Pass = err == nil && len(res.Missing) == 0
	if err != nil && res.Errors == 0 {
		res.Errors = 1 // javac failed without a parsable diagnostic (crash, bad flag)
	}
	if len(text) > 6000 {
		text = text[:6000] + "\n…"
	}
	res.Output = text
	res.Millis = time.Since(t0).Milliseconds()
	return res
}
