package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gin-gonic/gin"
)

func newHeaderTestContext(t *testing.T, headers map[string]string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	c.Request = req
	return c, recorder
}

func TestSecurityHeadersBasic(t *testing.T) {
	c, recorder := newHeaderTestContext(t, nil)
	SecurityHeaders()(c)
	c.Next()

	require.Equal(t, "nosniff", recorder.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "DENY", recorder.Header().Get("X-Frame-Options"))
	require.Equal(t, "strict-origin-when-cross-origin", recorder.Header().Get("Referrer-Policy"))
	// 纯 HTTP 直连不下发 HSTS
	require.Empty(t, recorder.Header().Get("Strict-Transport-Security"))
}

func TestSecurityHeadersHSTSBehindHTTPSProxy(t *testing.T) {
	c, recorder := newHeaderTestContext(t, map[string]string{"X-Forwarded-Proto": "https"})
	SecurityHeaders()(c)
	c.Next()

	require.Equal(t, "max-age=31536000", recorder.Header().Get("Strict-Transport-Security"))
}
