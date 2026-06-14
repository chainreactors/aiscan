package iutils

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var (
	cwtime = time.Now()
)

func IsLinux() bool {
	os := runtime.GOOS
	if os == "linux" {
		return true
	}
	return false
}

func IsWin() bool {
	os := runtime.GOOS
	if os == "windows" {
		return true
	}
	return false
}

func IsMac() bool {
	os := runtime.GOOS
	if os == "darwin" {
		return true
	}
	return false
}

func UpdateCWTime() time.Time {
	var ok bool = true
	dir, err := os.Getwd()
	if err != nil {
		ok = false
	}
	dirinfo, err := os.Stat(dir)
	if err != nil {
		ok = false
	}

	var t time.Time
	if ok {
		t = dirinfo.ModTime()
	} else {
		t = time.Now()
	}

	y, _ := time.ParseDuration("-14368h")
	return t.Add(y)
}

func Chtime(filename string) bool {
	err := os.Chtimes(filename, cwtime, cwtime)
	if err != nil {
		fmt.Println("[-] " + err.Error())
	}
	return true
}

func IsRoot() bool {
	if os.Getuid() == 0 {
		return true
	}
	return false
}

func GetFdLimit() int {
	cmd := exec.Command("sh", "-c", "ulimit -n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		println(err.Error())
		return -1
	}
	s := strings.TrimSpace(string(out))
	return ToInt(s)
}

func GetExcPath() string {
	file, _ := exec.LookPath(os.Args[0])
	// 获取包含可执行文件名称的路径
	path, _ := filepath.Abs(file)
	// 获取可执行文件所在目录
	index := strings.LastIndex(path, string(os.PathSeparator))
	ret := path[:index]
	return strings.Replace(ret, "\\", "/", -1) + "/"
}

func Fatal(s string) {
	fmt.Println("[-] " + s)
	os.Exit(0)
}
