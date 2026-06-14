package fileutils

import (
	"io/ioutil"
	"strings"
)

var (
	DictCache = make(map[string][]string)
)

func LoadFileToSlice(filename string) ([]string, error) {
	var ss []string
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	ss = strings.Split(strings.TrimSpace(string(content)), "\n")

	// 统一windows与linux的回车换行差异
	for i, word := range ss {
		ss[i] = strings.TrimSpace(word)
	}

	return ss, nil
}

func LoadFileWithCache(filename string) ([]string, error) {
	if dict, ok := DictCache[filename]; ok {
		return dict, nil
	}
	dict, err := LoadFileToSlice(filename)
	if err != nil {
		return nil, err
	}
	DictCache[filename] = dict
	return dict, nil
}

func LoadDictionaries(filenames []string) ([][]string, error) {
	dicts := make([][]string, len(filenames))
	for i, name := range filenames {
		dict, err := LoadFileWithCache(name)
		if err != nil {
			return nil, err
		}
		dicts[i] = dict
	}
	return dicts, nil
}
