package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/ipc"
)

func (d Disk) Migrate() error {
	files := []struct {
		path          string
		before, after []byte
	}{
		{path: ipc.State(d.Dir)},
		{path: ipc.Config(d.Dir)},
	}
	changed := false
	for index := range files {
		file := &files[index]
		data, err := os.ReadFile(file.path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", file.path, err)
		}
		file.before = data
		var document map[string]json.RawMessage
		if err := json.Unmarshal(data, &document); err != nil {
			return fmt.Errorf("read %s: %w", file.path, err)
		}
		migrated := false
		if index == 0 {
			for _, name := range []string{"active", "last"} {
				value, companion := document[name], document[name+"_subscription"]
				var nodeID, subscriptionID string
				if value == nil && companion == nil {
					continue
				}
				if value != nil {
					if err := json.Unmarshal(value, &nodeID); err != nil {
						if companion != nil {
							return fmt.Errorf("%s: conflicting %s reference formats", file.path, name)
						}
						continue
					}
					if bytes.Equal(bytes.TrimSpace(value), []byte("null")) && companion == nil {
						continue
					}
				}
				if companion != nil {
					if err := json.Unmarshal(companion, &subscriptionID); err != nil {
						return fmt.Errorf("%s: %s_subscription: %w", file.path, name, err)
					}
				}
				if nodeID != "" && subscriptionID == "" && document["subscriptions"] != nil {
					var subscriptions []Subscription
					if err := json.Unmarshal(document["subscriptions"], &subscriptions); err != nil {
						return fmt.Errorf("%s: subscriptions: %w", file.path, err)
					}
					for _, subscription := range subscriptions {
						for _, node := range subscription.Nodes {
							if node.ID == nodeID {
								if subscriptionID != "" && subscriptionID != subscription.ID {
									return fmt.Errorf("%s: ambiguous %s node reference", file.path, name)
								}
								subscriptionID = subscription.ID
							}
						}
					}
				}
				ref, err := json.Marshal(domain.NodeRef{SubscriptionID: subscriptionID, NodeID: nodeID})
				if err != nil {
					return err
				}
				document[name] = ref
				delete(document, name+"_subscription")
				migrated = true
			}
		} else if document["connection"] != nil {
			var connection map[string]json.RawMessage
			if err := json.Unmarshal(document["connection"], &connection); err != nil {
				return fmt.Errorf("%s: connection: %w", file.path, err)
			}
			var version string
			if value := connection["ip_version"]; value != nil {
				if err := json.Unmarshal(value, &version); err != nil {
					return fmt.Errorf("%s: ip_version: %w", file.path, err)
				}
			}
			if version == "auto" {
				connection["ip_version"] = json.RawMessage(`"mixed"`)
				encoded, err := json.Marshal(connection)
				if err != nil {
					return err
				}
				document["connection"] = encoded
				migrated = true
			}
		}
		if migrated {
			file.after, err = json.MarshalIndent(document, "", "  ")
			if err != nil {
				return err
			}
			file.after = append(file.after, '\n')
			changed = true
		}
	}
	if !changed {
		return nil
	}
	state := PersistentState{Settings: domain.Settings{General: domain.General{RefreshEvery: domain.DefaultRefresh}}}
	for index, file := range files {
		data := file.after
		if data == nil {
			data = file.before
		}
		if data == nil {
			continue
		}
		var err error
		if index == 0 {
			err = json.Unmarshal(data, &state)
		} else {
			err = json.Unmarshal(data, &state.Settings)
		}
		if err != nil {
			return fmt.Errorf("validate %s: %w", file.path, err)
		}
	}
	state.Settings.Autostart = "off"
	if _, err := state.Settings.Normalize(); err != nil {
		return fmt.Errorf("validate %s: %w", ipc.Config(d.Dir), err)
	}
	for _, file := range files {
		if file.after != nil {
			if err := write(file.path, file.after); err != nil {
				return fmt.Errorf("migrate %s: %w", file.path, err)
			}
		}
	}
	return nil
}
