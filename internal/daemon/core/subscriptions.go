package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/ipc"
	"github.com/luynrs/justray/internal/parser"
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
		key := nodeMatch{nodeKey(node), node.Name}
		byName[key] = append(byName[key], node.ID)
		byConfig[key.config] = append(byConfig[key.config], node.ID)
	}
	used := make(map[string]bool, len(previous))
	for i := range nodes {
		key := nodeMatch{nodeKey(nodes[i]), nodes[i].Name}
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
		key := nodeKey(nodes[i])
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

func nodeKey(node domain.Node) string {
	node.ID, node.Name = "", ""
	if node.Transport.Network == "" {
		node.Transport.Network = "tcp"
	}
	if node.Transport.Extra != "" {
		decoder := json.NewDecoder(strings.NewReader(node.Transport.Extra))
		decoder.UseNumber()
		var extra any
		if decoder.Decode(&extra) == nil && decoder.Decode(new(any)) == io.EOF {
			data, _ := json.Marshal(extra)
			node.Transport.Extra = string(data)
		}
	}
	data, _ := json.Marshal(node)
	return string(data)
}

func (c *Core) RemoveSubscription(ctx context.Context, id string) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	next := c.current()
	count := len(next.Subscriptions)
	next.Subscriptions = slices.DeleteFunc(next.Subscriptions, func(s store.Subscription) bool {
		return s.ID == id
	})
	if len(next.Subscriptions) == count {
		return fmt.Errorf("subscription %q not found", id)
	}
	dropConn := c.sanitizeRefs(&next, store.Subscription{ID: id})
	next.Collapsed = slices.DeleteFunc(next.Collapsed, func(s string) bool {
		return s == id
	})
	if err := c.commit(next); err != nil {
		return err
	}
	var cleanupErr error
	if dropConn {
		cleanupErr = c.conn.Disconnect(context.WithoutCancel(ctx))
	}
	c.publish()
	return cleanupErr
}

func (c *Core) RemoveNode(ctx context.Context, ref domain.NodeRef) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	next := c.current()
	i := slices.IndexFunc(next.Subscriptions, func(sub store.Subscription) bool {
		return (ref.SubscriptionID == "" || sub.ID == ref.SubscriptionID) && sub.URL == "" &&
			slices.ContainsFunc(sub.Nodes, func(node domain.Node) bool { return node.ID == ref.NodeID })
	})
	if i < 0 {
		return fmt.Errorf("node %q not found", ref.NodeID)
	}
	next.Subscriptions[i].Nodes = slices.DeleteFunc(slices.Clone(next.Subscriptions[i].Nodes), func(node domain.Node) bool { return node.ID == ref.NodeID })
	dropConn := c.sanitizeRefs(&next, next.Subscriptions[i])
	if err := c.commit(next); err != nil {
		return err
	}
	var cleanupErr error
	if dropConn {
		cleanupErr = c.conn.Disconnect(context.WithoutCancel(ctx))
	}
	c.publish()
	return cleanupErr
}

func (c *Core) MoveSubscription(ctx context.Context, id string, dir int) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
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
