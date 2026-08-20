package common

import (
	"fmt"
	"net"
	"os"
	"strings"
)

// TrustedProxyCIDRs 是 gin SetTrustedProxies 使用的可信代理地址列表。
// 默认为私有网段（Docker/内网部署的反向代理都在这些网段内），
// 公网直连或使用公网负载均衡时需通过 TRUSTED_PROXIES 环境变量显式指定。
var TrustedProxyCIDRs []string

// DefaultTrustedProxies 内网私有网段 + Docker 网桥，覆盖常见自托管部署拓扑。
// 需要信任公网代理（如云 LB）时设置 TRUSTED_PROXIES=203.0.113.10,198.51.100.0/24
var DefaultTrustedProxies = []string{
	"127.0.0.0/8",    // localhost（本机 nginx）
	"10.0.0.0/8",     // 内网 A 类
	"172.16.0.0/12",  // Docker 默认网桥
	"192.168.0.0/16", // 内网 C 类
}

// InitTrustedProxies 解析 TRUSTED_PROXIES 环境变量。
//   - 未设置：使用 DefaultTrustedProxies（私有网段）
//   - 设置为 "none"：不信任任何代理（客户端直连公网部署）
//   - 设置为 CIDR/IP 列表：仅信任指定地址
//
// 所有条目必须是合法 IP 或 CIDR，否则启动失败（避免静默忽略拼写错误导致限流被绕过）。
func InitTrustedProxies() error {
	raw := strings.TrimSpace(os.Getenv("TRUSTED_PROXIES"))
	if raw == "" {
		TrustedProxyCIDRs = DefaultTrustedProxies
		return nil
	}
	if strings.EqualFold(raw, "none") {
		TrustedProxyCIDRs = nil
		return nil
	}

	TrustedProxyCIDRs = nil
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			return fmt.Errorf("TRUSTED_PROXIES contains an empty entry")
		}
		if strings.Contains(item, "/") {
			if _, _, err := net.ParseCIDR(item); err != nil {
				return fmt.Errorf("invalid CIDR in TRUSTED_PROXIES: %s", item)
			}
		} else if net.ParseIP(item) == nil {
			return fmt.Errorf("invalid IP in TRUSTED_PROXIES: %s", item)
		}
		TrustedProxyCIDRs = append(TrustedProxyCIDRs, item)
	}
	return nil
}
