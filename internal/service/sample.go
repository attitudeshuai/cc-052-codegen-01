package service

import (
	"cc-052/internal/model"
	"cc-052/internal/repository"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// BizError 业务规则错误，handler 按 StatusCode 原样返回，消息面向操作人
type BizError struct {
	StatusCode int
	Msg        string
}

func (e *BizError) Error() string { return e.Msg }

func bizErrorf(code int, format string, args ...interface{}) *BizError {
	return &BizError{StatusCode: code, Msg: fmt.Sprintf(format, args...)}
}

type SampleService struct {
	repo     *repository.SampleRepo
	plotRepo *repository.PlotRepo
}

func NewSampleService(repo *repository.SampleRepo, plotRepo *repository.PlotRepo) *SampleService {
	return &SampleService{repo: repo, plotRepo: plotRepo}
}

// parseSampleTime 兼容 RFC3339 与纯日期两种格式
func parseSampleTime(v string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02", v)
}

func (s *SampleService) mustGet(id int64) (*model.Sample, error) {
	smp, err := s.repo.GetByID(id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, bizErrorf(http.StatusNotFound, "样品 #%d 不存在", id)
		}
		return nil, err
	}
	return smp, nil
}

// Create 取样登记：样品编号不许重号
func (s *SampleService) Create(req *model.CreateSampleRequest) (*model.Sample, error) {
	sampledAt, err := parseSampleTime(req.SampledAt)
	if err != nil {
		return nil, bizErrorf(http.StatusBadRequest, "取样时间格式不正确：%s", req.SampledAt)
	}
	var retainUntil *time.Time
	if req.RetainUntil != "" {
		t, err := time.Parse("2006-01-02", req.RetainUntil)
		if err != nil {
			return nil, bizErrorf(http.StatusBadRequest, "留样到期日格式不正确（应为 YYYY-MM-DD）：%s", req.RetainUntil)
		}
		retainUntil = &t
	}
	qty := req.Quantity
	if qty < 0 {
		return nil, bizErrorf(http.StatusBadRequest, "样品数量必须大于 0")
	}
	if qty == 0 {
		qty = 1
	}
	if _, err := s.plotRepo.GetByID(req.PlotID); err != nil {
		return nil, bizErrorf(http.StatusBadRequest, "地块 #%d 不存在，无法登记样品", req.PlotID)
	}
	if existing, err := s.repo.GetBySampleNo(req.SampleNo); err == nil {
		return nil, bizErrorf(http.StatusConflict, "样品编号 %s 已存在（台账 #%d，地块 #%d），不许重号",
			req.SampleNo, existing.ID, existing.PlotID)
	} else if err != sql.ErrNoRows {
		return nil, err
	}

	smp := &model.Sample{
		SampleNo:    req.SampleNo,
		PlotID:      req.PlotID,
		Crop:        req.Crop,
		Sampler:     req.Sampler,
		SampledAt:   sampledAt,
		Quantity:    qty,
		Status:      model.SampleStatusRegistered,
		Cabinet:     req.Cabinet,
		RetainUntil: retainUntil,
		Note:        req.Note,
	}
	if err := s.repo.Create(smp); err != nil {
		if strings.Contains(err.Error(), "idx_sample_sample_no") {
			return nil, bizErrorf(http.StatusConflict, "样品编号 %s 已存在，不许重号", req.SampleNo)
		}
		return nil, err
	}
	return smp, nil
}

func (s *SampleService) List(plotID int64, status string) ([]model.Sample, error) {
	if status != "" {
		switch model.SampleStatus(status) {
		case model.SampleStatusRegistered, model.SampleStatusSent,
			model.SampleStatusReturned, model.SampleStatusConcluded, model.SampleStatusVoid:
		default:
			return nil, bizErrorf(http.StatusBadRequest, "状态 %s 不支持（registered/sent/returned/concluded/void）", status)
		}
	}
	return s.repo.List(plotID, status)
}

// GetDetail 样品详情：台账 + 在途数量 + 交接流水 + 结论
func (s *SampleService) GetDetail(id int64) (*model.SampleDetail, error) {
	smp, err := s.mustGet(id)
	if err != nil {
		return nil, err
	}
	outstanding, err := s.repo.OutstandingQty(id)
	if err != nil {
		return nil, err
	}
	transfers, err := s.repo.ListTransfersBySample(id)
	if err != nil {
		return nil, err
	}
	detail := &model.SampleDetail{
		Sample:         smp,
		OutstandingQty: outstanding,
		Transfers:      []model.TransferWithItems{},
	}
	for i := range transfers {
		items, err := s.repo.ListTransferItems(transfers[i].ID)
		if err != nil {
			return nil, err
		}
		detail.Transfers = append(detail.Transfers, model.TransferWithItems{Transfer: &transfers[i], Items: items})
	}
	if concl, err := s.repo.GetConclusionBySample(id); err == nil {
		detail.Conclusion = concl
	} else if err != sql.ErrNoRows {
		return nil, err
	}
	return detail, nil
}

