package service

import "testing"

// 签名算法验证。采用文档章节 5.3.3 的官方算例（appid/appSecret/code/签名一一对应）；
// 注：附录 7.1 的算例值与三种语言官方代码样例均不一致，属文档过期错误，勿参照。
func TestATrustSSOSign(t *testing.T) {
	got := atrustSSOSign(
		"eab4391d-f8e4-4835-a06f-65e7f472775e",
		"51baf006-212d-4f5f-850f-760338ce0f08_d3769750-4c50-44c6-9c79-4cc6134df338",
		"d152ab8f-4103-4f7e-a31a-73ee816a439c",
	)
	want := "d8344fc40399d800d7cd3db877d7901022e668414f810584159135061085db5b"
	if got != want {
		t.Fatalf("签名不一致\n got=%s\nwant=%s", got, want)
	}
}
