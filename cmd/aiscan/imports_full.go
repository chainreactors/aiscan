//go:build full

package main

import (
	_ "github.com/chainreactors/aiscan/pkg/tools/katana"
	_ "github.com/chainreactors/aiscan/pkg/tools/passive"
	_ "github.com/chainreactors/aiscan/pkg/tools/playwright"
)
