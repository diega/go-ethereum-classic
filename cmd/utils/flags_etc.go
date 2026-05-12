// Copyright 2015 The go-ethereum Authors
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

package utils

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/internal/flags"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/urfave/cli/v2"
)

var (
	// ETC network flags
	ClassicFlag = &cli.BoolFlag{
		Name:     "classic",
		Usage:    "Ethereum Classic mainnet: proof-of-work network (ChainID 61)",
		Category: flags.EthCategory,
	}
	MordorFlag = &cli.BoolFlag{
		Name:     "mordor",
		Usage:    "Mordor network: Ethereum Classic proof-of-work test network (ChainID 63)",
		Category: flags.EthCategory,
	}

	// Ethash settings (for ETC PoW mining)
	EthashCacheDirFlag = &flags.DirectoryFlag{
		Name:     "ethash.cachedir",
		Usage:    "Directory to store the ethash verification caches (default = inside the datadir)",
		Category: flags.EthCategory,
	}
	EthashCachesInMemoryFlag = &cli.IntFlag{
		Name:     "ethash.cachesinmem",
		Usage:    "Number of recent ethash caches to keep in memory (16MB each)",
		Value:    ethconfig.Defaults.Ethash.CachesInMem,
		Category: flags.EthCategory,
	}
	EthashCachesOnDiskFlag = &cli.IntFlag{
		Name:     "ethash.cachesondisk",
		Usage:    "Number of recent ethash caches to keep on disk (16MB each)",
		Value:    ethconfig.Defaults.Ethash.CachesOnDisk,
		Category: flags.EthCategory,
	}
	EthashCachesLockMmapFlag = &cli.BoolFlag{
		Name:     "ethash.cacheslockmmap",
		Usage:    "Lock memory maps of recent ethash caches",
		Category: flags.EthCategory,
	}
	EthashDatasetDirFlag = &flags.DirectoryFlag{
		Name:     "ethash.dagdir",
		Usage:    "Directory to store the ethash mining DAGs",
		Value:    flags.DirectoryString(ethconfig.Defaults.Ethash.DatasetDir),
		Category: flags.EthCategory,
	}
	EthashDatasetsInMemoryFlag = &cli.IntFlag{
		Name:     "ethash.dagsinmem",
		Usage:    "Number of recent ethash mining DAGs to keep in memory (1+GB each)",
		Value:    ethconfig.Defaults.Ethash.DatasetsInMem,
		Category: flags.EthCategory,
	}
	EthashDatasetsOnDiskFlag = &cli.IntFlag{
		Name:     "ethash.dagsondisk",
		Usage:    "Number of recent ethash mining DAGs to keep on disk (1+GB each)",
		Value:    ethconfig.Defaults.Ethash.DatasetsOnDisk,
		Category: flags.EthCategory,
	}
	EthashDatasetsLockMmapFlag = &cli.BoolFlag{
		Name:     "ethash.dagslockmmap",
		Usage:    "Lock memory maps for recent ethash mining DAGs",
		Category: flags.EthCategory,
	}
	EthashEpochLengthFlag = &cli.Int64Flag{
		Name:     "epoch.length",
		Usage:    "Sets epoch length for makecache & makedag commands",
		Value:    30000,
		Category: flags.EthCategory,
	}

	// Mining flags (for PoW chains)
	MiningEnabledFlag = &cli.BoolFlag{
		Name:     "mine",
		Usage:    "Enable PoW mining",
		Category: flags.MinerCategory,
	}
	MinerThreadsFlag = &cli.IntFlag{
		Name:     "miner.threads",
		Usage:    "Number of CPU threads to use for mining",
		Value:    0,
		Category: flags.MinerCategory,
	}
	MinerNotifyFlag = &cli.StringFlag{
		Name:     "miner.notify",
		Usage:    "Comma separated HTTP URL list to notify of new work packages",
		Category: flags.MinerCategory,
	}
	MinerNotifyFullFlag = &cli.BoolFlag{
		Name:     "miner.notify.full",
		Usage:    "Notify with pending block headers instead of work packages",
		Category: flags.MinerCategory,
	}
	MinerNoVerifyFlag = &cli.BoolFlag{
		Name:     "miner.noverify",
		Usage:    "Disable remote mining solution verification",
		Category: flags.MinerCategory,
	}
	FakePoWFlag = &cli.BoolFlag{
		Name:     "fakepow",
		Usage:    "Disables proof-of-work verification",
		Category: flags.LoggingCategory,
	}

	// MESS (ECBP-1100) artificial finality flags
	MESSForceEnableFlag = &cli.BoolFlag{
		Name:     "mess-force-enable",
		Usage:    "Force enable MESS regardless of ECBP1100Transition config (emergency override)",
		Category: flags.EthCategory,
	}
	MESSForceDisableFlag = &cli.BoolFlag{
		Name:     "mess-force-disable",
		Usage:    "Force disable MESS regardless of ECBP1100Transition config",
		Category: flags.EthCategory,
	}

	// ETCFlags is the flag group of Ethereum Classic networks.
	ETCFlags = []cli.Flag{
		ClassicFlag,
		MordorFlag,
		MESSForceEnableFlag,
		MESSForceDisableFlag,
	}
)

