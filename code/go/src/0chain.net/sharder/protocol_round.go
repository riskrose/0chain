package sharder

import (
	"context"

	"0chain.net/block"
	"0chain.net/chain"
	. "0chain.net/logging"
	"0chain.net/round"
	"go.uber.org/zap"
)

/*AddNotarizedBlock - add a notarized block for a given round */
func (sc *Chain) AddNotarizedBlock(ctx context.Context, r round.RoundI, b *block.Block) bool {
	if _, ok := r.AddNotarizedBlock(b); !ok {
		return false
	}
	if sc.BlocksToSharder == chain.FINALIZED {
		nb := r.GetNotarizedBlocks()
		if len(nb) > 0 {
			Logger.Error("*** different blocks for the same round ***", zap.Any("round", b.Round), zap.Any("block", b.Hash), zap.Any("existing_block", nb[0].Hash))
		}
	}
	sc.UpdateNodeState(b)
	pr := sc.GetRound(r.GetRoundNumber() - 1)
	if pr != nil {
		go sc.FinalizeRound(ctx, pr, sc)
	}
	go sc.ComputeState(ctx, b)
	return true
}
