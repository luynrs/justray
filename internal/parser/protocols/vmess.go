package protocols

import (
	"cmp"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/luynrs/justray/internal/domain"
)

// v2rayn schema
type vmessLink struct {
	PS            string          `json:"ps"`
	Add           string          `json:"add"`
	Port          flexInt         `json:"port"`
	ID            string          `json:"id"`
	SCY           string          `json:"scy"`
	Net           string          `json:"net"`
	Type          string          `json:"type"`
	Mode          string          `json:"mode"`
	Host          string          `json:"host"`
	Path          string          `json:"path"`
	TLS           flexString      `json:"tls"`
	SNI           string          `json:"sni"`
	ALPN          flexString      `json:"alpn"`
	FP            string          `json:"fp"`
	Insecure      flexString      `json:"insecure"`
	AllowInsecure flexString      `json:"allowInsecure"`
	Extra         json.RawMessage `json:"extra"`
}

// vmess://<base64 json>
func ParseVMess(uri string) (domain.Node, error) {
	payload := strings.TrimPrefix(uri, "vmess://")
	payload, frag, _ := strings.Cut(payload, "#")
	if u, err := url.QueryUnescape(frag); err == nil {
		frag = u
	}

	data, err := Unbase64(payload)
	if err != nil {
		return domain.Node{}, errors.New("invalid vmess base64")
	}
	var vm vmessLink
	if err := json.Unmarshal(data, &vm); err != nil {
		return domain.Node{}, errors.New("invalid vmess json")
	}
	net := strings.ToLower(cmp.Or(vm.Net, "tcp"))
	host0 := strings.TrimSpace(strings.SplitN(vm.Host, ",", 2)[0])
	n := domain.Node{
		Name:     cmp.Or(vm.PS, frag, vm.Add),
		Protocol: domain.VMess,
		Server:   vm.Add,
		Port:     int(vm.Port),
		Auth: domain.Auth{
			UUID:   vm.ID,
			Method: strings.ToLower(cmp.Or(vm.SCY, "auto")),
		},
		Transport: domain.Transport{
			Network: net,
			Path:    vm.Path,
			Host:    cmp.Or(host0, vm.SNI),
			Mode:    vm.Type,
		},
	}
	if net == "grpc" {
		n.Transport.ServiceName = vm.Path // grpc exports reuse "path" as name
	}
	if net == "xhttp" || net == "splithttp" {
		n.Transport.Network, n.Transport.Host, n.Transport.Mode = "xhttp", host0, vm.Mode
		if n.Transport.Mode == "" && vm.Type != "none" {
			n.Transport.Mode = vm.Type
		}
		if len(vm.Extra) > 0 && vm.Extra[0] == '"' {
			_ = json.Unmarshal(vm.Extra, &n.Transport.Extra)
		} else {
			n.Transport.Extra = string(vm.Extra)
		}
	}
	tlsStr := strings.ToLower(string(vm.TLS))
	if tlsStr == "tls" || tlsStr == "reality" || tlsStr == "xtls" || truthy(tlsStr) {
		fp, insecure := cleanFingerprint(vm.FP, truthy(string(vm.Insecure)) || truthy(string(vm.AllowInsecure)))
		n.TLS = &domain.TLS{
			SNI:         cmp.Or(vm.SNI, host0, vm.Add),
			ALPN:        splitComma(string(vm.ALPN)),
			Fingerprint: fp,
			Insecure:    insecure,
		}
	}
	return n, nil
}
