package main

// Command registration via init() side effects for the web entrypoint.

import (
	_ "github.com/chainreactors/aiscan/pkg/tools"
	_ "github.com/chainreactors/aiscan/pkg/tools/ioa"
	_ "github.com/chainreactors/aiscan/pkg/tools/proxy"
	_ "github.com/chainreactors/aiscan/pkg/tools/search"
)
