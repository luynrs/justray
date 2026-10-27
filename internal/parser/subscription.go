package parser

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/luynrs/justray/internal/domain"
)

var errUnsupported = errors.New("unsupported protocol")

type parseResult struct {
	nodes   []domain.Node
	skipped map[string]int
}

func (result *parseResult) add(node domain.Node, err error) {
	if err == nil {
		err = validateNode(node)
	}
	if err == nil {
		result.nodes = append(result.nodes, node)
		return
	}
	reason := strings.ReplaceAll(err.Error(), "\"", "")
	if errors.Is(err, errUnsupported) {
		reason = "unsupported"
	}
	if result.skipped == nil {
		result.skipped = make(map[string]int)
	}
	result.skipped[reason]++
}

func (result *parseResult) finish() ([]domain.Node, string, error) {
	var reasons []string
	var total int
	for reason, count := range result.skipped {
		total += count
		reasons = append(reasons, fmt.Sprintf("%s: %d", reason, count))
	}
	slices.Sort(reasons)
	var warning string
	if total > 0 {
		warning = fmt.Sprintf("skipped nodes: %d (%s)", total, strings.Join(reasons, "; "))
	}
	if len(result.nodes) == 0 {
		if warning != "" {
			return nil, "", fmt.Errorf("no supported nodes; %s", warning)
		}
		return nil, "", errors.New("no supported nodes in subscription")
	}
	return result.nodes, warning, nil
}

func ParseSubscription(raw []byte) ([]domain.Node, string, error) {
	body := bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(raw), []byte("\xef\xbb\xbf")))
	if !bytes.ContainsAny(body, ":{}[]") {
		if decoded, err := decodeBase64(string(body)); err == nil {
			body = bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(decoded), []byte("\xef\xbb\xbf")))
		}
	}
	var result parseResult
	jsonLike := len(body) > 0 && (body[0] == '{' || body[0] == '[')
	if jsonLike && json.Valid(body) {
		if err := result.readJSON(body); err != nil {
			return nil, "", err
		}
		return result.finish()
	}
	var document struct {
		Proxies []yaml.Node `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(body, &document); err == nil && document.Proxies != nil {
		result.readClash(document.Proxies)
		return result.finish()
	} else if err != nil {
		for line := range strings.Lines(string(body)) {
			if strings.HasPrefix(strings.TrimSpace(line), "proxies:") {
				return nil, "", errors.New("clash: invalid YAML")
			}
		}
	}
	if jsonLike {
		return nil, "", errors.New("invalid JSON subscription")
	}
	for line := range strings.Lines(string(body)) {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || strings.HasPrefix(line, "//") {
			continue
		}
		if !IsLink(line) {
			scheme, _, found := strings.Cut(line, "://")
			if found && !strings.EqualFold(scheme, "http") && !strings.EqualFold(scheme, "https") {
				result.add(domain.Node{}, errUnsupported)
			}
			continue
		}
		result.add(parseURI(line))
	}
	return result.finish()
}

func (result *parseResult) readJSON(body []byte) error {
	documents := []json.RawMessage{body}
	if body[0] == '[' {
		if err := json.Unmarshal(body, &documents); err != nil {
			return errors.New("invalid JSON subscription")
		}
	}
	var pending []json.RawMessage
	for _, raw := range documents {
		var document struct {
			Type      json.RawMessage `json:"type"`
			Protocol  json.RawMessage `json:"protocol"`
			Outbounds json.RawMessage `json:"outbounds"`
			Endpoints json.RawMessage `json:"endpoints"`
			Proxies   json.RawMessage `json:"proxies"`
			Remarks   json.RawMessage `json:"remarks"`
		}
		if json.Unmarshal(raw, &document) != nil || document.Type != nil || document.Protocol != nil ||
			document.Outbounds == nil && document.Endpoints == nil && document.Proxies == nil {
			pending = append(pending, raw)
			continue
		}
		if err := result.readJSONOutbounds(pending, nil); err != nil {
			return err
		}
		pending = nil
		var entries []json.RawMessage
		for _, list := range []json.RawMessage{document.Outbounds, document.Endpoints} {
			if list == nil {
				continue
			}
			var outbounds []json.RawMessage
			if json.Unmarshal(list, &outbounds) != nil {
				return errors.New("invalid JSON outbound list")
			}
			entries = append(entries, outbounds...)
		}
		if err := result.readJSONOutbounds(entries, document.Remarks); err != nil {
			return err
		}
		if document.Proxies != nil {
			var proxies []yaml.Node
			if yaml.Unmarshal(document.Proxies, &proxies) != nil {
				return errors.New("clash: invalid proxy list")
			}
			result.readClash(proxies)
		}
	}
	return result.readJSONOutbounds(pending, nil)
}

func (result *parseResult) readJSONOutbounds(entries []json.RawMessage, remarks json.RawMessage) error {
	type record struct {
		singbox *singboxOutbound
		xray    *xrayOutbound
	}
	outbounds := make([]record, 0, len(entries))
	shadowTLS := make(map[string]singboxOutbound)
	detoured := make(map[string]bool)
	xrayCount := 0
	for _, raw := range entries {
		var kind struct {
			Type     json.RawMessage `json:"type"`
			Protocol json.RawMessage `json:"protocol"`
		}
		if json.Unmarshal(raw, &kind) != nil || kind.Type == nil && kind.Protocol == nil {
			result.add(domain.Node{}, errors.New("invalid outbound fields"))
			continue
		}
		var entry record
		if kind.Type != nil {
			var outbound singboxOutbound
			if json.Unmarshal(raw, &outbound) != nil || outbound.Type == "" {
				result.add(domain.Node{}, errors.New("sing-box: invalid outbound fields"))
				continue
			}
			switch strings.ToLower(outbound.Type) {
			case "direct", "block", "dns", "selector", "urltest":
				continue
			case "shadowtls":
				if outbound.Tag != "" {
					shadowTLS[outbound.Tag] = outbound
				}
			case "shadowsocks":
				if outbound.Detour != "" {
					detoured[outbound.Detour] = true
				}
			}
			entry.singbox = &outbound
		} else {
			var outbound xrayOutbound
			if json.Unmarshal(raw, &outbound) != nil || outbound.Protocol == "" {
				result.add(domain.Node{}, errors.New("xray: invalid outbound fields"))
				continue
			}
			switch strings.ToLower(outbound.Protocol) {
			case "freedom", "blackhole", "dns", "loopback":
				continue
			case "vless", "vmess", "trojan", "shadowsocks", "ss":
				xrayCount++
			default:
				result.add(domain.Node{}, errUnsupported)
				continue
			}
			entry.xray = &outbound
		}
		outbounds = append(outbounds, entry)
	}
	var name string
	if xrayCount > 0 && len(remarks) > 0 && json.Unmarshal(remarks, &name) != nil {
		return errors.New("xray: invalid remarks")
	}
	for _, entry := range outbounds {
		if entry.singbox != nil {
			outbound := *entry.singbox
			if strings.EqualFold(outbound.Type, "shadowtls") && detoured[outbound.Tag] {
				continue
			}
			result.add(singboxNode(outbound, shadowTLS))
		} else {
			result.readXray(*entry.xray, name, xrayCount)
		}
	}
	return nil
}
