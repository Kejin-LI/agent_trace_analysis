package net2

import (
	"bytes"
	"errors"
	"net"
)

var (
	v4InV6Prefix          = []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff}
	localIP               net.IP
	localIPStr            string
	localIPList           []net.IP
	localIPStrList        []string
	privateNets           []net.IPNet
	errNotSupportFamily   = errors.New("family not supported")
	errCannotGetDefaultIP = errors.New("cannot get default IP")
)

type AddressFamily uint

const (
	UnknownIPAddr               = "-"
	FamilyIPv4    AddressFamily = 4
	FamilyIPv6    AddressFamily = 6
)

func init() {
	// 1. 2605:340:CDB1:100::/56 is private IPv6 Address for bytedance
	// 2. 33.0.0.0/8 is DOD(United States Department of Defense) public IPv4 Address,
	// 	  bytedance use this network in some datacenter to solve address exhaustion
	for _, s := range []string{
		"10.0.0.0/8", "fc00::/7", "fdbd::/16", "2605:340:CD00::/40",
		"172.16.0.0/12", "192.168.0.0/16", "33.0.0.0/8",
	} {
		_, n, _ := net.ParseCIDR(s)
		privateNets = append(privateNets, *n)
	}

	// get all network interfaces
	netIfaces, err := net.Interfaces()
	if err != nil {
		return
	}

	// get all private ip list from non-loopback active net interfaces
	for _, netIface := range netIfaces {
		if netIface.Flags&net.FlagLoopback != 0 {
			// skip all Loopback interface
			continue
		}
		if netIface.Flags&net.FlagUp == 0 {
			// skip interface not UP
			continue
		}
		addrs, err := netIface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ipnet.IP.IsLoopback() {
				continue
			}
			ip := ipnet.IP
			if IsPrivateIP(ip) {
				localIPList = append(localIPList, ip)
				localIPStrList = append(localIPStrList, ip.String())
			}
		}
	}

	// set local ip by priority of privateNets
	for _, pnet := range privateNets {
		for _, ip := range localIPList {
			if pnet.Contains(ip) {
				localIP = ip
				localIPStr = ip.String()
				return
			}
		}
	}
}

func GetLocalIP() net.IP {
	return localIP
}

func GetLocalIPStr() string {
	return localIPStr
}

// GetLocalIp return local IP address
//
// Deprecated: use GetLocalIP or GetLocalIPStr instead.
func GetLocalIp() string {
	if localIPStr == "" {
		return UnknownIPAddr
	}
	return localIPStr
}

func GetAllLocalIP() []net.IP {
	return localIPList
}

func GetAllLocalIPStr() []string {
	return localIPStrList
}

func IsPrivateIP(ip net.IP) bool {
	for _, n := range privateNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func IsV4IP(ip net.IP) bool {
	if len(ip) == net.IPv4len || len(ip) == net.IPv6len && bytes.Equal(ip[:12], v4InV6Prefix) {
		return true
	}
	return false
}

func IsV6IP(ip net.IP) bool {
	if len(ip) == net.IPv6len && !bytes.Equal(ip[:12], v4InV6Prefix) {
		return true
	}
	return false
}

// GetDefaultIPAddress will get the default address to connect to remote by default router
// When there is a route on the machine that affects access to the test server (8.8.8.8/10.8.8.8), unexpected results will be obtained
// This test does not actually access the target server, nor does it require that the target server can be connected。
// !!! Do not frequently execute this method to get IP
// 本方法用户获取机器在默认路由下的本地 IP, 在机器上存在可以影响测试服务器的路由的时候，会导致一些非预期的结果
// 测试不需要目标服务器可以真实的访问到. !!! 不要频繁执行本方法获取IP
func GetDefaultIPAddress(addressFamilies []AddressFamily) (net.IP, error) {
	var (
		v4Targets = []string{"8.8.8.8:53", "10.8.8.8:53"}
		v6Targets = []string{"[240C::6644]:53", "[fdbd:dc00::10:8:8:8]:53"}
	)
	for _, family := range addressFamilies {
		var network string
		var targets []string
		switch family {
		case FamilyIPv4:
			network = "udp4"
			targets = v4Targets
		case FamilyIPv6:
			network = "udp6"
			targets = v6Targets
		default:
			return nil, errNotSupportFamily
		}

		for _, target := range targets {
			ip, err := getLocalIPAddressByDial(network, target)
			if err == nil {
				return ip, nil
			}
		}
	}
	return nil, errCannotGetDefaultIP

}

func getLocalIPAddressByDial(network string, testAddr string) (net.IP, error) {

	dnsUDPAddr, err := net.ResolveUDPAddr(network, testAddr)
	if err != nil {
		return nil, err
	}
	c, err := net.DialUDP(network, nil, dnsUDPAddr)
	if err != nil {
		return nil, err
	}
	localIP := c.LocalAddr().(*net.UDPAddr).IP
	_ = c.Close()
	return localIP, nil
}
