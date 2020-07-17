package chain

import (
	"context"
	"sort"
	"sync"
	"time"

	"0chain.net/chaincore/block"
	"0chain.net/chaincore/config"
	"0chain.net/chaincore/node"
	"0chain.net/chaincore/round"
	"0chain.net/core/common"
	"0chain.net/core/datastore"

	. "0chain.net/core/logging"
	"go.uber.org/zap"
)

// FinalizedBlockFromShardersGetter represents
// FB fetcher. The Chain implements it.
type FinalizedBlockFromShardersGetter interface {
	GetFinalizedBlockFromSharders(context.Context, string) (*block.Block, error)
	asyncFetchFinalizedBlock(context.Context, string, chan<- string)
}

// FBRequestor represents FB from sharders requestor.
var FBRequestor node.EntityRequestor

// - Setup FBRequestor on start up.
func SetupFBRequestor() {
	var options = node.SendOptions{
		Timeout:  node.TimeoutLargeMessage,
		CODEC:    node.CODEC_MSGPACK,
		Compress: true,
	}
	FBRequestor = node.RequestEntityHandler("/v1/block/get", &options,
		datastore.GetEntityMetadata("block"))
}

// FinalizedBlockFetcher fetches a FB from sharders.
type FinalizedBlockFetcher struct {
	add    chan string
	got    chan string
	fetch  chan string
	getter FinalizedBlockFromShardersGetter
}

func NewFinalizedBlockFetcher(chain FinalizedBlockFromShardersGetter) (
	fbf *FinalizedBlockFetcher) {

	fbf = new(FinalizedBlockFetcher)
	fbf.add = make(chan string, 100)
	fbf.got = make(chan string, 100)
	fbf.fetch = make(chan string, 100)
	fbf.getter = chain
	return
}

// AsyncFetchFinalizedBlockFromSharders fetches a FB from all sharders from
// current MB.
func (fbf *FinalizedBlockFetcher) AsyncFetchFinalizedBlockFromSharders(
	ctx context.Context, hash string) {

	select {
	case fbf.add <- hash:
	case <-ctx.Done():
	}
}

// StartFinalizedBlockFetcherWorker starts FB from sharders fetcher.
func (fbf *FinalizedBlockFetcher) StartFinalizedBlockFetcherWorker(
	ctx context.Context) {

	var (
		lt       = config.GetFBFetchingLifetime()
		tick     = time.NewTicker(lt)
		fetching = make(map[string]time.Time)

		now time.Time
	)

	defer tick.Stop()

	for {
		select {

		// the FB has fetched or received another way
		case hash := <-fbf.got:
			delete(fetching, hash)

		// fetch new FB
		case hash := <-fbf.add:
			now = time.Now()
			if tp, ok := fetching[hash]; ok && now.Sub(tp) < lt {
				continue // fetching
			}
			fetching[hash] = time.Now()
			go fbf.getter.asyncFetchFinalizedBlock(ctx, hash, fbf.got)

		// cleanup the fetching list every 'lifetime' from old FB requested
		case <-tick.C:
			now = time.Now()
			for hash, tp := range fetching {
				if now.Sub(tp) >= lt {
					delete(fetching, hash) // lifetime exceeded
				}
			}

		// stop when context is done
		case <-ctx.Done():
			return
		}
	}

}

func (c *Chain) asyncFetchFinalizedBlock(ctx context.Context,
	hash string, got chan<- string) {

	var err error
	if _, err = c.GetBlock(ctx, hash); err == nil {
		select {
		case got <- hash:
		case <-ctx.Done():
			return
		}
		return // already have the block
	}

	Logger.Info("get FB from sharders", zap.String("block", hash),
		zap.Int64("current_round", c.GetCurrentRound()))

	var fb *block.Block
	if fb, err = c.GetFinalizedBlockFromSharders(ctx, hash); err != nil {
		Logger.Error("getting FB from sharders", zap.Error(err))
		return
	}

	var r = c.GetRound(fb.Round)
	if r == nil {
		Logger.Info("get FB - no round will create...",
			zap.Int64("round", fb.Round), zap.String("block", hash),
			zap.Int64("current_round", c.GetCurrentRound()))

		r = c.RoundF.CreateRoundF(fb.Round).(*round.Round)
		c.AddRound(r)
	}

	err = c.VerifyNotarization(ctx, fb.Hash, fb.GetVerificationTickets(),
		r.GetRoundNumber())
	if err != nil {
		Logger.Error("get FB - validate notarization",
			zap.Int64("round", fb.Round), zap.String("block", hash),
			zap.Error(err))
		return
	}

	if err = fb.Validate(ctx); err != nil {
		Logger.Error("get FB - validate", zap.Int64("round", fb.Round),
			zap.String("block", hash), zap.Any("block_obj", fb), zap.Error(err))
		return
	}

	Logger.Info("got FB", zap.String("block", fb.Hash),
		zap.Int64("round", fb.Round),
		zap.Int("verifictation_tickers", fb.VerificationTicketsSize()))
	var b = c.AddBlock(fb)
	b, r = c.AddNotarizedBlockToRound(r, fb)
	b, _ = r.AddNotarizedBlock(b)
	if b == fb {
		go c.fetchedNotarizedBlockHandler.NotarizedBlockFetched(ctx, fb)
		if node.Self.Type == node.NodeTypeSharder {
			// TODO (sfxdx): do we need an additional work here for
			//               sharders to force blocks finalization?
		}
	}

	select {
	case got <- hash:
	case <-ctx.Done():
		return
	}
}

// GetFinalizedBlockFromSharders - request for a finalized block from all
// sharders from current magic block.
func (c *Chain) GetFinalizedBlockFromSharders(ctx context.Context,
	hash string) (fb *block.Block, err error) {

	type blockConsensus struct {
		*block.Block
		consensus int
	}

	var (
		mb              = c.GetCurrentMagicBlock()
		sharders        = mb.Sharders
		finalizedBlocks = make([]*blockConsensus, 0, 1)

		fbMutex sync.Mutex
	)

	var handler = func(ctx context.Context, entity datastore.Entity) (
		resp interface{}, err error) {

		var fb, ok = entity.(*block.Block)
		if !ok {
			return nil, datastore.ErrInvalidEntity
		}

		// verify the block fist?

		fbMutex.Lock()
		defer fbMutex.Unlock()
		for i, b := range finalizedBlocks {
			if b.Hash == fb.Hash {
				finalizedBlocks[i].consensus++
				return fb, nil
			}
		}
		finalizedBlocks = append(finalizedBlocks, &blockConsensus{
			Block:     fb,
			consensus: 1,
		})

		return fb, nil
	}

	sharders.RequestEntityFromAll(ctx, FBRequestor, nil, handler)

	// highest (the first sorting order), most popular (the second order)
	sort.Slice(finalizedBlocks, func(i int, j int) bool {
		return finalizedBlocks[i].Round >= finalizedBlocks[j].Round ||
			finalizedBlocks[i].consensus > finalizedBlocks[j].consensus

	})

	if len(finalizedBlocks) == 0 {
		return nil, common.NewError("fb_fetcher", "no FB given")
	}

	return finalizedBlocks[0].Block, nil // highest, most popular
}
