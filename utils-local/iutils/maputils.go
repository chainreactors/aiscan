package iutils

import (
	"fmt"
)

// MergeMaps merges two maps into a New map
func MergeMaps(m1, m2 map[string]interface{}) map[string]interface{} {
	m := make(map[string]interface{}, len(m1)+len(m2))
	for k, v := range m1 {
		m[k] = v
	}
	for k, v := range m2 {
		m[k] = v
	}
	return m
}

// 转为字符串
func Map2String(m map[string]interface{}) string {
	if m == nil || len(m) == 0 {
		return ""
	}
	var s string
	for k, v := range m {
		s += fmt.Sprintf(" %s:%s ", k, ToString(v))
	}
	return s
}

// 转为map[string]string
func ToStringMap(i interface{}) map[string]string {
	var m = map[string]string{}

	switch v := i.(type) {
	case map[interface{}]interface{}:
		for k, val := range v {
			m[ToString(k)] = ToString(val)
		}
		return m
	case map[string]interface{}:
		for k, val := range v {
			m[k] = ToString(val)
		}
		return m
	default:
		return nil
	}
}

func MapToString(m map[string]interface{}) string {
	if m == nil || len(m) == 0 {
		return ""
	}
	var s string
	for k, v := range m {
		s += fmt.Sprintf(" %s:%v ", k, v)
	}
	return s
}
