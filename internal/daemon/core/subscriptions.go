package core

import (
	"context"
	"fmt"
	"slices"

	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/ipc"
	"github.com/luynrs/justray/internal/parser"
	"github.com/luynrs/justray/internal/parser/protocols"
)

func (c *Core) AddSubscription(ctx context.Context, rawURL string) (ipc.Subscription, error) {
	sub, err := c.subs.PrepareAdd(ctx, rawURL)
	if err != nil {
		return ipc.Subscription{}, err
	}
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return ipc.Subscription{}, err
	}
	next := c.current()
	sub.Nodes = assignNodeIDs(sub.Nodes, nil)
	if parser.IsLink(sub.URL) {
		i := slices.IndexFunc(next.Subscriptions, func(s store.Subscription) bool { return s.ID == "default" })
		if i < 0 {
			i = len(next.Subscriptions)
			next.Subscriptions = append(next.Subscriptions, store.Subscription{ID: "default", Name: "Default"})
		}
		next.Subscriptions[i].Nodes = append(next.Subscriptions[i].Nodes, sub.Nodes...)
		sub = next.Subscriptions[i]
	} else {
		next.Subscriptions = append(next.Subscriptions, sub)
	}
	if err := c.commit(next); err != nil {
		return ipc.Subscription{}, err
	}
	c.publish()
	return subView(sub, false), nil
}

func assignNodeIDs(nodes, previous []domain.Node) []domain.Node {
	type nodeMatch struct{ config, name string }
	byName := make(map[nodeMatch][]string, len(previous))
	byConfig := make(map[string][]string, len(previous))
	for _, node := range previous {
		key := nodeMatch{protocols.NodeKey(node), node.Name}
		byName[key] = append(byName[key], node.ID)
		byConfig[key.config] = append(byConfig[key.config], node.ID)
	}
	used := make(map[string]bool, len(previous))
	for i := range nodes {
		key := nodeMatch{protocols.NodeKey(nodes[i]), nodes[i].Name}
		if ids := byName[key]; len(ids) > 0 {
			nodes[i].ID = ids[0]
			byName[key] = ids[1:]
			used[nodes[i].ID] = true
		}
	}
	for i := range nodes {
		if nodes[i].ID != "" {
			continue
		}
		key := protocols.NodeKey(nodes[i])
		ids := byConfig[key]
		for len(ids) > 0 && used[ids[0]] {
			ids = ids[1:]
		}
		if len(ids) > 0 {
			nodes[i].ID = ids[0]
			ids = ids[1:]
		} else {
			nodes[i].ID = store.NewNodeID()
		}
		byConfig[key] = ids
		used[nodes[i].ID] = true
	}
	return nodes
}

func (c *Core) RemoveSubscription(id string) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	next := c.current()
	count := len(next.Subscriptions)
	next.Subscriptions = slices.DeleteFunc(next.Subscriptions, func(s store.Subscription) bool {
		return s.ID == id
	})
	if len(next.Subscriptions) == count {
		return fmt.Errorf("subscription %q not found", id)
	}
	if next.Active.SubscriptionID == id {
		next.Active = domain.NodeRef{}
	}
	if next.Last.SubscriptionID == id {
		next.Last = domain.NodeRef{}
	}
	next.Collapsed = slices.DeleteFunc(next.Collapsed, func(s string) bool {
		return s == id
	})
	if err := c.commit(next); err != nil {
		return err
	}
	var cleanupErr error
	if c.conn.Status().NodeRef.SubscriptionID == id {
		cleanupErr = c.conn.Disconnect(context.Background())
	}
	c.publish()
	return cleanupErr
}

func (c *Core) RemoveNode(ref domain.NodeRef) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	next := c.current()
	i := slices.IndexFunc(next.Subscriptions, func(sub store.Subscription) bool {
		return (ref.SubscriptionID == "" || sub.ID == ref.SubscriptionID) && sub.URL == "" &&
			slices.ContainsFunc(sub.Nodes, func(node domain.Node) bool { return node.ID == ref.NodeID })
	})
	if i < 0 {
		return fmt.Errorf("node %q not found", ref.NodeID)
	}
	ref.SubscriptionID = next.Subscriptions[i].ID
	next.Subscriptions[i].Nodes = slices.DeleteFunc(slices.Clone(next.Subscriptions[i].Nodes), func(node domain.Node) bool { return node.ID == ref.NodeID })
	if next.Active == ref {
		next.Active = domain.NodeRef{}
	}
	if next.Last == ref {
		next.Last = domain.NodeRef{}
	}
	if err := c.commit(next); err != nil {
		return err
	}
	var cleanupErr error
	if c.conn.Status().NodeRef == ref {
		cleanupErr = c.conn.Disconnect(context.Background())
	}
	c.publish()
	return cleanupErr
}

func (c *Core) MoveSubscription(id string, dir int) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	next := c.current()
	i := slices.IndexFunc(next.Subscriptions, func(sub store.Subscription) bool { return sub.ID == id })
	if i < 0 {
		return fmt.Errorf("subscription %q not found", id)
	}
	j := i + dir
	if j < 0 || j >= len(next.Subscriptions) {
		return nil
	}
	next.Subscriptions[i], next.Subscriptions[j] = next.Subscriptions[j], next.Subscriptions[i]
	if err := c.commit(next); err != nil {
		return err
	}
	c.publish()
	return nil
}
