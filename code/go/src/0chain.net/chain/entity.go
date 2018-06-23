package chain

import (
	"context"
	"fmt"
	"sync"

	"0chain.net/block"
	"0chain.net/common"
	"0chain.net/datastore"
	. "0chain.net/logging"
	"0chain.net/node"
	"0chain.net/round"
	"go.uber.org/zap"
)

/*ServerChain - the chain object of the chain  the server is responsible for */
var ServerChain *Chain

/*SetServerChain - set the server chain object */
func SetServerChain(c *Chain) {
	ServerChain = c
}

/*GetServerChain - returns the chain object for the server chain */
func GetServerChain() *Chain {
	return ServerChain
}

/*BlockStateHandler - handles the block state changes */
type BlockStateHandler interface {
	UpdatePendingBlock(ctx context.Context, b *block.Block, txns []datastore.Entity)
	UpdateFinalizedBlock(ctx context.Context, b *block.Block)
}

/*Chain - data structure that holds the chain data*/
type Chain struct {
	datastore.IDField
	datastore.VersionField
	datastore.CreationDateField
	ClientID      datastore.Key `json:"client_id"`                 // Client who created this chain
	ParentChainID datastore.Key `json:"parent_chain_id,omitempty"` // Chain from which this chain is forked off

	Decimals  int8  `json:"decimals"`   // Number of decimals allowed for the token on this chain
	BlockSize int32 `json:"block_size"` // Number of transactions in a block

	/*Miners - this is the pool of miners */
	Miners *node.Pool `json:"-"`

	/*Sharders - this is the pool of sharders */
	Sharders *node.Pool `json:"-"`

	/*Blobbers - this is the pool of blobbers */
	Blobbers *node.Pool `json:"-"`

	GenesisBlockHash string `json:"genesis_block_hash"`

	blocksMutex *sync.Mutex
	/* This is a cache of blocks that may include speculative blocks */
	Blocks               map[datastore.Key]*block.Block `json:"-"`
	LatestFinalizedBlock *block.Block                   `json:"latest_finalized_block,omitempty"` // Latest block on the chain the program is aware of
	CurrentRound         int64
	CurrentMagicBlock    *block.Block
}

var chainEntityMetadata *datastore.EntityMetadataImpl

/*GetEntityMetadata - implementing the interface */
func (c *Chain) GetEntityMetadata() datastore.EntityMetadata {
	return chainEntityMetadata
}

/*Validate - implementing the interface */
func (c *Chain) Validate(ctx context.Context) error {
	if datastore.IsEmpty(c.ID) {
		return common.InvalidRequest("chain id is required")
	}
	if datastore.IsEmpty(c.ClientID) {
		return common.InvalidRequest("client id is required")
	}
	return nil
}

/*Read - store read */
func (c *Chain) Read(ctx context.Context, key datastore.Key) error {
	return c.GetEntityMetadata().GetStore().Read(ctx, key, c)
}

/*Write - store read */
func (c *Chain) Write(ctx context.Context) error {
	return c.GetEntityMetadata().GetStore().Write(ctx, c)
}

/*Delete - store read */
func (c *Chain) Delete(ctx context.Context) error {
	return c.GetEntityMetadata().GetStore().Delete(ctx, c)
}

/*Provider - entity provider for chain object */
func Provider() datastore.Entity {
	c := &Chain{}
	c.Initialize()
	c.Version = "1.0"
	c.blocksMutex = &sync.Mutex{}
	c.InitializeCreationDate()
	c.Miners = node.NewPool(node.NodeTypeMiner)
	c.Sharders = node.NewPool(node.NodeTypeSharder)
	c.Blobbers = node.NewPool(node.NodeTypeBlobber)
	return c
}

/*Initialize - intializes internal datastructures to start again */
func (c *Chain) Initialize() {
	c.Blocks = make(map[string]*block.Block)
	c.CurrentRound = 0
	c.LatestFinalizedBlock = nil
	c.CurrentMagicBlock = nil
}

/*SetupEntity - setup the entity */
func SetupEntity(store datastore.Store) {
	chainEntityMetadata = datastore.MetadataProvider()
	chainEntityMetadata.Name = "chain"
	chainEntityMetadata.Provider = Provider
	chainEntityMetadata.Store = store
	datastore.RegisterEntityMetadata("chain", chainEntityMetadata)
}

