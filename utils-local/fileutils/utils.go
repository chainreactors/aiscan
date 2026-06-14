package fileutils

import (
	"bytes"
	"encoding/base64"
	"errors"
	"github.com/chainreactors/utils/encode"
	"io"
	"io/ioutil"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

func CreateFile(filename string) (*os.File, error) {
	var err error
	var filehandle *os.File
	if _, err := os.Stat(filename); err == nil { //如果文件存在
		return nil, errors.New("File already exists")
	} else {
		filehandle, err = os.Create(filename) //创建文件
		if err != nil {
			return nil, err
		}
	}
	return filehandle, err
}

func OverWriteFile(filename string, mod int) (*os.File, error) {
	var err error
	var filehandle *os.File
	if _, err := os.Stat(filename); err == nil { //如果文件存在
		filehandle, err = os.OpenFile(filename, mod, 0600)
		if err != nil {
			return nil, err
		}
	} else {
		filehandle, err = os.Create(filename) //创建文件
		if err != nil {
			return nil, err
		}
	}
	return filehandle, err
}

// Open file from current env and binary path
func Open(filename string) (*os.File, error) {
	f, err := os.Open(filename)
	if err == nil {
		return f, nil
	}

	f, err = os.Open(path.Join(GetExcPath(), filename))
	if err != nil {
		return nil, err
	}
	return f, nil
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

var Key = []byte{}

func DecryptFile(file io.Reader, keys []byte) []byte {
	content, err := ioutil.ReadAll(file)
	if err != nil {
		println(err.Error())
	}
	decoded, err := base64.StdEncoding.DecodeString(string(content))
	if err == nil {
		// try to base64 decode, if decode successfully, return data
		return bytes.TrimSpace(decoded)
	}
	// else try to unflate
	decrypted := encode.XorEncode(content, keys, 0)
	if shannonEntropy(content) < 5.5 {
		// 使用香农熵判断是否是deflate后的文件, 测试了几个数据集, 数据量较大的时候接近4, 数据量较小时接近5. deflate后的文件大多都在6以上
		return content
	} else {
		return bytes.TrimSpace(encode.MustDeflateDeCompress(decrypted))
	}
}

func LoadCommonArg(arg string) []byte {
	var content []byte
	f, err := Open(arg)
	if err != nil {
		// if open not found , try base64 decode
		content, err = base64.StdEncoding.DecodeString(arg)
		if err != nil {
			return []byte(arg)
		} else {
			return content
		}
	}
	return DecryptFile(f, Key)
}

func shannonEntropy(data []byte) float64 {
	freq := make(map[byte]int)
	for _, b := range data {
		freq[b]++
	}

	entropy := 0.0
	dataLength := len(data)
	for _, count := range freq {
		freqRatio := float64(count) / float64(dataLength)
		entropy -= freqRatio * math.Log2(freqRatio)
	}

	return entropy
}

func HasStdin() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}

	isPipedFromChrDev := (stat.Mode() & os.ModeCharDevice) == 0
	isPipedFromFIFO := (stat.Mode() & os.ModeNamedPipe) != 0

	return isPipedFromChrDev || isPipedFromFIFO
}

func IsExist(filename string) bool {
	var exist = true
	if _, err := os.Stat(filename); err != nil {
		exist = false
	}
	return exist
}
