// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package main

import (
	"fmt"
	"os"

	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/storage"
)

func main() {
	db, err := storage.NewDB(config.DBConfig{
		Driver: "sqlite",
		DSN:    "web3.db",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "db:", err)
		os.Exit(1)
	}

	fmt.Println("=== transactions with bridge tx_info (last 10) ===")
	var txs []storage.Transaction
	db.Where("tx_info LIKE '%bridge%'").Order("created_at desc").Limit(10).Find(&txs)
	if len(txs) == 0 {
		fmt.Println("  (empty)")
	}
	for _, tx := range txs {
		fmt.Printf("  hash=%s chain=%s\n  tx_info=%s\n\n", tx.Hash, tx.Chain, tx.TxInfo)
	}
}
