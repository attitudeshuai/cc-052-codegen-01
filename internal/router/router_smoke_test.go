package router

import "testing"

// 冒烟：确认样品台账与既有路由在同一棵 Gin 路由树上能成功注册（冲突会 panic）
func TestRoutesRegisterSmoke(t *testing.T) {
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("router registration panicked: %v", rec)
		}
	}()
	r := Setup(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if r == nil {
		t.Fatal("nil engine")
	}
}