// Void 作废样品：作废后不再进入统计
func (s *SampleService) Void(id int64, reason string) (*model.Sample, error) {
	smp, err := s.mustGet(id)
	if err != nil {
		return nil, err
	}
	if smp.Status == model.SampleStatusVoid {
		return nil, bizErrorf(http.StatusConflict, "样品 %s 已是作废状态（作废原因：%s）", smp.SampleNo, smp.VoidReason)
	}
	if err := s.repo.UpdateVoid(id, reason); err != nil {
		return nil, err
	}
	return s.repo.GetByID(id)
}

// UpdateRetention 登记/修改留样位置与到期日
func (s *SampleService) UpdateRetention(id int64, req *model.UpdateRetentionRequest) (*model.Sample, error) {
	smp, err := s.mustGet(id)
	if err != nil {
		return nil, err
	}
	if smp.Status == model.SampleStatusVoid {
		return nil, bizErrorf(http.StatusConflict, "样品 %s 已作废，不能登记留样", smp.SampleNo)
	}
	if smp.DisposedAt != nil {
		return nil, bizErrorf(http.StatusConflict, "样品 %s 的留样已处理（方式：%s），不能再改柜子", smp.SampleNo, smp.DisposalMethod)
	}
	retainUntil, err := time.Parse("2006-01-02", req.RetainUntil)
	if err != nil {
		return nil, bizErrorf(http.StatusBadRequest, "留样到期日格式不正确（应为 YYYY-MM-DD）：%s", req.RetainUntil)
	}
	if err := s.repo.UpdateRetention(id, req.Cabinet, retainUntil); err != nil {
		return nil, err
	}
	return s.repo.GetByID(id)
}

// CreateTransfer 交接登记：out=交出去，in=收回来。
// 收回时逐项与送出单对账，少了的在返回的 missing 里当场点名。
func (s *SampleService) CreateTransfer(req *model.CreateTransferRequest) (*model.TransferWithItems, error) {
	happenedAt, err := parseSampleTime(req.HappenedAt)
	if err != nil {
		return nil, bizErrorf(http.StatusBadRequest, "交接时间格式不正确：%s", req.HappenedAt)
	}
	direction := model.TransferDirection(req.Direction)
	if direction != model.TransferOut && direction != model.TransferIn {
		return nil, bizErrorf(http.StatusBadRequest, "direction 只支持 out（交出去）/ in（收回来）")
	}

	// 同一编号先合并，防止重复行绕过数量校验
	type aggItem struct {
		sample *model.Sample
		qty    int
	}
	agg := map[string]*aggItem{}
	order := []string{}
	for _, it := range req.Items {
		no := strings.TrimSpace(it.SampleNo)
		if no == "" {
			return nil, bizErrorf(http.StatusBadRequest, "明细中存在空的样品编号")
		}
		if it.Quantity <= 0 {
			return nil, bizErrorf(http.StatusBadRequest, "样品 %s 的交接数量必须大于 0", no)
		}
		if a, ok := agg[no]; ok {
			a.qty += it.Quantity
			continue
		}
		smp, err := s.repo.GetBySampleNo(no)
		if err != nil {
			if err == sql.ErrNoRows {
				return nil, bizErrorf(http.StatusNotFound, "样品编号 %s 不存在", no)
			}
			return nil, err
		}
		agg[no] = &aggItem{sample: smp, qty: it.Quantity}
		order = append(order, no)
	}

	transfer := &model.SampleTransfer{
		Direction:  req.Direction,
		Lab:        req.Lab,
		Handler:    req.Handler,
		HappenedAt: happenedAt,
	}
	newStatus := map[int64]model.SampleStatus{}

	if direction == model.TransferOut {
		for _, no := range order {
			a := agg[no]
			if a.sample.Status == model.SampleStatusVoid {
				return nil, bizErrorf(http.StatusConflict, "样品 %s 已作废，不能送出", no)
			}
			outstanding, err := s.repo.OutstandingQty(a.sample.ID)
			if err != nil {
				return nil, err
			}
			if outstanding+a.qty > a.sample.Quantity {
				return nil, bizErrorf(http.StatusConflict,
					"样品 %s 可送出数量不足：共 %d 份，在途 %d 份，本次要送 %d 份", no, a.sample.Quantity, outstanding, a.qty)
			}
			newStatus[a.sample.ID] = model.SampleStatusSent
		}
	} else {
		if req.OutTransferID == 0 {
			return nil, bizErrorf(http.StatusBadRequest, "收回登记必须指定对应的送出单 out_transfer_id")
		}
		outT, err := s.repo.GetTransferByID(req.OutTransferID)
		if err != nil {
			if err == sql.ErrNoRows {
				return nil, bizErrorf(http.StatusNotFound, "送出单 #%d 不存在", req.OutTransferID)
			}
			return nil, err
		}
		if outT.Direction != string(model.TransferOut) {
			return nil, bizErrorf(http.StatusBadRequest, "单据 #%d 不是送出单，不能作为收回依据", req.OutTransferID)
		}
		transfer.OutTransferID = &req.OutTransferID
		for _, no := range order {
			a := agg[no]
			sentQty, err := s.repo.SentQtyInTransfer(outT.ID, a.sample.ID)
			if err != nil {
				return nil, err
			}
			if sentQty == 0 {
				return nil, bizErrorf(http.StatusConflict, "样品 %s 不在送出单 #%d 中，收回对不上，请当场核对", no, outT.ID)
			}
			returned, err := s.repo.ReturnedQtyForOutTransfer(outT.ID, a.sample.ID)
			if err != nil {
				return nil, err
			}
			if returned+a.qty > sentQty {
				return nil, bizErrorf(http.StatusConflict,
					"样品 %s 收回超量：送出单 #%d 共送出 %d 份，已收回 %d 份，本次又交 %d 份", no, outT.ID, sentQty, returned, a.qty)
			}
			outstanding, err := s.repo.OutstandingQty(a.sample.ID)
			if err != nil {
				return nil, err
			}
			// 作废样品允许登记收回（保持数量对得上），但状态保持 void，不再回到台账流转
			if a.sample.Status != model.SampleStatusVoid && outstanding-a.qty <= 0 {
				newStatus[a.sample.ID] = model.SampleStatusReturned
			}
		}
	}

	items := []model.SampleTransferItem{}
	for _, no := range order {
		a := agg[no]
		items = append(items, model.SampleTransferItem{SampleID: a.sample.ID, SampleNo: no, Quantity: a.qty})
	}
	if err := s.repo.CreateTransfer(transfer, items, newStatus); err != nil {
		return nil, err
	}

	result := &model.TransferWithItems{Transfer: transfer, Items: items}
	if direction == model.TransferIn {
		missing, err := s.repo.ReconcileOutTransfer(*transfer.OutTransferID)
		if err != nil {
			return nil, err
		}
		result.Missing = filterMissing(missing)
	}
	return result, nil
}

