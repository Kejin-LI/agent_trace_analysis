package vendor_tags

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"regexp"
	"strings"

	"code.byted.org/aiops/monitoring-common-go/utils"
	"code.byted.org/gopkg/env"
)

func GetPodIP() string {
	if podIP := os.Getenv("MY_POD_IP"); podIP != "" {
		return podIP
	}

	if podIPV6 := os.Getenv("MY_POD_IPv6"); podIPV6 != "" {
		return podIPV6
	}

	return "-"
}

func GetPhysicalCluster() string {
	if physicalCluster := os.Getenv("TCE_PHYSICAL_CLUSTER"); physicalCluster != "" {
		return physicalCluster
	}
	return "-"
}

func getIPV4() string {
	if ip := env.HostIPV4(); ip != "" {
		return ip
	}
	return "-"
}

func getIPv6() string {
	if ipv6 := env.HostIPV6(); ipv6 != "" {
		return ipv6
	}
	return "-"
}

func getHostEnv() string {
	if hostEnv := os.Getenv("TCE_HOST_ENV"); hostEnv != "" {
		return hostEnv
	}
	return "-"
}

func IsSidecar() bool {
	return strings.ToLower(os.Getenv("TCE_IS_INJECTED")) == "true"
}

func getIsSidecar() string {
	if IsSidecar() {
		return "1"
	}
	return "0"
}

func getServiceInline() string {
	if psm := os.Getenv("SERVICE_INLINE_REAL_PSM"); psm != "" {
		return psm
	}
	return "-"
}

func GetPrimaryPSM() string {
	if IsSidecar() {
		if psm := os.Getenv("TCE_PRIMARY_PSM_IN_SIDECAR"); psm != "" {
			return psm
		}
		return "-"
	}
	return env.PSM()
}

func GetHost() string {
	if hostname := os.Getenv("HOSTNAME"); isBytedHostname(hostname) {
		return hostname
	}

	if hostname, _ := os.Hostname(); isBytedHostname(hostname) {
		return hostname
	}

	// if the hostname is invalid, then convert the ip.
	return HostIPToHost(env.HostIP())
}

// GetDC returns the dc.
// It also detects if it is in the sandbox according to the environment variable.
func GetDC() string {
	if sandbox := os.Getenv("METRICS_SANDBOX"); sandbox != "" {
		return "fake_sandbox"
	}

	// priority: BYTED_INJECT_DC env > metrics_edge_conf > consul_idc

	if injectDC := os.Getenv("BYTED_INJECT_DC"); injectDC != "" {
		return injectDC
	}

	dc := readDCFromMetricsEdgeConf(kMetricsEdgeConfFile)
	if dc == "" {
		dc = env.IDC()
	}

	newDC := overwriteDCForPPE(dc)
	return newDC
}

func HostIPToHost(ip string) string {
	var host string
	if strings.Contains(ip, ".") {
		host = ipV4ToHost(ip)
	} else if strings.Contains(ip, ":") {
		host = ipV6ToHost(ip)
	}
	if len(host) == 0 {
		host = "-"
	}
	return host
}

func ipV4ToHost(ipv4 string) string {
	if strings.HasPrefix(ipv4, "10.") {
		ips := strings.Split(ipv4, ".")
		hostname := fmt.Sprintf("n%s-%03s-%03s", ips[1], ips[2], ips[3])
		return hostname
	}
	if strings.HasPrefix(ipv4, "33.") {
		ips := strings.Split(ipv4, ".")
		hostname := fmt.Sprintf("p%s-%03s-%03s", ips[1], ips[2], ips[3])
		return hostname
	}

	ips := strings.Split(ipv4, ".")
	if len(ips) == 4 {
		return fmt.Sprintf("n%03s-%03s-%03s-%03s", ips[0], ips[1], ips[2], ips[3])
	}
	return ipv4
}

func ipV6ToHost(ipv6 string) string {
	ele := strings.Split(ipv6, ":")
	if (0 == len(ele)) || ("fdbd" != ele[0]) || (5 > len(ele)) {
		return ipv6
	}

	if nonEmptyStrCount(ele) == 5 {
		return ipv6ToHostNameV6Only(ipv6)
	}
	return ipv6ToHostName(ipv6)
}

func ipv6ToHostNameV6Only(ipv6 string) string {
	ele := strings.Split(ipv6, ":")
	return fmt.Sprintf("%s-p%s-t%s-n%03s", ele[1], ele[2], ele[3], ele[len(ele)-1])
}

func ipv6ToHostName(ipv6 string) string {
	sb := strings.Builder{}
	instanceIp := net.ParseIP(ipv6)
	for i := 2; i < net.IPv6len; i += 2 {
		if i == 4 {
			sb.WriteString("p")
		}
		if instanceIp[i] != 0 {
			sb.WriteString(fmt.Sprintf("%x", instanceIp[i]))
			sb.WriteString(fmt.Sprintf("%.02x", instanceIp[i+1]))
		} else {
			sb.WriteString(fmt.Sprintf("%x", instanceIp[i+1]))
		}
		sb.WriteString("-")
	}
	return sb.String()[:sb.Len()-1]
}

