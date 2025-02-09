package partitions

import (
	"errors"
	"fmt"

	"0chain.net/chaincore/chain/state"
	"0chain.net/core/datastore"
	"github.com/0chain/common/core/statecache"
)

//go:generate msgp -io=false -tests=false -unexported=true -v

var (
	ErrPartitionItemAlreadyExist = errors.New("item already exists")
)

// item represent the partition item
type item struct {
	ID   string
	Data []byte
}

type partition struct {
	Key     string `json:"-" msg:"-"`
	Loc     int    `json:"loc"`
	Items   []item `json:"items"`
	Changed bool   `json:"-" msg:"-"`
}

func (p *partition) clone() *partition {
	// DEBUG: msgp marshal/unmarshal to get clone of partition
	v, err := p.MarshalMsg(nil)
	if err != nil {
		panic("partiton marshal failed")
	}

	var np partition
	_, err = np.UnmarshalMsg(v)
	if err != nil {
		panic("partition unmarshal failed")
	}
	return &np

	// newPartition := &partition{
	// 	Key:     p.Key,
	// 	Loc:     p.Loc,
	// 	Items:   make([]item, len(p.Items)),
	// 	Changed: p.Changed,
	// }

	// for i, it := range p.Items {
	// 	nit := item{
	// 		ID:   it.ID,
	// 		Data: make([]byte, len(it.Data)),
	// 	}
	// 	copy(nit.Data, it.Data)

	// 	newPartition.Items[i] = nit
	// }

	// return newPartition
}

func (p *partition) Clone() statecache.Value {
	return p.clone()
}

func (p *partition) CopyFrom(v interface{}) bool {
	if ps, ok := v.(*partition); ok {
		np := ps.clone()
		*p = *np
		// p.Key = ps.Key
		// p.Loc = ps.Loc
		// p.Items = make([]item, len(ps.Items))

		// for i, it := range ps.Items {
		// 	nit := item{
		// 		ID:   it.ID,
		// 		Data: make([]byte, len(it.Data)),
		// 	}
		// 	copy(nit.Data, it.Data)

		// 	p.Items[i] = nit
		// }

		// p.Changed = ps.Changed
		return true
	}
	return false
}

func (p *partition) save(state state.StateContextI) error {
	_, err := state.InsertTrieNode(p.Key, p)
	return err
}

func (p *partition) load(state state.StateContextI, key datastore.Key) error {
	err := state.GetTrieNode(key, p)
	if err != nil {
		return fmt.Errorf("load partition failed, key: %s, %v", key, err)
	}

	p.Key = key
	return nil
}

func (p *partition) add(it PartitionItem) error {
	for _, bi := range p.Items {
		if bi.ID == it.GetID() {
			return ErrPartitionItemAlreadyExist
		}
	}

	v, err := it.MarshalMsg(nil)
	if err != nil {
		return err
	}
	p.Items = append(p.Items, item{ID: it.GetID(), Data: v})
	p.Changed = true
	return nil
}

func (p *partition) addRaw(it item) error {
	for _, v := range p.Items {
		if v.ID == it.ID {
			return errors.New("item already exists")
		}
	}

	p.Items = append(p.Items, it)
	p.Changed = true
	return nil
}

func (p *partition) update(it PartitionItem) error {
	for i := 0; i < p.length(); i++ {
		if p.Items[i].ID == it.GetID() {
			v, err := it.MarshalMsg(nil)
			if err != nil {
				return err
			}

			p.Items[i] = item{ID: it.GetID(), Data: v}
			p.Changed = true
			return nil
		}
	}
	return errors.New("item not found")
}

func (p *partition) remove(id string) error {
	if len(p.Items) == 0 {
		return fmt.Errorf("searching empty partition")
	}
	index := p.findIndex(id)
	if index == notFoundIndex {
		return fmt.Errorf("cannot findIndex id %v in partition", id)
	}
	p.Items[index] = p.Items[len(p.Items)-1]
	p.Items = p.Items[:len(p.Items)-1]
	p.Changed = true
	return nil
}

func (p *partition) cutTail() *item {
	if len(p.Items) == 0 {
		return nil
	}

	tail := p.Items[len(p.Items)-1]
	p.Items = p.Items[:len(p.Items)-1]
	p.Changed = true
	return &tail
}

func (p *partition) tail() *item {
	if len(p.Items) == 0 {
		return nil
	}

	tail := p.Items[len(p.Items)-1]
	return &tail
}

func (p *partition) length() int {
	return len(p.Items)
}

func (p *partition) changed() bool {
	return p.Changed
}

func (p *partition) itemRange(start, end int) ([]item, error) {
	if start > end || end > len(p.Items) {
		return nil, fmt.Errorf("invalid index, start:%v, end:%v, len:%v", start, end, len(p.Items))
	}

	vs := make([]item, len(p.Items[start:end]))
	copy(vs, p.Items[start:end])
	return vs, nil
}

func (p *partition) find(id string) (item, int, bool) {
	for i, v := range p.Items {
		if v.ID == id {
			return v, i, true
		}
	}

	return item{}, -1, false
}

func (p *partition) findIndex(id string) int {
	for i, item := range p.Items {
		if item.ID == id {
			return i
		}
	}
	return notFoundIndex
}

//go:generate msgp -io=false -tests=false -unexported=true -v

type location struct {
	Location int
}

func (l *location) Clone() statecache.Value {
	return &location{Location: l.Location}
}

func (l *location) CopyFrom(v interface{}) bool {
	if l2, ok := v.(*location); ok {
		l.Location = l2.Location
		return true
	}
	return false
}
