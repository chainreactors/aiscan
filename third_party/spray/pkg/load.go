package pkg

import (
	"fmt"
	"os"
	"strings"

	"github.com/chainreactors/fingers"
	fingerscommon "github.com/chainreactors/fingers/common"
	"github.com/chainreactors/fingers/favicon"
	fingerslib "github.com/chainreactors/fingers/fingers"
	"github.com/chainreactors/parsers"
	"github.com/chainreactors/utils"
	"github.com/chainreactors/utils/iutils"
	"github.com/chainreactors/words/mask"
	yaml "sigs.k8s.io/yaml/goyaml.v3"
)

func LoadPorts() error {
	var err error
	var ports []*utils.PortConfig
	err = yaml.Unmarshal(LoadConfig("port"), &ports)
	if err != nil {
		return err
	}
	utils.PrePort = utils.NewPortPreset(ports)
	return nil
}

func LoadFingers() error {
	var err error
	ActivePath = ActivePath[:0]
	// Build the engine in a local variable and only publish it to the package
	// global once Compile() has populated Aliases. Otherwise concurrent
	// readers (e.g. spray BrutePool goroutines running Baseline.Collect →
	// Engine.WebMatch → MergeFrameworks) can observe pkg.FingerEngine with a
	// nil embedded *alias.Aliases and panic on engine.Aliases.FindFramework.
	var engine *fingers.Engine
	if httpData := LoadConfig("http"); len(httpData) > 0 {
		httpFingers, err := fingerslib.LoadFingers(httpData)
		if err != nil {
			return err
		}
		socketFingers, err := fingerslib.LoadFingers(LoadConfig("socket"))
		if err != nil {
			return err
		}
		fingerImpl, err := fingerslib.NewEngine(httpFingers, socketFingers)
		if err != nil {
			return err
		}
		engine = &fingers.Engine{
			EnginesImpl:  make(map[string]fingers.EngineImpl),
			Enabled:      make(map[string]bool),
			Capabilities: make(map[string]fingerscommon.EngineCapability),
		}
		faviconEngine := favicon.NewFavicons()
		engine.EnginesImpl[fingers.FaviconEngine] = faviconEngine
		engine.Capabilities[fingers.FaviconEngine] = faviconEngine.Capability()
		engine.Register(fingerImpl)
		if err := engine.Compile(); err != nil {
			return err
		}
	} else {
		engine, err = fingers.NewEngine(fingers.FingersEngine)
		if err != nil {
			return err
		}
	}
	// Atomic-by-word-size pointer swap publishes a fully initialized engine.
	FingerEngine = engine
	for _, f := range FingerEngine.Fingers().HTTPFingers {
		if f.SendDataStr != "" {
			ActivePath = append(ActivePath, f.SendDataStr)
		}
		for _, rule := range f.Rules {
			if rule.SendDataStr != "" {
				ActivePath = append(ActivePath, rule.SendDataStr)
			}
		}
	}

	return nil
}

func LoadTemplates() error {
	var err error
	Rules = make(map[string]string)
	Dicts = make(map[string][]string)
	ExtractRegexps = make(parsers.Extractors)
	Extractors = make(parsers.Extractors)
	// load rule

	err = yaml.Unmarshal(LoadConfig("spray_rule"), &Rules)
	if err != nil {
		return err
	}

	// load default words
	var dicts map[string]string
	err = yaml.Unmarshal(LoadConfig("spray_dict"), &dicts)
	if err != nil {
		return err
	}
	for name, wordlist := range dicts {
		dict := strings.Split(strings.TrimSpace(wordlist), "\n")
		for i, d := range dict {
			dict[i] = strings.TrimSpace(d)
		}
		Dicts[strings.TrimSuffix(name, ".txt")] = dict
	}

	// load mask
	var keywords map[string]interface{}
	err = yaml.Unmarshal(LoadConfig("spray_common"), &keywords)
	if err != nil {
		return err
	}

	for k, v := range keywords {
		items, ok := v.([]interface{})
		if !ok {
			continue
		}
		t := make([]string, len(items))
		for i, vv := range items {
			t[i] = iutils.ToString(vv)
		}
		mask.SpecialWords[k] = t
	}

	// Load legacy extractors (still used for URL crawl regexps: js, url)
	var extracts []*parsers.Extractor
	err = yaml.Unmarshal(LoadConfig("extract"), &extracts)
	if err != nil {
		return err
	}

	for _, extract := range extracts {
		extract.Compile()

		ExtractRegexps[extract.Name] = []*parsers.Extractor{extract}
		for _, tag := range extract.Tags {
			if _, ok := ExtractRegexps[tag]; !ok {
				ExtractRegexps[tag] = []*parsers.Extractor{extract}
			} else {
				ExtractRegexps[tag] = append(ExtractRegexps[tag], extract)
			}
		}
	}

	// Load proton templates for high-performance extraction
	if err := LoadProtonRules(); err != nil {
		return err
	}

	return nil
}

// LoadProtonRules loads embedded proton YAML templates.
// The data is a YAML array of template objects produced by recuLoadPoc.
func LoadProtonRules() error {
	rulesData := LoadConfig("proton_rules")
	if len(rulesData) == 0 {
		return nil
	}

	var templates []interface{}
	if err := yaml.Unmarshal(rulesData, &templates); err != nil {
		return err
	}

	docs := make([][]byte, 0, len(templates))
	for _, tmpl := range templates {
		doc, err := yaml.Marshal(tmpl)
		if err != nil {
			continue
		}
		docs = append(docs, doc)
	}
	return LoadProtonTemplates(docs)
}

func LoadExtractorConfig(filename string) ([]*parsers.Extractor, error) {
	var extracts []*parsers.Extractor
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	err = yaml.Unmarshal(content, &extracts)
	if err != nil {
		return nil, err
	}

	for _, extract := range extracts {
		extract.Compile()
	}

	return extracts, nil
}

func Load() error {
	err := LoadPorts()
	if err != nil {
		return fmt.Errorf("load ports, %w", err)
	}
	err = LoadTemplates()
	if err != nil {
		return fmt.Errorf("load templates, %w", err)
	}

	return nil
}

func LoadNeutron() error {
	return LoadNeutronTemplates()
}
