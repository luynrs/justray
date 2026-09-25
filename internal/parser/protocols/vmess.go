package protocols

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/luynrs/justray/internal/domain"
)

// v2rayn schema
type vmessLink struct {
	PS            string     `json:"ps"`
	Add           string     `json:"add"`
	Port          flexInt    `json:"port"`
	ID            string     `json:"id"`
	AID           flexInt    `json:"aid"`
	SCY           string     `json:"scy"`
	Net           string     `json:"net"`
	Type          string     `json:"type"`
	Host          string     `json:"host"`
	Path          string     `json:"path"`
	TLS           flexString `json:"tls"`
	SNI           string     `json:"sni"`
	ALPN          flexString `json:"alpn"`
	FP            string     `json:"fp"`
	Insecure      flexString `json:"insecure"`
	AllowInsecure flexString `json:"allowInsecure"`
	Extra         string     `json:"extra"`
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
	if vm.Add == "" || !domain.ValidPort(int(vm.Port)) || vm.ID == "" {
		return domain.Node{}, fmt.Errorf("vmess: missing add/port/id")
	}

	net := strings.ToLower(cmp.Or(vm.Net, "tcp"))
	if net == "splithttp" {
		net = "xhttp"
	}
	if net == "h2" {
		net = "http"
	}
	host0 := strings.TrimSpace(strings.SplitN(vm.Host, ",", 2)[0])
	n := domain.Node{
		Name:     cmp.Or(vm.PS, frag, vm.Add),
		Protocol: domain.VMess,
		Server:   vm.Add,
		Port:     int(vm.Port),
		Auth: domain.Auth{
			UUID:    vm.ID,
			AlterID: int(vm.AID),
			Method:  strings.ToLower(cmp.Or(vm.SCY, "auto")),
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
	if net == "xhttp" {
		n.Transport.Extra = vm.Extra
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
