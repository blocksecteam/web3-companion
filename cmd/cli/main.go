// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package main

import "github.com/blocksecteam/web3-companion/internal/cli"

var (
	version = "dev"
)

func main() {
	cli.SetBuildInfo(version)
	cli.Run()
}
