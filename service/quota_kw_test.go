package service

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/types"
)

// 火山 CodingPlan 订阅过期（400）必须被判为额度耗尽 → 强制禁用渠道
func TestSubscriptionExpiredIsQuotaExhausted(t *testing.T) {
	msg := "Your account (2100562996) does not have a valid CodingPlan subscription, or your subscription has expired. Please visit https://***.com/*** to review your subscription status"
	err := types.NewErrorWithStatusCode(newPlainErr(msg), types.ErrorCodeDoRequestFailed, http.StatusBadRequest)
	if !IsQuotaExhaustedError(err) {
		t.Fatal("订阅过期错误应命中额度耗尽关键词（会强制禁用渠道）")
	}
	if !isQuotaExhaustedReason(msg) {
		t.Fatal("DisableChannel 的关键词判定也应命中")
	}
}

func newPlainErr(msg string) error { return &plainErr{msg} }

type plainErr struct{ s string }

func (e *plainErr) Error() string { return e.s }
