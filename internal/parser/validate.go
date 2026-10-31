package parser

import (
	"errors"
	"fmt"
	"math"
	"net/netip"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/sagernet/sing-quic/hysteria"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine/outbound"
)

func validateNode(node domain.Node) error {
	if !utf8.ValidString(node.Auth.Password) || !utf8.ValidString(node.Auth.Username) || !utf8.ValidString(node.ObfsPassword) || node.ShadowTLS != nil && !utf8.ValidString(node.ShadowTLS.Password) {
		return errors.New("non-UTF8 credentials are not supported")
	}
	if node.TLS != nil && node.TLS.Insecure {
		return errors.New("insecure TLS is not supported")
	}
	if (node.Protocol == domain.SOCKS || node.Protocol == domain.SS && node.ShadowTLS == nil) && (node.TLS != nil || node.Reality != nil) {
		return fmt.Errorf("%s: TLS is not supported", node.Protocol)
	}
	if node.Reality != nil && node.Reality.PublicKey == "" {
		return errors.New("reality: missing public key")
	}
	if node.Server == "" || !domain.ValidPort(node.Port) {
		return errors.New("missing host or valid port")
	}
	if (node.Protocol == domain.HY1 || node.Protocol == domain.HY2) && (min(node.UpMbps, node.DownMbps) < 0 || max(node.UpMbps, node.DownMbps) > math.MaxInt/hysteria.MbpsToBps) {
		return fmt.Errorf("%s: bandwidth out of range", node.Protocol)
	}
	if node.Protocol == domain.WG {
		wireguard := node.WireGuard
		if wireguard == nil || wireguard.PrivateKey == "" || wireguard.PeerPublicKey == "" || len(wireguard.Address) == 0 {
			return errors.New("wireguard: missing keys or address")
		}
		if len(wireguard.Reserved) != 0 && len(wireguard.Reserved) != 3 {
			return errors.New("wireguard: reserved must be three bytes")
		}
		if wireguard.Jc > math.MaxUint16 || wireguard.Jmin > maxDatagramSize || wireguard.Jmax > maxDatagramSize || uint64(wireguard.Jc)*(uint64(wireguard.Jmax)+8) > 8<<20 {
			return errors.New("amneziawg: junk count or packet size exceeds allocation limits")
		}
		for _, packet := range []string{wireguard.I1, wireguard.I2, wireguard.I3, wireguard.I4, wireguard.I5} {
			if err := validatePacketTemplate(packet); err != nil {
				return err
			}
		}
		if wireguard.PersistentKeepaliveInterval != nil {
			if _, err := wireguard.PersistentKeepaliveInterval.Spec(); err != nil {
				return fmt.Errorf("wireguard: invalid keepalive: %w", err)
			}
		}
		for _, address := range wireguard.Address {
			if _, err := netip.ParsePrefix(address); err != nil {
				return fmt.Errorf("wireguard: invalid address: %w", err)
			}
		}
	}
	_, _, err := outbound.New(node, "node")
	return err
}

const maxDatagramSize = 65507

func validatePacketTemplate(packet string) error {
	if strings.ContainsAny(packet, "\r\n") {
		return errors.New("amneziawg: packet templates must not contain line breaks")
	}
	size := 0
	for {
		_, remaining, found := strings.Cut(packet, "<")
		if !found {
			break
		}
		tag, remaining, found := strings.Cut(remaining, ">")
		if !found {
			break
		}
		packet = remaining
		parts := strings.Fields(tag)
		if len(parts) == 0 {
			continue
		}
		switch parts[0] {
		case "t":
			size += 4
		case "b":
			if len(parts) > 1 {
				size += len(strings.TrimPrefix(parts[1], "0x")) / 2
			}
		case "r", "rc", "rd", "dz":
			if len(parts) > 1 {
				length, err := strconv.Atoi(parts[1])
				if err != nil || length < 0 || length > maxDatagramSize-size {
					return errors.New("amneziawg: packet template exceeds UDP payload limit")
				}
				size += length
			}
		}
		if size > maxDatagramSize {
			return errors.New("amneziawg: packet template exceeds UDP payload limit")
		}
	}
	return nil
}
