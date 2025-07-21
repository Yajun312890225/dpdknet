package dpdknet

import (
	"testing"
)

func TestListenFunction(t *testing.T) {
	// 测试 Listen 函数是否正确工作
	// 由于这个测试需要DPDK环境，我们只测试函数存在性和基本参数验证

	// 测试不支持的网络类型
	_, err := Listen("invalid", ":8080")
	if err == nil {
		t.Error("Expected error for invalid network type, got nil")
	}

	expectedErr := "unsupported network type: invalid"
	if err.Error() != expectedErr {
		t.Errorf("Expected error message '%s', got '%s'", expectedErr, err.Error())
	}

	t.Log("Listen function exists and handles invalid network types correctly")
}
