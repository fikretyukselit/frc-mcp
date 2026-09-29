// Package agenteval is the agent-level eval (M4 exit criterion): the same
// coding agent writes robot code for a task with and without frc-mcp, and
// the result is compiled against the exact Maven artifacts GradleRIO puts on
// the Java compile classpath for that season plus the task's vendordeps.
// The score is the compile-pass rate difference.
//
// Compilation is javac over the GradleRIO classpath rather than a full
// `gradlew build`: it checks the same thing (does the code compile against
// this season's jars), needs no Gradle or toolchain download, and is
// deterministic. It does not run tests, simulation or deploy.
package agenteval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Artifact is one Maven jar.
type Artifact struct {
	Repo    string `json:"repo"`
	Group   string `json:"group"`
	Name    string `json:"name"`
	Version string `json:"version"`
	// Fallbacks are further repositories tried in order when Repo does not
	// serve the jar (a vendordep's later mavenUrls: ChoreoLib's gson is on
	// Maven Central, not on its own repository).
	Fallbacks []string `json:"fallbacks,omitempty"`
}

// URL is the jar's download URL in Repo.
func (a Artifact) URL() string { return a.urlIn(a.Repo) }

func (a Artifact) urlIn(repo string) string {
	return strings.TrimSuffix(repo, "/") + "/" + strings.ReplaceAll(a.Group, ".", "/") + "/" + a.Name + "/" + a.Version +
		"/" + a.Name + "-" + a.Version + ".jar"
}

func (a Artifact) String() string { return a.Group + ":" + a.Name + ":" + a.Version }

// Season describes one season's toolchain: GradleRIO version and plugin id,
// javac --release level, and the Java artifacts GradleRIO adds
// (WPIJavaDepsExtension at that GradleRIO tag, plus the WPILib-bundled
// command-based vendordep and the pinned third-party jars with their
// transitive dependencies spelled out).
type Season struct {
	Season    string
	GradleRIO string
	PluginID  string
	Release   int
	Artifacts []Artifact
	// Processors is GradleRIO's wpilibAnnotations() (Epilogue's annotation
	// processor; in 2027 also WPILib's javac plugin, which turns e.g. an
	// ignored @NoDiscard result into a compile error).
	Processors []Artifact
	// Commands is the WPILibNewCommands vendordep file the WPILib template
	// ships (not part of the vendor catalog).
	Commands []byte
}

const (
	frcMaven = "https://frcmaven.wpi.edu/artifactory/release/"
	central  = "https://repo1.maven.org/maven2/"
)

// wpi lists WPILib artifacts: "<name>-java" lives in group "<root>.<name>"
// ("epilogue-runtime-java" and "epilogue-processor-java" in "<root>.epilogue").
func wpi(group string, version string, names ...string) []Artifact {
	out := make([]Artifact, len(names))
	for i, n := range names {
		base := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(n, "-java"), "-runtime"), "-processor")
		out[i] = Artifact{Repo: frcMaven, Group: group + "." + base, Name: n, Version: version}
	}
	return out
}

func ejml(v string) []Artifact {
	out := make([]Artifact, 0, 8)
	for _, n := range []string{"ejml-simple", "ejml-core", "ejml-fdense", "ejml-ddense", "ejml-cdense", "ejml-zdense", "ejml-dsparse", "ejml-fsparse"} {
		out = append(out, Artifact{Repo: central, Group: "org.ejml", Name: n, Version: v})
	}
	return out
}

