package cases

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"0chain.net/chaincore/block"
	"0chain.net/conductor/config"
)

type (
	// BreakingSingleBlock represents implementation of the config.TestCase interface.
	//
	//	check that proposer can’t break single block
	//		(T0) Leader_0:  send Proposal0_0
	//		(T0 + δ + Δ) Replica_i: send Verification_0
	//		(T0 + 2δ + Δ) Leader_0: send Proposal0_1
	//		(T0 + 3δ + Δ) Replica_i: send Notarization0_0 #ignore Proposal0_1
	BreakingSingleBlock struct {
		cfg *BreakingSingleBlockCfg

		res *BreakingSingleBlockResult

		wg *sync.WaitGroup
	}

	BreakingSingleBlockCfg struct {
		FirstSentBlockHash  string `json:"first_sent_block_hash"`
		SecondSentBlockHash string `json:"second_sent_block_hash"`
	}

	BreakingSingleBlockResult struct {
		FinalisedBlockHash string       `json:"finalised_block_hash"`
		RoundBlocksInfo    []*BlockInfo `json:"round_blocks_info"`
	}

	BlockInfo struct {
		Hash               string `json:"hash"`
		Notarised          bool   `json:"notarised"`
		VerificationStatus int    `json:"verification_status"`
	}
)

var (
	// Ensure BreakingSingleBlock implements config.TestCase interface.
	_ config.TestCase = (*BreakingSingleBlock)(nil)
)

// NewBreakingSingleBlock creates initialised BreakingSingleBlock.
func NewBreakingSingleBlock() *BreakingSingleBlock {
	wg := new(sync.WaitGroup)
	wg.Add(2)
	return &BreakingSingleBlock{
		wg: wg,
	}
}

// Check implements config.TestCase interface.
func (n *BreakingSingleBlock) Check(ctx context.Context) (success bool, err error) {
	prepared := make(chan struct{})
	go func() {
		n.wg.Wait()
		prepared <- struct{}{}
	}()

	select {
	case <-ctx.Done():
		return false, errors.New("cases state is not prepared, context is done")

	case <-prepared:
		return n.check()
	}
}

func (n *BreakingSingleBlock) check() (success bool, err error) {
	if n.cfg.FirstSentBlockHash != n.res.FinalisedBlockHash {
		return false, errors.New("unexpected finalised block hash")
	}

	for _, bi := range n.res.RoundBlocksInfo {
		if bi.Hash == n.cfg.SecondSentBlockHash {
			switch {
			case bi.Notarised:
				return false, errors.New("second sent block must be not notarised")

			case bi.VerificationStatus != block.VerificationFailed:
				return false, errors.New("second sent block must be not notarised")
			}
		}
	}

	return true, nil
}

// Configure implements config.TestCase interface.
func (n *BreakingSingleBlock) Configure(blob []byte) error {
	defer n.wg.Done()
	n.cfg = new(BreakingSingleBlockCfg)
	return n.cfg.Decode(blob)
}

// AddResult implements config.TestCase interface.
func (n *BreakingSingleBlock) AddResult(blob []byte) error {
	defer n.wg.Done()
	n.res = newBreakingSingleBlockResult()
	return n.res.Decode(blob)
}

func newBreakingSingleBlockResult() *BreakingSingleBlockResult {
	return &BreakingSingleBlockResult{
		RoundBlocksInfo: make([]*BlockInfo, 0),
	}
}

// Encode encodes BreakingSingleBlockResult to bytes.
func (r *BreakingSingleBlockResult) Encode() ([]byte, error) {
	return json.Marshal(r)
}

// Decode decodes BreakingSingleBlockResult from bytes.
func (r *BreakingSingleBlockResult) Decode(blob []byte) error {
	return json.Unmarshal(blob, r)
}

// Encode encodes BreakingSingleBlockCfg to bytes.
func (r *BreakingSingleBlockCfg) Encode() ([]byte, error) {
	return json.Marshal(r)
}

// Decode decodes BreakingSingleBlockCfg from bytes.
func (r *BreakingSingleBlockCfg) Decode(blob []byte) error {
	return json.Unmarshal(blob, r)
}
