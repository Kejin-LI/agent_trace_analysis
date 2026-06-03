package vendor_tags

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"

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

func isSidecar() bool {
	return strings.ToLower(os.Getenv("TCE_IS_INJECTED")) == "true"
}

func getIsSidecar() string {
	if isSidecar() {
		return "1"
	}
	return "0"
}

func getPrimaryPSM() string {
	if isSidecar() {
		if psm := os.Getenv("TCE_PRIMARY_PSM_IN_SIDECAR"); psm != "" {
			return psm
		}
		return "-"
	}
	return env.PSM()
}

func GetHost() string {
	if hostname := os.Getenv("HOSTNAME"); hostname != "" {
		if strings.HasPrefix(hostname, "n") ||
			strings.HasPrefix(hostname, "p") ||
			strings.HasPrefix(hostname, "dc") {
			return hostname
		}
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

	dc := env.IDC()
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
	if (ppeFlag == "PPE") || (ppeFlag == "ppe") {
		if "lf" == dc || "hl" == dc || "lq" == dc || "yg" == dc {
			newDC = "ppe"
		}
		if "va" == dc || "maliva" == dc {
			newDC = "ppe-va"

		}
		if "sg" == dc || "sg1" == dc || "alisg" == dc || "sgsaas1larkidc1" == dc || "sgsaas1larkidc2" == dc ||
			"sgsaas1larkidc3" == dc {
			newDC = "ppe-sig"
		}
		if "sg2" == dc {
			newDC = "ppe-sig2"
		}
		if "useast2a" == dc {
			newDC = "ppe-useast2a"
		}
		if "useast5" == dc {
			newDC = "ppe-useast5"
		}
	}
	if "ppe-va" == ppeFlag {
		newDC = "ppe-va"
	}

	if "ppe-sig" == ppeFlag {
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

// exists returns whether the given file or directory exists
func exists(path string) bool {
	_, err := os.Stat(path)

	if os.IsNotExist(err) {
		return false
	}

	return true
}
