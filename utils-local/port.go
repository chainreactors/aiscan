package utils

import (
	"fmt"
	"github.com/chainreactors/utils/iutils"
	"strconv"
	"strings"
)

var PrePort *PortPreset

func ParsePortsString(s string) []string {
	return PrePort.ParsePortString(s)
}

func ParsePortsSlice(ports []string) []string {
	if PrePort == nil {
		fmt.Println("PrePort is nil, please NewPortPreset")
		return ports
	}
	return PrePort.ParsePortSlice(ports)
}

type PortConfig struct {
	Name  string   `json:"name" yaml:"name"`
	Ports []string `json:"ports" yaml:"ports"`
	Tags  []string `json:"tags" yaml:"tags"`
}

type PortMapper map[string][]string

func (p PortMapper) Get(name string) []string {
	return p[name]
}

func (p PortMapper) Set(name string, ports []string) {
	p[name] = ports
}

func (p PortMapper) Append(name string, ports ...string) {
	p[name] = append(p[name], ports...)
}

func NewPortPreset(conf []*PortConfig) *PortPreset {
	preset := &PortPreset{
		NameMap: make(PortMapper),
		PortMap: make(PortMapper),
		TagMap:  make(PortMapper),
	}
	for _, v := range conf {
		ports := expandPorts(v.Ports)
		preset.NameMap.Append(v.Name, ports...)
		for _, t := range v.Tags {
			preset.TagMap.Append(t, ports...)
		}
		for _, p := range ports {
			preset.PortMap.Append(p, v.Name)
		}
	}
	return preset
}

type PortPreset struct {
	NameMap PortMapper
	PortMap PortMapper
	TagMap  PortMapper
}

// 端口预设
func (preset PortPreset) ChoicePort(portname string) []string {
	var ports []string
	if portname == "all" {
		for p := range preset.PortMap {
			ports = append(ports, p)
		}
		return ports
	}

	if preset.NameMap.Get(portname) != nil {
		ports = append(ports, preset.NameMap.Get(portname)...)
		return ports
	} else if preset.TagMap.Get(portname) != nil {
		ports = append(ports, preset.TagMap.Get(portname)...)
		return ports
	} else {
		return []string{portname}
	}
}

func (preset PortPreset) ParsePortString(portstring string) []string {
	portstring = strings.TrimSpace(portstring)
	portstring = strings.Replace(portstring, "\r", "", -1)
	return preset.ParsePortSlice(strings.Split(portstring, ","))
}

func (preset PortPreset) ParsePortSlice(ports []string) []string {
	var portSlice []string
	var excludePorts []string

	for _, portname := range ports {
		portname = strings.TrimSpace(portname)
		if len(portname) == 0 {
			continue
		}

		if len(portname) > 1 && (portname[0] == '!') {
			// 处理带负号或感叹号的值，加入到排除列表中
			excludePorts = append(excludePorts, preset.ChoicePort(portname[1:])...)
		} else {
			// 处理普通值，加入到端口列表中
			portSlice = append(portSlice, preset.ChoicePort(portname)...)
		}
	}

	portSlice = expandPorts(portSlice)
	excludePorts = expandPorts(excludePorts)
	portSlice = removeExcludedPorts(portSlice, excludePorts)

	return iutils.StringsUnique(portSlice)
}

func removeExcludedPorts(portSlice, excludePorts []string) []string {
	excludeMap := make(map[string]struct{})
	for _, port := range excludePorts {
		excludeMap[port] = struct{}{}
	}

	var result []string
	for _, port := range portSlice {
		if _, found := excludeMap[port]; !found {
			result = append(result, port)
		}
	}
	return result
}

// 将string格式的port range 转为单个port组成的slice
func expandPorts(ports []string) []string {
	var tmpports []string
	for _, pr := range ports {
		if len(pr) == 0 {
			continue
		}
		pr = strings.TrimSpace(pr)
		if pr[0] == '-' {
			pr = "1" + pr
		}
		if pr[len(pr)-1] == '-' {
			pr = pr + "65535"
		}
		tmpports = append(tmpports, expandPort(pr)...)
	}
	return tmpports
}

// 将string格式的port range 转为单个port组成的slice
func expandPort(port string) []string {
	var tmpports []string
	if strings.Contains(port, "-") {
		sf := strings.Split(port, "-")
		start, _ := strconv.Atoi(sf[0])
		fin, _ := strconv.Atoi(sf[1])
		for port := start; port <= fin; port++ {
			tmpports = append(tmpports, strconv.Itoa(port))
		}
	} else {
		tmpports = append(tmpports, port)
	}
	return tmpports
}
