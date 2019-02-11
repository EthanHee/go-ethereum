// Copyright 2018 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package ethash

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
)

var errEthashStopped = errors.New("ethash stopped")

// API exposes ethash related methods for the RPC interface.
type API struct {
	ethash *Ethash // Make sure the mode of ethash is normal.
}

// GetWork returns a work package for external miner.
//
// The work package consists of 3 strings:
//   result[0] - 32 bytes hex encoded current block header pow-hash
//   result[1] - 32 bytes hex encoded seed hash used for DAG
//   result[2] - 32 bytes hex encoded boundary condition ("target"), 2^256/difficulty
//   result[3] - hex encoded block number
//   result[4], 32 bytes hex encoded parent block header pow-hash
//   result[5], hex encoded gas limit
//   result[6], hex encoded gas used
//   result[7], hex encoded transaction count
//   result[8], hex encoded uncle count
func (api *API) GetWork() ([9]string, error) {
	if api.ethash.config.PowMode != ModeNormal && api.ethash.config.PowMode != ModeTest {
		return [9]string{}, errors.New("not supported")
	}

	var (
		workCh = make(chan [9]string, 1)
		errc   = make(chan error, 1)
	)

	select {
	case api.ethash.fetchWorkCh <- &sealWork{errc: errc, res: workCh}:
	case <-api.ethash.exitCh:
		return [9]string{}, errEthashStopped
	}

	select {
	case work := <-workCh:
		return work, nil
	case err := <-errc:
		return [9]string{}, err
	}
}

// SubmitWork can be used by external miner to submit their POW solution.
// It returns an indication if the work was accepted.
// Note either an invalid solution, a stale work a non-existent work will return false.
func (api *API) SubmitWork(nonce types.BlockNonce, hash, digest common.Hash) bool {
	if api.ethash.config.PowMode != ModeNormal && api.ethash.config.PowMode != ModeTest {
		return false
	}

	var errorCh = make(chan error, 1)
	var blockHashCh = make(chan common.Hash, 1)

	select {
	case api.ethash.submitWorkCh <- &mineResult{
		nonce:       nonce,
		mixDigest:   digest,
		hash:        hash,
		errorCh:     errorCh,
		blockHashCh: blockHashCh,
	}:
	case <-api.ethash.exitCh:
		return false
	}

	select {
	case <-errorCh:
		return false
	case <-blockHashCh:
		return true
	}
}

// SubmitWorkDetail is similar to eth_submitWork but will return the block hash on success,
// and return an explicit error message on failure.
//
// Params (same as `eth_submitWork`):
//   [
//       "<nonce>",
//       "<pow_hash>",
//       "<mix_hash>"
//   ]
//
// Result on success:
//   "block_hash"
//
// Error on failure:
//   {code: -32005, message: "Cannot submit work.", data: "<reason for submission failure>"}
//
// See the original proposal here: <https://github.com/paritytech/parity-ethereum/pull/9404>
//
func (api *API) SubmitWorkDetail(nonce types.BlockNonce, hash, digest common.Hash) (blockHash common.Hash, err rpc.ErrorWithInfo) {
	if api.ethash.config.PowMode != ModeNormal && api.ethash.config.PowMode != ModeTest {
		err = cannotSubmitWorkError{"not supported"}
		return
	}

	var errorCh = make(chan error, 1)
	var blockHashCh = make(chan common.Hash, 1)

	select {
	case api.ethash.submitWorkCh <- &mineResult{
		nonce:       nonce,
		mixDigest:   digest,
		hash:        hash,
		errorCh:     errorCh,
		blockHashCh: blockHashCh,
	}:
	case <-api.ethash.exitCh:
		err = cannotSubmitWorkError{errEthashStopped.Error()}
		return
	}

	select {
	case submitErr := <-errorCh:
		err = cannotSubmitWorkError{submitErr.Error()}
		return
	case blockHash = <-blockHashCh:
		return
	}
}

// SubmitHashRate can be used for remote miners to submit their hash rate.
// This enables the node to report the combined hash rate of all miners
// which submit work through this node.
//
// It accepts the miner hash rate and an identifier which must be unique
// between nodes.
func (api *API) SubmitHashRate(rate hexutil.Uint64, id common.Hash) bool {
	if api.ethash.config.PowMode != ModeNormal && api.ethash.config.PowMode != ModeTest {
		return false
	}

	var done = make(chan struct{}, 1)

	select {
	case api.ethash.submitRateCh <- &hashrate{done: done, rate: uint64(rate), id: id}:
	case <-api.ethash.exitCh:
		return false
	}

	// Block until hash rate submitted successfully.
	<-done

	return true
}

// GetHashrate returns the current hashrate for local CPU miner and remote miner.
func (api *API) GetHashrate() uint64 {
	return uint64(api.ethash.Hashrate())
}
