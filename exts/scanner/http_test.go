package scanner_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	coretool "github.com/chainreactors/cyber/core/tool"
	scannerext "github.com/chainreactors/cyber/exts/scanner"
	"github.com/chainreactors/cyber/pkg/testutil/hosttest"
)

func TestHTTPInstallationNeedsNoEnginesOrModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "local-http-fixture")
	}))
	defer server.Close()
	commands := coretool.NewCommandRegistry()
	set := hosttest.Load(t, t.Context(), hosttest.Capabilities(), commands, scannerext.NewHTTP())
	if !slices.Equal(commands.Names(), []string{"curl"}) {
		t.Fatalf("HTTP installation changed the tool surface: %v", commands.Names())
	}
	var output bytes.Buffer
	if _, err := commands.Execute(t.Context(), "curl", &coretool.Execution{Args: []string{server.URL}, Stdout: &output}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output.Bytes(), []byte("local-http-fixture")) {
		t.Fatalf("HTTP command did not read the fixture: %s", output.String())
	}
	if err := set.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if commands.Has("curl") {
		t.Fatal("HTTP command survived installation close")
	}
}
