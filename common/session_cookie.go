package common

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

func InitSessionCookieSettings() error {
	secureRaw := strings.TrimSpace(os.Getenv("SESSION_COOKIE_SECURE"))
	trustedURLsRaw := strings.TrimSpace(os.Getenv("SESSION_COOKIE_TRUSTED_URL"))

	SessionCookieSecure = false
	SessionCookieTrustedURLs = nil

	if !strings.EqualFold(secureRaw, "true") && !strings.EqualFold(secureRaw, "false") && secureRaw != "" {
		return fmt.Errorf("SESSION_COOKIE_SECURE must be true or false")
	}

	if secureRaw == "" {
		// 未显式配置：设置了可信 HTTPS 入口即自动启用 Secure cookie（部署在
		// HTTPS 反代之后的安全默认值），否则保持关闭以兼容纯 HTTP 内网部署。
		if trustedURLsRaw == "" {
			return nil
		}
	} else if strings.EqualFold(secureRaw, "false") {
		if trustedURLsRaw != "" {
			return fmt.Errorf("SESSION_COOKIE_TRUSTED_URL requires SESSION_COOKIE_SECURE=true")
		}
		return nil
	}

	if trustedURLsRaw == "" {
		return fmt.Errorf("SESSION_COOKIE_SECURE=true requires SESSION_COOKIE_TRUSTED_URL")
	}

	trustedURLs := strings.Split(trustedURLsRaw, ",")
	for _, trustedURL := range trustedURLs {
		trustedURL = strings.TrimSpace(trustedURL)
		if trustedURL == "" {
			return fmt.Errorf("SESSION_COOKIE_TRUSTED_URL contains an empty URL")
		}
		parsedURL, err := url.Parse(trustedURL)
		if err != nil {
			return fmt.Errorf("invalid SESSION_COOKIE_TRUSTED_URL: %w", err)
		}
		if parsedURL.Scheme != "https" || parsedURL.Host == "" {
			return fmt.Errorf("SESSION_COOKIE_TRUSTED_URL must contain only https URLs with hosts")
		}
		SessionCookieTrustedURLs = append(SessionCookieTrustedURLs, trustedURL)
	}

	SessionCookieSecure = true
	return nil
}