// homeDir returns the user's home directory.
func homeDir() string {
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	if usr, err := user.Current(); err == nil {
		return usr.HomeDir
	}
	return ""
}

// setEthashDatasetDir sets the ethash DAG directory based on context flags.
// For ETC networks, uses etchash directory instead of ethash.
func setEthashDatasetDir(ctx *cli.Context, cfg *ethconfig.Config) {
	switch {
	case ctx.IsSet(EthashDatasetDirFlag.Name):
		cfg.Ethash.DatasetDir = ctx.String(EthashDatasetDirFlag.Name)

	case (ctx.Bool(ClassicFlag.Name) || ctx.Bool(MordorFlag.Name)) && cfg.Ethash.DatasetDir == ethconfig.Defaults.Ethash.DatasetDir:
		// ECIP-1099 is set, use etchash dir for DAGs instead
		home := homeDir()

		if runtime.GOOS == "darwin" {
			cfg.Ethash.DatasetDir = filepath.Join(home, "Library", "Etchash")
		} else if runtime.GOOS == "windows" {
			localappdata := os.Getenv("LOCALAPPDATA")
			if localappdata != "" {
				cfg.Ethash.DatasetDir = filepath.Join(localappdata, "Etchash")
			} else {
				cfg.Ethash.DatasetDir = filepath.Join(home, "AppData", "Local", "Etchash")
			}
		} else {
			cfg.Ethash.DatasetDir = filepath.Join(home, ".etchash")
		}
	}
}

// setEthashCacheDir sets the ethash cache directory based on context flags.
// For ETC networks, uses etchash directory instead of ethash.
func setEthashCacheDir(ctx *cli.Context, cfg *ethconfig.Config) {
	switch {
	case ctx.IsSet(EthashCacheDirFlag.Name):
		cfg.Ethash.CacheDir = ctx.String(EthashCacheDirFlag.Name)

	case (ctx.Bool(ClassicFlag.Name) || ctx.Bool(MordorFlag.Name)) && cfg.Ethash.CacheDir == ethconfig.Defaults.Ethash.CacheDir:
		// ECIP-1099 is set, use etchash dir for caches instead
		cfg.Ethash.CacheDir = "etchash"
	}
}

// setEthash configures the ethash settings from CLI flags.
func setEthash(ctx *cli.Context, cfg *ethconfig.Config) {
	// ECIP-1099 directory configuration
	setEthashCacheDir(ctx, cfg)
	setEthashDatasetDir(ctx, cfg)

	if ctx.IsSet(EthashCachesInMemoryFlag.Name) {
		cfg.Ethash.CachesInMem = ctx.Int(EthashCachesInMemoryFlag.Name)
	}
	if ctx.IsSet(EthashCachesOnDiskFlag.Name) {
		cfg.Ethash.CachesOnDisk = ctx.Int(EthashCachesOnDiskFlag.Name)
	}
	if ctx.IsSet(EthashCachesLockMmapFlag.Name) {
		cfg.Ethash.CachesLockMmap = ctx.Bool(EthashCachesLockMmapFlag.Name)
	}
	if ctx.IsSet(EthashDatasetsInMemoryFlag.Name) {
		cfg.Ethash.DatasetsInMem = ctx.Int(EthashDatasetsInMemoryFlag.Name)
	}
	if ctx.IsSet(EthashDatasetsOnDiskFlag.Name) {
		cfg.Ethash.DatasetsOnDisk = ctx.Int(EthashDatasetsOnDiskFlag.Name)
	}
	if ctx.IsSet(EthashDatasetsLockMmapFlag.Name) {
		cfg.Ethash.DatasetsLockMmap = ctx.Bool(EthashDatasetsLockMmapFlag.Name)
	}
	if ctx.Bool(FakePoWFlag.Name) {
		cfg.Ethash.PowMode = ethash.ModeFake
	}
}

// setMinerETC configures the PoW mining settings from CLI flags.
func setMinerETC(ctx *cli.Context, cfg *miner.Config) {
	if ctx.IsSet(MinerNotifyFlag.Name) {
		cfg.Notify = strings.Split(ctx.String(MinerNotifyFlag.Name), ",")
	}
	if ctx.IsSet(MinerNotifyFullFlag.Name) {
		cfg.NotifyFull = ctx.Bool(MinerNotifyFullFlag.Name)
	}
	if ctx.IsSet(MinerNoVerifyFlag.Name) {
		cfg.Noverify = ctx.Bool(MinerNoVerifyFlag.Name)
	}
}
