// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package main

import "github.com/blocksecteam/web3-companion/internal/server"

var (
	version = "dev"
)

func main() {
	server.SetBuildInfo(version)
	server.Main()
}