/*GenerateGenesisBlock - Create the genesis block for the chain */
func (c *Chain) GenerateGenesisBlock(hash string) (*round.Round, *block.Block) {
	c.GenesisBlockHash = hash
	gb := datastore.GetEntityMetadata("block").Instance().(*block.Block)
	gb.Hash = hash
	gb.Round = 0
	gr := datastore.GetEntityMetadata("round").Instance().(*round.Round)
	gr.Number = 0
	gr.Block = gb
	gr.AddNotarizedBlock(gb)
	return gr, gb
}

/*AddGenesisBlock - adds the genesis block to the chain */
func (c *Chain) AddGenesisBlock(b *block.Block) {
	if b.Round != 0 {
		return
	}
	c.LatestFinalizedBlock = b // Genesis block is always finalized
	c.CurrentMagicBlock = b    // Genesis block is always a magic block
	c.Blocks[b.Hash] = b
	return
}

/*AddBlock - adds a block to the cache */
func (c *Chain) AddBlock(b *block.Block) {
	if b.Round <= c.LatestFinalizedBlock.Round {
		return
	}
	c.blocksMutex.Lock()
	defer c.blocksMutex.Unlock()
	if _, ok := c.Blocks[b.Hash]; ok {
		return
	}
	c.Blocks[b.Hash] = b
	if b.PrevBlock == nil {
		pb, ok := c.Blocks[b.PrevHash]
		if ok {
			b.PrevBlock = pb
		} else {
			Logger.Debug("previous block not present", zap.Any("round", b.Round), zap.Any("block", b.Hash), zap.Any("prev_block", b.PrevHash))
		}
	}
}

/*GetBlock - returns a known block for a given hash from the cache */
func (c *Chain) GetBlock(ctx context.Context, hash string) (*block.Block, error) {
	c.blocksMutex.Lock()
	defer c.blocksMutex.Unlock()
	b, ok := c.Blocks[datastore.ToKey(hash)]
	if ok {
		return b, nil
	}
	/*
		b = block.Provider().(*block.Block)
		err := b.Read(ctx, datastore.ToKey(hash))
		if err != nil {
			return b, nil
		}*/
	return nil, common.NewError(datastore.EntityNotFound, fmt.Sprintf("Block with hash (%v) not found", hash))
}

/*DeleteBlock - delete a block from the cache */
func (c *Chain) DeleteBlock(ctx context.Context, b *block.Block) {
	c.blocksMutex.Lock()
	defer c.blocksMutex.Unlock()
	if _, ok := c.Blocks[b.Hash]; !ok {
		return
	}
	delete(c.Blocks, b.Hash)
}

/*GetRoundBlocks - get the blocks for a given round */
func (c *Chain) GetRoundBlocks(round int64) []*block.Block {
	blocks := make([]*block.Block, 0, 1)
	c.blocksMutex.Lock()
	defer c.blocksMutex.Unlock()
	for _, b := range c.Blocks {
		if b.Round == round {
			blocks = append(blocks, b)
		}
	}
	return blocks
}

/*DeleteBlocksBelowRound - delete all the blocks below this round */
func (c *Chain) DeleteBlocksBelowRound(round int64) {
	c.blocksMutex.Lock()
	defer c.blocksMutex.Unlock()
	ts := common.Now() - 60
	blocks := make([]*block.Block, 0, 1)
	for _, b := range c.Blocks {
		if b.Round < round && b.CreationDate < ts {
			Logger.Debug("found block to delete", zap.Int64("round", round), zap.Int64("block_round", b.Round), zap.Int64("current_round", c.CurrentRound), zap.Int64("lf_round", c.LatestFinalizedBlock.Round))
			blocks = append(blocks, b)
		}
	}
	for _, b := range blocks {
		delete(c.Blocks, b.Hash)
	}

}

/*DeleteBlocks - delete a list of blocks */
func (c *Chain) DeleteBlocks(blocks []*block.Block) {
	c.blocksMutex.Lock()
	defer c.blocksMutex.Unlock()
	for _, b := range blocks {
		delete(c.Blocks, b.Hash)
	}
}

/*ValidateMagicBlock - validate the block for a given round has the right magic block */
func (c *Chain) ValidateMagicBlock(ctx context.Context, b *block.Block) bool {
	//TODO: This needs to take the round number into account and go backwards as needed to validate
	return b.MagicBlockHash == c.CurrentMagicBlock.Hash
}
