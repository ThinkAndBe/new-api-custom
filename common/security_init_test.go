package common

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func withEnv(t *testing.T, kv map[string]string, fn func()) {
	t.Helper()
	keys := []string{"TRUSTED_PROXIES"}
	saved := map[string]string{}
	for _, k := range keys {
		saved[k] = os.Getenv(k)
		os.Unsetenv(k)
	}
	for k, v := range kv {
		os.Setenv(k, v)
	}
	t.Cleanup(func() {
		for k := range kv {
			os.Unsetenv(k)
		}
		for k, v := range saved {
			if v != "" {
				os.Setenv(k, v)
			}
		}
	})
	fn()
}

func TestInitTrustedProxiesDefault(t *testing.T) {
	withEnv(t, nil, func() {
		require.NoError(t, InitTrustedProxies())
		require.Equal(t, DefaultTrustedProxies, TrustedProxyCIDRs)
		require.Contains(t, TrustedProxyCIDRs, "172.16.0.0/12")
	})
}

func TestInitTrustedProxiesExplicitList(t *testing.T) {
	withEnv(t, map[string]string{"TRUSTED_PROXIES": "203.0.113.10, 198.51.100.0/24"}, func() {
		require.NoError(t, InitTrustedProxies())
		require.Equal(t, []string{"203.0.113.10", "198.51.100.0/24"}, TrustedProxyCIDRs)
	})
}

func TestInitTrustedProxiesNone(t *testing.T) {
	withEnv(t, map[string]string{"TRUSTED_PROXIES": "none"}, func() {
		require.NoError(t, InitTrustedProxies())
		require.Empty(t, TrustedProxyCIDRs)
	})
}

func TestInitTrustedProxiesInvalid(t *testing.T) {
	withEnv(t, map[string]string{"TRUSTED_PROXIES": "not-an-ip"}, func() {
		require.Error(t, InitTrustedProxies())
	})
	withEnv(t, map[string]string{"TRUSTED_PROXIES": "10.0.0.0/999"}, func() {
		require.Error(t, InitTrustedProxies())
	})
}

func TestInitSessionCookieSecureAutoFollowsTrustedURL(t *testing.T) {
	// 未显式设置 SESSION_COOKIE_SECURE，但配置了可信 HTTPS 入口 -> 自动启用 Secure
	t.Setenv("SESSION_COOKIE_SECURE", "")
	t.Setenv("SESSION_COOKIE_TRUSTED_URL", "https://tokenhub.erke.com")
	require.NoError(t, InitSessionCookieSettings())
	require.True(t, SessionCookieSecure)
	require.Equal(t, []string{"https://tokenhub.erke.com"}, SessionCookieTrustedURLs)

	// 两者都未设置 -> 保持关闭（纯 HTTP 内网兼容）
	t.Setenv("SESSION_COOKIE_TRUSTED_URL", "")
	require.NoError(t, InitSessionCookieSettings())
	require.False(t, SessionCookieSecure)

	// 显式 false + 设置 TRUSTED_URL -> 报错（保留原校验语义）
	t.Setenv("SESSION_COOKIE_SECURE", "false")
	t.Setenv("SESSION_COOKIE_TRUSTED_URL", "https://tokenhub.erke.com")
	require.Error(t, InitSessionCookieSettings())
}
