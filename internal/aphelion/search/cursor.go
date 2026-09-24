package search

import (
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"time"
)

// Cursor is stepped only by the UI owner against one display generation. The
// caller discards it on a revision change; no worker reads mutable instances.
type Cursor struct {
	source                        *dmmap.Dmm
	ids                           []uint64
	groups                        map[uint64][]*dmminstance.Instance
	tile, instance, group, member int
	result                        []*dmminstance.Instance
	done                          bool
}

func NewCursor(source *dmmap.Dmm, ids []uint64) *Cursor {
	c := &Cursor{source: source, ids: append([]uint64(nil), ids...), groups: make(map[uint64][]*dmminstance.Instance, len(ids))}
	for _, id := range ids {
		c.groups[id] = nil
	}
	c.done = source == nil || len(ids) == 0
	return c
}
func (c *Cursor) Step(limit int, deadline time.Time) bool {
	for count := 0; count < limit && !c.done; count++ {
		if count%64 == 0 && time.Now().After(deadline) {
			break
		}
		if c.tile < len(c.source.Tiles) {
			instances := c.source.Tiles[c.tile].Instances()
			if c.instance == len(instances) {
				c.tile++
				c.instance = 0
				continue
			}
			instance := instances[c.instance]
			c.instance++
			id := instance.Prefab().Id()
			if group, ok := c.groups[id]; ok {
				c.groups[id] = append(group, instance)
			}
		} else if c.group < len(c.ids) {
			group := c.groups[c.ids[c.group]]
			if c.member == len(group) {
				delete(c.groups, c.ids[c.group])
				c.group++
				c.member = 0
				continue
			}
			c.result = append(c.result, group[c.member])
			c.member++
		} else {
			c.done = true
			c.source = nil
			c.groups = nil
		}
	}
	return c.done
}
func (c *Cursor) Result() []*dmminstance.Instance {
	if !c.done {
		return nil
	}
	return c.result
}
