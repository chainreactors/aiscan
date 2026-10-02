// A small executable used by the bootstrap shell tests. Its archive mode
// produces a realistic release ZIP without downloading public releases.
package main

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"strconv"
)

func main() {
	if len(os.Args) == 4 && os.Args[1] == "--archive" {
		if err := archive(os.Args[2], os.Args[3]); err != nil {
			panic(err)
		}
		return
	}
	binary, _ := os.Executable()
	cwd, _ := os.Getwd()
	data, _ := json.Marshal(map[string]any{"args": os.Args[1:], "binary": binary, "cwd": cwd})
	if err := os.WriteFile(os.Getenv("BOOTSTRAP_CAPTURE"), data, 0600); err != nil {
		panic(err)
	}
	code, _ := strconv.Atoi(os.Getenv("BOOTSTRAP_EXIT"))
	os.Exit(code)
}

func archive(path, name string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := zip.NewWriter(f)
	binary, err := w.Create(name)
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	source, err := os.Open(executable)
	if err != nil {
		return err
	}
	defer source.Close()
	if _, err := io.Copy(binary, source); err != nil {
		return err
	}
	readme, err := w.Create("README.md")
	if err != nil {
		return err
	}
	if _, err := io.WriteString(readme, "Bootstrap fixture"); err != nil {
		return err
	}
	return w.Close()
}
