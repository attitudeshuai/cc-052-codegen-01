package service

import (
	"cc-052/internal/model"
	"testing"
)

// 交接对账：交出去 / 收回来每次都要对得上
func TestCustodyCheck(t *testing.T) {
	const no = "YP202609170001"

	// 取样 10 份
	if err := custodyCheck(no, model.CustodySend, 6, 10, 0, 0); err != nil {
		t.Fatalf("首次交出 6 份应通过，实际: %v", err)
	}
	// 再交出超过取样总量 → 当场点名为对不上
	err := custodyCheck(no, model.CustodySend, 5, 10, 6, 0)
	if err == nil || err.Kind != "custody_over_send" {
		t.Fatalf("累计交出 11>10 应报 custody_over_send，实际: %+v", err)
	}
	// 收回 4 份正常
	if err := custodyCheck(no, model.CustodyReturn, 4, 10, 6, 0); err != nil {
		t.Fatalf("收回 4 份应通过，实际: %v", err)
	}
	// 在外只剩 2 份，收回 3 份 → 来源对不上
	err = custodyCheck(no, model.CustodyReturn, 3, 10, 6, 4)
	if err == nil || err.Kind != "custody_over_return" {
		t.Fatalf("收回超过在外数量应报 custody_over_return，实际: %+v", err)
	}
	// 恰好结清应通过
	if err := custodyCheck(no, model.CustodyReturn, 2, 10, 6, 4); err != nil {
		t.Fatalf("恰好收回剩余 2 份应通过，实际: %v", err)
	}
}

// 到期留样：结论未回不许销毁，应建议续存
func TestExpiredSuggestion(t *testing.T) {
	cases := []struct {
		status       model.SampleStatus
		wantMethod   model.DisposeMethod
	}{
		{model.SampleStatusConcluded, model.DisposeDestroy},
		{model.SampleStatusInLab, model.DisposeRetain},
		{model.SampleStatusSampled, model.DisposeReturn},
	}
	for _, c := range cases {
		method, note := expiredSuggestion(c.status)
		if method != string(c.wantMethod) {
			t.Errorf("状态 %s 期望处置 %s，实际 %s（%s）", c.status, c.wantMethod, method, note)
		}
		if note == "" {
			t.Errorf("状态 %s 必须给出处理办法说明", c.status)
		}
	}
}