// GetTransfer 交接单详情；送出单附对账结果（短缺点名）
func (s *SampleService) GetTransfer(id int64) (*model.TransferWithItems, error) {
	t, err := s.repo.GetTransferByID(id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, bizErrorf(http.StatusNotFound, "交接单 #%d 不存在", id)
		}
		return nil, err
	}
	items, err := s.repo.ListTransferItems(id)
	if err != nil {
		return nil, err
	}
	result := &model.TransferWithItems{Transfer: t, Items: items}
	if t.Direction == string(model.TransferOut) {
		missing, err := s.repo.ReconcileOutTransfer(t.ID)
		if err != nil {
			return nil, err
		}
		result.Missing = filterMissing(missing)
	}
	return result, nil
}

// filterMissing 筛出送出多于收回的样品（当场点名清单）
func filterMissing(items []model.ReconcileItem) []model.ReconcileItem {
	missing := []model.ReconcileItem{}
	for _, it := range items {
		it.MissingQty = it.SentQty - it.ReturnedQty
		if it.MissingQty > 0 {
			missing = append(missing, it)
		}
	}
	return missing
}

// CreateConclusion 登记检测结论：重复结论、挂错样品当场指认
func (s *SampleService) CreateConclusion(req *model.CreateConclusionRequest) (*model.SampleConclusion, error) {
	concludedAt, err := parseSampleTime(req.ConcludedAt)
	if err != nil {
		return nil, bizErrorf(http.StatusBadRequest, "结论日期格式不正确：%s", req.ConcludedAt)
	}
	smp, err := s.repo.GetBySampleNo(strings.TrimSpace(req.SampleNo))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, bizErrorf(http.StatusNotFound, "样品编号 %s 不存在，结论挂错了样品，请当场核对", req.SampleNo)
		}
		return nil, err
	}
	if smp.Status == model.SampleStatusVoid {
		return nil, bizErrorf(http.StatusConflict, "样品 %s 已作废（原因：%s），作废样品不能登记结论", smp.SampleNo, smp.VoidReason)
	}
	if smp.Status == model.SampleStatusRegistered {
		return nil, bizErrorf(http.StatusConflict, "样品 %s 从未送出检测，这份结论挂错了样品，请当场核对", smp.SampleNo)
	}
	if existing, err := s.repo.GetConclusionBySample(smp.ID); err == nil {
		return nil, bizErrorf(http.StatusConflict,
			"样品 %s 已有一份结论（结论 #%d，结果 %s），同一份样品不允许两份结论", smp.SampleNo, existing.ID, existing.Result)
	} else if err != sql.ErrNoRows {
		return nil, err
	}

	concl := &model.SampleConclusion{
		SampleID:    smp.ID,
		Lab:         req.Lab,
		Result:      req.Result,
		ConcludedAt: concludedAt,
		ReportURL:   req.ReportURL,
	}
	if err := s.repo.CreateConclusion(concl); err != nil {
		if strings.Contains(err.Error(), "idx_sample_conclusion_sample") {
			return nil, bizErrorf(http.StatusConflict, "样品 %s 已有一份结论，同一份样品不允许两份结论", smp.SampleNo)
		}
		return nil, err
	}
	return concl, nil
}

