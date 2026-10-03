package main

import (
	types "github.com/chainreactors/cyber/core/types"
	scannerext "github.com/chainreactors/cyber/exts/scanner"
	searchext "github.com/chainreactors/cyber/exts/search"
	cfg "github.com/chainreactors/cyber/pkg/config"
)

// DistributeFromOption projects shared harness configuration for transport and UI.
func DistributeFromOption(option *cfg.Option) (*types.DistributeConfig, error) {
	if option == nil {
		return &types.DistributeConfig{}, nil
	}
	if _, err := scannerext.ReadCyberhub(option); err != nil {
		return nil, err
	}
	_, err := scannerext.ReadRecon(option)
	if err != nil {
		return nil, err
	}
	if _, err := scannerext.ReadScan(option); err != nil {
		return nil, err
	}
	_, err = searchext.ReadKeys(option)
	if err != nil {
		return nil, err
	}
	value, err := cfg.SharedFromOption(option)
	if err != nil {
		return nil, err
	}
	cfg.NormalizeLLMConfig(value.Llm)
	return value, nil
}