func nonEmptyStrCount(ss []string) int {
	count := 0
	for _, v := range ss {
		if len(v) != 0 {
			count++
		}
	}
	return count
}

func inByteOS() bool {
	if psm := os.Getenv("BSERVICE_psm"); psm != "" {
		return true
	}
	return false
}

func getByteOSPsm() string {
	if psm := os.Getenv("BSERVICE_psm"); psm != "" {
		return psm
	}
	return "-"
}

func getByteOSCluster() string {
	if cluster := os.Getenv("BSERVICE_cluster"); cluster != "" {
		return cluster
	}
	return "-"
}

func getByteOSIP() string {
	if ipv4 := os.Getenv("BYTED_HOST_IP"); ipv4 != "" {
		return ipv4
	}
	return "-"
}

func getByteOSIPv6() string {
	if ipv6 := os.Getenv("BYTED_HOST_IPV6"); ipv6 != "" {
		return ipv6
	}
	return "-"
}

func overwriteDCForPPE(dc string) (newDC string) {
	newDC = dc
	ppeFlag := getPPEFlag()
	switch ppeFlag {
	case "PPE", "ppe":
		switch dc {
		case "lf", "hl", "lq", "yg", "lfzg", "hlzg":
			newDC = "ppe"
		case "va", "maliva":
			newDC = "ppe-va"
		case "sg", "sg1", "alisg", "sgsaas1larkidc1", "sgsaas1larkidc2", "sgsaas1larkidc3":
			newDC = "ppe-sig"
		case "sg2":
			newDC = "ppe-sig2"
		case "useast2a":
			newDC = "ppe-useast2a"
		case "useast5":
			newDC = "ppe-useast5"
		default:
			newDC = "ppe-" + dc
		}
	case "ppe-va":
		newDC = "ppe-va"
	case "ppe-sig":
		newDC = "ppe-sig"
	}
	return
}

func getPPEFlag() string {
	// for tce containers
	if strings.Contains(strings.ToLower(getHostEnv()), "ppe") {
		return "ppe"
	}

	if exists(kOsPPEFile) {
		return "ppe"
	}

	if ppeFlag := os.Getenv("TCE_PHYSICAL_CLUSTER"); ppeFlag != "" {
		if strings.Contains(strings.ToLower(ppeFlag), "ppe") {
			return ppeFlag
		}
	}

	ppeFlag := readPPEFlagFromFile(kTceClusterFile)
	if ppeFlag != "" {
		return ppeFlag
	}

	ppeFlag = readPPEFlagFromFile(kTceSubClusterFile)
	if ppeFlag != "" {
		return ppeFlag
	}

	return ""
}

func isBytedHostname(hostname string) bool {
	// n112-028-209-194
	// n148-050-105
	// p23-080-123
	// dc03-pff-500-28c6-641b-f93-8de2
	// dc03-p14-tc2c-n052
	return strings.HasPrefix(hostname, "n") ||
		strings.HasPrefix(hostname, "p") ||
		strings.HasPrefix(hostname, "dc")
}

func readPPEFlagFromFile(filename string) string {
	if exists(filename) {
		f, err := os.OpenFile(filename, os.O_RDONLY, 0666)
		if err == nil {
			buf := bufio.NewReaderSize(f, 4096)
			for {
				line, err := buf.ReadString('\n')
				if err != nil {
					break
				}
				if strings.Contains(strings.ToLower(line), "ppe") {
					return line[:len(line)-1]
				}
			}
		}
	}
	return ""
}

func readDCFromMetricsEdgeConf(file string) string {
	if exists(file) {
		data, err := ioutil.ReadFile(file)
		if err != nil {
			return ""
		}
		str := utils.BytesToStringUnsafe(data)
		reg := regexp.MustCompile(`^EDGE_MACHINE=edge-([^-\s]+)-([^-\s]+)`)
		matches := reg.FindStringSubmatch(str)
		if len(matches) == 3 && matches[2] != "" {
			return matches[2]
		}
		reg = regexp.MustCompile(`^EDGE_MACHINE=external_edge-([^-\s]+)-([^-\s]+)`)
		matches = reg.FindStringSubmatch(str)
		if len(matches) == 3 && matches[2] != "" {
			return matches[2]
		}
		reg = regexp.MustCompile(`^EDGE_MACHINE=internal_edge-([^-\s]+)-([^-\s]+)`)
		matches = reg.FindStringSubmatch(str)
		if len(matches) == 3 && matches[2] != "" {
			return matches[2]
		}
	}
	return ""
}

// exists returns whether the given file or directory exists
func exists(path string) bool {
	_, err := os.Stat(path)

	if os.IsNotExist(err) {
		return false
	}

	return true
}
