package transfer

import (
	"net"
	"net/netip"
	"strconv"
)

// Connection 直连地址由接收端浏览器上报，仅作为诊断信息，不参与鉴权或传输判断。
type Connection struct {
	Local  Endpoint `json:"local"`
	Remote Endpoint `json:"remote"`
}

type Endpoint struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
}

func EndpointLabel(endpoint Endpoint) string {
	if endpoint.Port < 1 || endpoint.Port > 65535 {
		return "不可用"
	}
	address, err := netip.ParseAddr(endpoint.Address)
	if err != nil {
		return "浏览器未公开（端口 " + strconv.Itoa(endpoint.Port) + "）"
	}
	return net.JoinHostPort(address.Unmap().String(), strconv.Itoa(endpoint.Port))
}
