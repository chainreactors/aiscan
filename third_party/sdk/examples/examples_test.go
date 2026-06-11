package examples_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestExampleMainPackagesBuild(t *testing.T) {
	root := repoRoot(t)
	out, err := runGo(t, root, time.Minute, "list", "-f", "{{if eq .Name \"main\"}}{{.ImportPath}}{{end}}", "./examples/...")
	if err != nil {
		t.Fatalf("go list examples failed: %v\n%s", err, out)
	}

	for _, importPath := range strings.Fields(out) {
		name := strings.TrimPrefix(importPath, "github.com/chainreactors/sdk/examples/")
		t.Run(name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), strings.ReplaceAll(name, "/", "_"))
			if runtime.GOOS == "windows" {
				output += ".exe"
			}
			args := []string{"build", "-o", output, importPath}
			if out, err := runGo(t, root, 2*time.Minute, args...); err != nil {
				t.Fatalf("go %s failed: %v\n%s", strings.Join(args, " "), err, out)
			}
		})
	}
}

func TestExampleSpecialBuilds(t *testing.T) {
	root := repoRoot(t)
	commands := []struct {
		name string
		args []string
	}{
		{"host_spray_sdk", []string{"./examples/spray/host_spray_sdk.go"}},
	}

	for _, tc := range commands {
		t.Run(tc.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), tc.name)
			if runtime.GOOS == "windows" {
				output += ".exe"
			}
			args := append([]string{"build", "-o", output}, tc.args...)
			if out, err := runGo(t, root, 2*time.Minute, args...); err != nil {
				t.Fatalf("go %s failed: %v\n%s", strings.Join(args, " "), err, out)
			}
		})
	}
}

func TestExampleCommandUsageSmoke(t *testing.T) {
	root := repoRoot(t)
	commands := []struct {
		name    string
		args    []string
		want    string
		wantErr bool
	}{
		{"fingers", []string{"run", "./examples/fingers/main.go"}, "Usage: fingers", true},
		{"neutron", []string{"run", "./examples/neutron/main.go"}, "Usage: neutron", true},
		{"gogo", []string{"run", "./examples/gogo/main.go"}, "Usage: gogo", true},
		{"gogo_cyberhub", []string{"run", "./examples/gogo_cyberhub/main.go"}, "Usage: gogo_cyberhub", true},
		{"spray", []string{"run", "./examples/spray/main.go"}, "Usage: spray", true},
		{"host_spray_sdk", []string{"run", "./examples/spray/host_spray_sdk.go"}, "Usage: host_spray_sdk", true},
		{"cyberhub", []string{"run", "./examples/cyberhub/main.go"}, "Usage: go run ./examples/cyberhub", false},
		{"match_detail_case", []string{"run", "./examples/cases/match_detail"}, "-target", true},
		{"request_response_case", []string{"run", "./examples/cases/request_response"}, "-target", true},
		{"spray_crawl_finger_case", []string{"run", "./examples/cases/spray_crawl_finger"}, "-target", true},
	}

	for _, tc := range commands {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runGo(t, root, time.Minute, tc.args...)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected command to fail without required args\n%s", out)
				}
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) {
					t.Fatalf("expected exit error, got %T: %v\n%s", err, err, out)
				}
			} else if err != nil {
				t.Fatalf("expected command to succeed: %v\n%s", err, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("expected output to contain %q\n%s", tc.want, out)
			}
		})
	}
}

func TestAssociationExampleInlineSmoke(t *testing.T) {
	root := repoRoot(t)
	out, err := runGo(t, root, time.Minute, "run", "./examples/association/main.go")
	if err != nil {
		t.Fatalf("association example failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"finger -> alias -> template",
		"template -> alias -> finger",
		"CVE-2022-0001",
		"tomcat",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q\n%s", want, out)
		}
	}
}

func TestAssociationExampleQuerySmoke(t *testing.T) {
	root := repoRoot(t)
	out, err := runGo(t, root, time.Minute, "run", "./examples/association/main.go", "-finger", "apache tomcat")
	if err != nil {
		t.Fatalf("association query example failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"inline lookup",
		"tomcat",
		"CVE-2022-0001",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q\n%s", want, out)
		}
	}
}

func TestFilterExampleSmoke(t *testing.T) {
	root := repoRoot(t)
	out, err := runGo(t, root, time.Minute, "run", "./examples/filter/main.go")
	if err != nil {
		t.Fatalf("filter example failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"Fingers ExportFilter:",
		"FullFingers.Filter: OK",
		"Templates.Filter (severity): OK",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q\n%s", want, out)
		}
	}
}

func TestSprayExampleChecksLocalHTTP(t *testing.T) {
	root := repoRoot(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<html><head><title>SDK CI</title></head><body>ok</body></html>`)
	}))
	defer srv.Close()

	out, err := runGo(
		t,
		root,
		2*time.Minute,
		"run",
		"./examples/spray/main.go",
		"-u",
		srv.URL,
		"-json",
		"-threads",
		"1",
		"-timeout",
		"3",
		"-mc",
		"200",
	)
	if err != nil {
		t.Fatalf("spray example failed: %v\n%s", err, out)
	}

	var payload struct {
		TotalURLs    int `json:"total_urls"`
		Processed    int `json:"processed"`
		MatchedCount int `json:"matched_count"`
		Results      []struct {
			URL    string `json:"url"`
			Status int    `json:"status"`
			Title  string `json:"title"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(extractJSONObject(t, out)), &payload); err != nil {
		t.Fatalf("failed to parse json output: %v\n%s", err, out)
	}
	if payload.TotalURLs != 1 || payload.Processed != 1 || payload.MatchedCount != 1 {
		t.Fatalf("unexpected counters: %+v\n%s", payload, out)
	}
	if len(payload.Results) != 1 {
		t.Fatalf("expected one result: %+v\n%s", payload, out)
	}
	if payload.Results[0].URL != srv.URL || payload.Results[0].Status != http.StatusOK {
		t.Fatalf("unexpected spray result: %+v\n%s", payload.Results[0], out)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find repo root from %s", dir)
		}
		dir = parent
	}
}

func runGo(t *testing.T, root string, timeout time.Duration, args ...string) (string, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = root
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("go %s timed out\n%s", strings.Join(args, " "), out.String())
	}
	return out.String(), err
}

func extractJSONObject(t *testing.T, output string) string {
	t.Helper()

	start := strings.Index(output, "{")
	end := strings.LastIndex(output, "}")
	if start < 0 || end < start {
		t.Fatalf("output does not contain a json object\n%s", output)
	}
	return output[start : end+1]
}