// ListExpiredRetentions 到期未处理留样清单，附处理办法建议
func (s *SampleService) ListExpiredRetentions() ([]model.ExpiredRetention, error) {
	samples, err := s.repo.ListExpiredRetentions()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]model.ExpiredRetention, 0, len(samples))
	for _, smp := range samples {
		days := 0
		if smp.RetainUntil != nil {
			days = int(now.Sub(*smp.RetainUntil).Hours() / 24)
		}
		action := "结论未回，建议延长留存（extend）或申请复检（retest）"
		if smp.Status == model.SampleStatusConcluded {
			action = "结论已出，留样可销毁（destroy）"
		}
		out = append(out, model.ExpiredRetention{
			Sample:          smp,
			DaysExpired:     days,
			SuggestedAction: action,
		})
	}
	return out, nil
}

// Dispose 留样处理登记：destroy=销毁，extend=延长留存，retest=送复检
func (s *SampleService) Dispose(id int64, req *model.DisposeRetentionRequest) (*model.Sample, error) {
	smp, err := s.mustGet(id)
	if err != nil {
		return nil, err
	}
	if smp.Status == model.SampleStatusVoid {
		return nil, bizErrorf(http.StatusConflict, "样品 %s 已作废，无需处理留样", smp.SampleNo)
	}
	if smp.RetainUntil == nil {
		return nil, bizErrorf(http.StatusConflict, "样品 %s 未登记留样（无柜子与到期日），无需处理", smp.SampleNo)
	}
	if smp.DisposedAt != nil {
		return nil, bizErrorf(http.StatusConflict, "样品 %s 的留样已处理（方式：%s），不能重复处理", smp.SampleNo, smp.DisposalMethod)
	}

	switch model.DisposalMethod(req.Method) {
	case model.DisposalDestroy, model.DisposalRetest:
		if err := s.repo.UpdateRetentionDisposed(id, req.Method, req.Operator); err != nil {
			return nil, err
		}
	case model.DisposalExtend:
		if req.RetainUntil == "" {
			return nil, bizErrorf(http.StatusBadRequest, "延长留存必须给出新的到期日 retain_until")
		}
		t, err := time.Parse("2006-01-02", req.RetainUntil)
		if err != nil {
			return nil, bizErrorf(http.StatusBadRequest, "新的到期日格式不正确（应为 YYYY-MM-DD）：%s", req.RetainUntil)
		}
		if t.Before(time.Now().Truncate(24 * time.Hour)) {
			return nil, bizErrorf(http.StatusBadRequest, "新的到期日 %s 不能早于今天", req.RetainUntil)
		}
		if err := s.repo.UpdateRetainUntil(id, t); err != nil {
			return nil, err
		}
	default:
		return nil, bizErrorf(http.StatusBadRequest, "处理办法 %s 不支持（可选 destroy/extend/retest）", req.Method)
	}
	return s.repo.GetByID(id)
}

// Stats 台账统计：作废样品不进入统计，仅单列作废数
func (s *SampleService) Stats() (*model.SampleStats, error) {
	stats := &model.SampleStats{ByStatus: map[string]int{}, ByResult: map[string]int{}}

	statusCounts, err := s.repo.StatusCounts()
	if err != nil {
		return nil, err
	}
	for _, sc := range statusCounts {
		if sc.Status == string(model.SampleStatusVoid) {
			stats.Voided += sc.Count
			continue
		}
		stats.ByStatus[sc.Status] = sc.Count
		stats.Total += sc.Count
	}

	resultCounts, err := s.repo.ResultCounts()
	if err != nil {
		return nil, err
	}
	for _, rc := range resultCounts {
		stats.ByResult[rc.Result] = rc.Count
	}

	if stats.OutstandingQty, err = s.repo.OutstandingTotalQty(); err != nil {
		return nil, err
	}
	if stats.RetentionExpired, err = s.repo.CountExpiredRetentions(); err != nil {
		return nil, err
	}
	return stats, nil
}