// Seasons returns the toolchains the eval supports.
func Seasons() map[string]Season {
	const v26, v27 = "2026.2.1", "2027.0.0-alpha-7"
	s26 := Season{Season: "2026", GradleRIO: v26, PluginID: "edu.wpi.first.GradleRIO", Release: 17}
	s26.Artifacts = wpi("edu.wpi.first", v26, "wpilibj-java", "wpimath-java", "ntcore-java", "cscore-java", "cameraserver-java",
		"hal-java", "wpinet-java", "wpiutil-java", "apriltag-java", "wpiunits-java", "epilogue-runtime-java")
	s26.Artifacts = append(s26.Artifacts,
		Artifact{Repo: frcMaven, Group: "edu.wpi.first.wpilibNewCommands", Name: "wpilibNewCommands-java", Version: v26},
		Artifact{Repo: frcMaven, Group: "edu.wpi.first.thirdparty.frc2025.opencv", Name: "opencv-java", Version: "4.10.0-3"},
		Artifact{Repo: central, Group: "com.fasterxml.jackson.core", Name: "jackson-annotations", Version: "2.19.2"},
		Artifact{Repo: central, Group: "com.fasterxml.jackson.core", Name: "jackson-core", Version: "2.19.2"},
		Artifact{Repo: central, Group: "com.fasterxml.jackson.core", Name: "jackson-databind", Version: "2.19.2"},
		Artifact{Repo: central, Group: "us.hebi.quickbuf", Name: "quickbuf-runtime", Version: "1.4"})
	s26.Artifacts = append(s26.Artifacts, ejml("0.44.0")...)
	s26.Processors = wpi("edu.wpi.first", v26, "epilogue-processor-java", "epilogue-runtime-java")
	s26.Commands = commandsJSON("WPILibNewCommands", v26, "2026", "edu.wpi.first.wpilibNewCommands", "wpilibNewCommands-java",
		"111e20f7-815e-48f8-9dd6-e675ce75b266")

	s27 := Season{Season: "2027", GradleRIO: v27, PluginID: "org.wpilib.GradleRIO", Release: 25}
	s27.Artifacts = wpi("org.wpilib", v27, "wpilibj-java", "wpimath-java", "ntcore-java", "cscore-java", "cameraserver-java",
		"hal-java", "wpinet-java", "wpiutil-java", "apriltag-java", "wpiunits-java", "epilogue-runtime-java", "datalog-java",
		"drivers-java", "fields-java", "telemetry-java", "tunables-java")
	s27.Artifacts = append(s27.Artifacts,
		Artifact{Repo: frcMaven, Group: "org.wpilib", Name: "annotations-java", Version: v27},
		Artifact{Repo: frcMaven, Group: "org.wpilib.commandsv2", Name: "commandsv2-java", Version: v27},
		Artifact{Repo: frcMaven, Group: "org.wpilib.thirdparty.opencv", Name: "opencv-java", Version: "2027-4.13.0-3"},
		Artifact{Repo: central, Group: "io.avaje", Name: "avaje-jsonb", Version: "3.14"},
		Artifact{Repo: central, Group: "io.avaje", Name: "avaje-json-core", Version: "3.14"},
		Artifact{Repo: central, Group: "us.hebi.quickbuf", Name: "quickbuf-runtime", Version: "1.4"})
	s27.Artifacts = append(s27.Artifacts, ejml("0.46.0")...)
	s27.Processors = append(wpi("org.wpilib", v27, "epilogue-processor-java", "epilogue-runtime-java"),
		Artifact{Repo: central, Group: "io.avaje", Name: "avaje-jsonb-generator", Version: "3.14"},
		Artifact{Repo: frcMaven, Group: "org.wpilib", Name: "javac-plugin-java", Version: v27},
		Artifact{Repo: frcMaven, Group: "org.wpilib", Name: "annotations-java", Version: v27})
	s27.Commands = commandsJSON("CommandsV2", v27, "2027_alpha7", "org.wpilib.commandsv2", "commandsv2-java",
		"111e20f7-815e-48f8-9dd6-e675ce75b266")
	return map[string]Season{"2026": s26, "2027": s27}
}

func commandsJSON(name, version, year, group, artifact, uuid string) []byte {
	b, _ := json.MarshalIndent(map[string]any{
		"fileName": name + ".json", "name": name, "version": "1.0.0", "uuid": uuid, "frcYear": year,
		"mavenUrls": []string{}, "jsonUrl": "", "javaDependencies": []map[string]string{{"groupId": group, "artifactId": artifact, "version": version}},
		"jniDependencies": []any{}, "cppDependencies": []any{},
	}, "", "  ")
	return b
}

