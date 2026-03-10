// Copyright 2016 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"strconv"

	"github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/urfave/cli/v2"
)

var (
	makecacheCommand = &cli.Command{
		Action:    makecache,
		Name:      "makecache",
		Usage:     "Generate ethash verification cache (for testing)",
		ArgsUsage: "<blockNum> <outputDir>",
		Flags: []cli.Flag{
			utils.EthashEpochLengthFlag,
		},
		Description: `
The makecache command generates an ethash cache in <outputDir>.

This command exists to support the system testing project.
Regular users do not need to execute it.
`,
	}
	makedagCommand = &cli.Command{
		Action:    makedag,
		Name:      "makedag",
		Usage:     "Generate ethash mining DAG (for testing)",
		ArgsUsage: "<blockNum> <outputDir>",
		Flags: []cli.Flag{
			utils.EthashEpochLengthFlag,
		},
		Description: `
The makedag command generates an ethash DAG in <outputDir>.

This command exists to support the system testing project.
Regular users do not need to execute it.
`,
	}
)

// makecache generates an ethash verification cache into the provided folder.
func makecache(ctx *cli.Context) error {
	args := ctx.Args().Slice()
	if len(args) != 2 {
		utils.Fatalf(`Usage: geth makecache <block number> <outputdir>`)
	}
	block, err := strconv.ParseUint(args[0], 0, 64)
	if err != nil {
		utils.Fatalf("Invalid block number: %v", err)
	}

	epochLength := ctx.Uint64(utils.EthashEpochLengthFlag.Name)

	ethash.MakeCache(block, epochLength, args[1])

	return nil
}

// makedag generates an ethash mining DAG into the provided folder.
func makedag(ctx *cli.Context) error {
	args := ctx.Args().Slice()
	if len(args) != 2 {
		utils.Fatalf(`Usage: geth makedag <block number> <outputdir>`)
	}
	block, err := strconv.ParseUint(args[0], 0, 64)
	if err != nil {
		utils.Fatalf("Invalid block number: %v", err)
	}

	epochLength := ctx.Uint64(utils.EthashEpochLengthFlag.Name)

	ethash.MakeDataset(block, epochLength, args[1])

	return nil
}
