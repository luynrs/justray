package parser

import (
	"cmp"
	"encoding/json"
	"errors"
	"net/netip"

	"github.com/sagernet/sing-box/option"
	SJSON "github.com/sagernet/sing/common/json"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine/outbound"
)

// The envelope only groups subscription entries. sing-box decodes and executes
// their complete options, rather than a second protocol schema.
type singboxOutbound struct {
	Type   string `json:"type"`
	Tag    string `json:"tag"`
	Detour string `json:"detour"`
	raw    json.RawMessage
}

func singboxNode(entry singboxOutbound, companions map[string]singboxOutbound) (domain.Node, error) {
	if entry.Type == "wireguard" {
		return singboxWireGuard(entry)
	}
	var options option.Outbound
	err := SJSON.UnmarshalContext(outbound.Context, entry.raw, &options)
	if err != nil {
		return domain.Node{}, err
	}
	node := domain.Node{Name: entry.Tag, Protocol: domain.Proto(options.Type)}
	if server, ok := options.Options.(option.ServerOptionsWrapper); ok {
		address := server.TakeServerOptions()
		node.Server, node.Port = address.Server, int(address.ServerPort)
	}
	node.Name = cmp.Or(node.Name, node.Server)
	if err := nativeMetadata(&node, options.Options); err != nil {
		return domain.Node{}, err
	}
	options.Tag = "node"
	if entry.Detour != "" {
		companion, ok := companions[entry.Detour]
		if !ok || options.Type != "shadowsocks" {
			return domain.Node{}, errors.New("unsupported native outbound detour")
		}
		var tls option.Outbound
		err := SJSON.UnmarshalContext(outbound.Context, companion.raw, &tls)
		if err != nil {
			return domain.Node{}, err
		}
		if companion.Detour != "" {
			return domain.Node{}, errors.New("unsupported native outbound detour")
		}
		tls.Tag = "node-stls"
		wrapper := options.Options.(option.DialerOptionsWrapper)
		dialer := wrapper.TakeDialerOptions()
		dialer.Detour = tls.Tag
		wrapper.ReplaceDialerOptions(dialer)
		settings := tls.Options.(*option.ShadowTLSOutboundOptions)
		node.Server, node.Port = settings.Server, int(settings.ServerPort)
		raw, err := SJSON.Marshal(&tls)
		if err != nil {
			return domain.Node{}, err
		}
		node.Native = append(node.Native, raw)
	}
	raw, err := SJSON.Marshal(&options)
	if err != nil {
		return domain.Node{}, err
	}
	node.Native = append([]json.RawMessage{raw}, node.Native...)
	return node, nil
}

// Keep only display metadata and values needed by resource limits.
func nativeMetadata(node *domain.Node, options any) error {
	var transport *option.V2RayTransportOptions
	switch options := options.(type) {
	case *option.VLESSOutboundOptions:
		transport = options.Transport
	case *option.VMessOutboundOptions:
		transport = options.Transport
	case *option.TrojanOutboundOptions:
		transport = options.Transport
	case *option.HysteriaOutboundOptions:
		node.UpMbps, node.DownMbps = options.UpMbps, options.DownMbps
	case *option.Hysteria2OutboundOptions:
		node.UpMbps, node.DownMbps = options.UpMbps, options.DownMbps
	}
	if transport != nil {
		node.Transport.Network = transport.Type
	}
	return outbound.ValidateTransportOptions(transport)
}

func singboxWireGuard(entry singboxOutbound) (domain.Node, error) {
	if entry.Detour != "" {
		return domain.Node{}, errors.New("unsupported native endpoint detour")
	}
	raw, err := legacyWireGuard(entry.raw)
	if err != nil {
		return domain.Node{}, err
	}
	var endpoint option.Endpoint
	err = SJSON.UnmarshalContext(outbound.Context, raw, &endpoint)
	if err != nil {
		return domain.Node{}, err
	}
	settings := endpoint.Options.(*option.WireGuardEndpointOptions)
	if len(settings.Peers) == 0 {
		return domain.Node{}, errors.New("wireguard: missing peer")
	}
	peer := settings.Peers[0]
	node := domain.Node{Name: cmp.Or(entry.Tag, peer.Address), Protocol: domain.WG, Server: peer.Address, Port: int(peer.Port), WireGuard: &domain.WireGuard{
		AmneziaWGOptions: settings.AmneziaWGOptions, PrivateKey: settings.PrivateKey, PeerPublicKey: peer.PublicKey, PreSharedKey: peer.PreSharedKey,
		Reserved: peer.Reserved, MTU: settings.MTU,
	}}
	if peer.PersistentKeepaliveInterval != "" {
		node.WireGuard.PersistentKeepaliveInterval = &peer.PersistentKeepaliveInterval
	}
	for _, address := range settings.Address {
		node.WireGuard.Address = append(node.WireGuard.Address, address.String())
	}
	endpoint.Tag = "node"
	raw, err = SJSON.Marshal(&endpoint)
	node.Native = []json.RawMessage{raw}
	return node, err
}

// Translate the removed WireGuard outbound shape before native decoding.
func legacyWireGuard(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	if fields["local_address"] == nil && fields["peer_public_key"] == nil {
		return raw, nil
	}
	address := fields["local_address"]
	if address == nil {
		address = fields["address"]
	}
	var addresses stringOrSlice
	if err := json.Unmarshal(address, &addresses); err != nil {
		return nil, err
	}
	fields["address"], _ = json.Marshal(normalizeCIDRs(addresses))
	var peers []map[string]json.RawMessage
	if fields["peers"] != nil {
		if err := json.Unmarshal(fields["peers"], &peers); err != nil {
			return nil, err
		}
	} else {
		peers = append(peers, map[string]json.RawMessage{
			"address": fields["server"], "port": fields["server_port"],
			"public_key": fields["peer_public_key"], "pre_shared_key": fields["pre_shared_key"],
		})
	}
	for _, peer := range peers {
		if peer == nil {
			return nil, errors.New("wireguard: invalid peer")
		}
		for source, target := range map[string]string{"server": "address", "server_port": "port"} {
			if peer[target] == nil && peer[source] != nil {
				peer[target] = peer[source]
			}
			delete(peer, source)
		}
		for _, key := range []string{"reserved", "persistent_keepalive_interval"} {
			if peer[key] == nil && fields[key] != nil {
				peer[key] = fields[key]
			}
		}
		if peer["allowed_ips"] == nil {
			peer["allowed_ips"], _ = json.Marshal([]netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")})
		}
	}
	for _, key := range []string{"server", "server_port", "peer_public_key", "pre_shared_key", "reserved", "persistent_keepalive_interval", "local_address"} {
		delete(fields, key)
	}
	fields["peers"], _ = json.Marshal(peers)
	return json.Marshal(fields)
}