// VendordepArtifacts reads a vendordep JSON's Java dependencies; each is
// fetched from the first of its mavenUrls that serves it (then frcmaven and
// Maven Central, which GradleRIO always adds).
func VendordepArtifacts(raw []byte) ([]Artifact, error) {
	var v struct {
		MavenURLs []string `json:"mavenUrls"`
		Java      []struct {
			GroupID, ArtifactID, Version string
		} `json:"javaDependencies"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("agenteval: vendordep: %w", err)
	}
	repos := append(append([]string{}, v.MavenURLs...), frcMaven, central)
	var out []Artifact
	for _, d := range v.Java {
		out = append(out, Artifact{Repo: repos[0], Fallbacks: repos[1:], Group: d.GroupID, Name: d.ArtifactID, Version: d.Version})
	}
	return out, nil
}

// Fetcher downloads jars into a content cache (group/name/version paths).
// It is safe for concurrent use.
type Fetcher struct {
	Dir    string
	Client *http.Client

	mu    sync.Mutex
	hosts []string // allowed besides the built-in repositories
}

// AllowHost adds a download (and redirect) host, e.g. a vendor Maven repo.
func (f *Fetcher) AllowHost(h string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !slices.Contains(f.hosts, h) {
		f.hosts = append(f.hosts, h)
	}
}

// ErrHost is returned for a download outside the allowed hosts.
var ErrHost = errors.New("agenteval: host not allowed")

func (f *Fetcher) allowed(u string) bool {
	p, err := url.Parse(u)
	if err != nil || p.Scheme != "https" {
		return false
	}
	f.mu.Lock()
	extra := slices.Clone(f.hosts)
	f.mu.Unlock()
	for _, h := range append([]string{"frcmaven.wpi.edu", "repo1.maven.org", "storage.googleapis.com", "github.com",
		"release-assets.githubusercontent.com", "objects.githubusercontent.com"}, extra...) {
		if p.Hostname() == h {
			return true
		}
	}
	return false
}

// Jar returns the local path of an artifact, downloading it once.
func (f *Fetcher) Jar(ctx context.Context, a Artifact) (string, error) {
	dst := filepath.Join(f.Dir, filepath.FromSlash(strings.ReplaceAll(a.Group, ".", "/")), a.Name, a.Version, a.Name+"-"+a.Version+".jar")
	if st, err := os.Stat(dst); err == nil && st.Size() > 0 {
		return dst, nil
	}
	var resp *http.Response
	var lastErr error
	for _, repo := range append([]string{a.Repo}, a.Fallbacks...) {
		r, err := f.get(ctx, a.urlIn(repo))
		if err == nil {
			resp = r
			break
		}
		lastErr = err
	}
	if resp == nil {
		return "", fmt.Errorf("agenteval: fetch %s: %w", a, lastErr)
	}
	defer resp.Body.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	// A unique temp file per download: parallel runs fetch the same jars on
	// a cold cache, and the first finished copy wins.
	out, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".*.tmp")
	if err != nil {
		return "", err
	}
	tmp := out.Name()
	defer func() { _ = os.Remove(tmp) }()
	if _, err := io.Copy(out, io.LimitReader(resp.Body, 512<<20)); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		if st, serr := os.Stat(dst); serr == nil && st.Size() > 0 {
			return dst, nil // another run published it first (Windows refuses to replace)
		}
		return "", err
	}
	return dst, nil
}

// get fetches one URL (allowlisted, redirects re-checked); a non-200
// response is an error.
func (f *Fetcher) get(ctx context.Context, u string) (*http.Response, error) {
	if !f.allowed(u) {
		return nil, fmt.Errorf("%w: %s", ErrHost, u)
	}
	c := &http.Client{Timeout: 5 * time.Minute}
	if f.Client != nil {
		cc := *f.Client // never mutate a shared client from concurrent downloads
		c = &cc
	}
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 || !f.allowed(req.URL.String()) {
			return fmt.Errorf("%w: redirect to %s", ErrHost, req.URL)
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "frc-mcp-agenteval (+https://github.com/fikretyukselit/frc-mcp)")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}
	return resp, nil
}

// Classpath fetches every artifact and returns the jar paths.
func (f *Fetcher) Classpath(ctx context.Context, arts []Artifact) ([]string, error) {
	var out []string
	for _, a := range arts {
		p, err := f.Jar(ctx, a)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
