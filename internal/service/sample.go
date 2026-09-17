package service

import (
	"cc-052/internal/model"
	"cc-052/internal/repository"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/lib/pq"
)

type SampleService struct {
	sampleRepo     *repository.SampleRepo
	custodyRepo    *repository.CustodyRepo
	reserveRepo    *repository.ReserveRepo
	conclusionRepo *repository.ConclusionRepo
	plotRepo       *repository.PlotRepo
	farmRepo       *repository.FarmRepo
	batchRepo      *repository.BatchRepo
}

func NewSampleService(
	sampleRepo *repository.SampleRepo,
	custodyRepo *repository.CustodyRepo,
	reserveRepo *repository.ReserveRepo,
	conclusionRepo *repository.ConclusionRepo,
	plotRepo *repository.PlotRepo,
	farmRepo *repository.FarmRepo,
	batchRepo *repository.BatchRepo,
) *SampleService {
	return &SampleService{
		sampleRepo:     sampleRepo,
		custodyRepo:    custodyRepo,
		reserveRepo:    reserveRepo,
		conclusionRepo: conclusionRepo,
		plotRepo:       plotRepo,
		farmRepo:       farmRepo,
		batchRepo:      batchRepo,
	}
}

// parseFlexTime 兼容 RFC3339 与 yyyy-MM-dd
func parseFlexTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02", s)
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ---------- 取样登记 ----------

func (s *SampleService) Create(req *model.CreateSampleRequest) (*model.Sample, error) {
	sampledAt, err := parseFlexTime(req.SampledAt)
	if err != nil {
		return nil, model.NewConflict("invalid_time", "取样时间格式不正确", map[string]string{"sampled_at": req.SampledAt})
	}

	// 地块必须存在
	plot, err := s.plotRepo.GetByID(req.PlotID)
	if err != nil {
		return nil, model.NewConflict("plot_not_found", "地块不存在", map[string]int64{"plot_id": req.PlotID})
	}
	_ = plot

	// 批次若指定也必须存在
	if req.BatchID != nil {
		if _, err := s.batchRepo.GetByID(*req.BatchID); err != nil {
			return nil, model.NewConflict("batch_not_found", "作物批次不存在", map[string]int64{"batch_id": *req.BatchID})
		}
	}

	qty := req.Quantity
	if qty <= 0 {
		qty = 1
	}
	unit := req.Unit
	if unit == "" {
		unit = "份"
	}

	sample := &model.Sample{
		SampleNo:  req.SampleNo,
		PlotID:    req.PlotID,
		BatchID:   req.BatchID,
		CropID:    req.CropID,
		Sampler:   req.Sampler,
		SampledAt: sampledAt,
		Quantity:  qty,
		Unit:      unit,
		Status:    model.SampleStatusSampled,
	}

	if err := s.sampleRepo.Create(sample); err != nil {
		if repository.IsUniqueViolation(err) {
			return nil, model.NewConflict("duplicate_no",
				"样品编号已存在，不许重号", map[string]string{"sample_no": req.SampleNo})
		}
		return nil, err
	}
	return sample, nil
}

// ResolveSample 支持按内部 ID 或样品编号取样品
func (s *SampleService) ResolveSample(idOrNo string) (*model.Sample, error) {
	// 纯数字先按 ID 找，找不到再按编号
	var sample *model.Sample
	if id, err := strconv.ParseInt(idOrNo, 10, 64); err == nil {
		if sp, err := s.sampleRepo.GetByID(id); err == nil {
			sample = sp
		}
	}
	if sample == nil {
		sp, err := s.sampleRepo.GetBySampleNo(idOrNo)
		if err != nil {
			return nil, err
		}
		sample = sp
	}
	return sample, nil
}

func (s *SampleService) GetDetail(idOrNo string) (*model.SampleDetail, error) {
	sample, err := s.ResolveSample(idOrNo)
	if err != nil {
		return nil, err
	}

	plot, _ := s.plotRepo.GetByID(sample.PlotID)
	detail := &model.SampleDetail{Sample: *sample, Conclusions: []model.SampleConclusion{}}
	if plot != nil {
		detail.PlotName = plot.Name
		if farm, err := s.farmRepo.GetByID(plot.FarmID); err == nil {
			detail.FarmName = farm.Name
		}
	}

	custodies, err := s.custodyRepo.ListBySample(sample.ID)
	if err != nil {
		return nil, err
	}
	detail.Custodies = custodies

	if reserve, err := s.reserveRepo.GetBySample(sample.ID); err == nil {
		detail.Reserve = reserve
	}

	conclusions, err := s.conclusionRepo.ListBySample(sample.ID)
	if err != nil {
		return nil, err
	}
	for i := range conclusions {
		c := conclusions[i]
		if c.IsActive {
			cp := c
			detail.ActiveConclusion = &cp
		}
	}
	detail.Conclusions = conclusions

	return detail, nil
}

func (s *SampleService) List(f repository.SampleFilter) ([]model.Sample, int, error) {
	return s.sampleRepo.List(f)
}

// ---------- 作废 ----------

func (s *SampleService) Void(idOrNo string, reason string) (*model.Sample, error) {
	sample, err := s.ResolveSample(idOrNo)
	if err != nil {
		return nil, err
	}
	if sample.Status == model.SampleStatusVoid {
		return nil, model.NewConflict("already_void", "样品已作废", map[string]string{"sample_no": sample.SampleNo})
	}
	if err := s.sampleRepo.Void(sample.ID, reason, time.Now()); err != nil {
		return nil, err
	}
	// 作废样品的结论一并失效，立刻退出统计
	if err := s.conclusionRepo.DeactivateBySample(sample.ID, "样品作废："+reason); err != nil {
		return nil, err
	}
	return s.sampleRepo.GetByID(sample.ID)
}

// ---------- 交接对账 ----------

func (s *SampleService) AddCustody(idOrNo string, req *model.CreateCustodyRequest) (*model.CustodyBalance, error) {
	sample, err := s.ResolveSample(idOrNo)
	if err != nil {
		return nil, err
	}
	if sample.Status == model.SampleStatusVoid {
		return nil, model.NewConflict("sample_void", "样品已作废，不能再交接",
			map[string]string{"sample_no": sample.SampleNo})
	}

	at, err := parseFlexTime(req.TransferredAt)
	if err != nil {
		return nil, model.NewConflict("invalid_time", "交接时间格式不正确", nil)
	}

	sent, returned, err := s.custodyRepo.Sums(sample.ID)
	if err != nil {
		return nil, err
	}

	if conflict := custodyCheck(sample.SampleNo, req.Direction, req.Quantity, sample.Quantity, sent, returned); conflict != nil {
		return nil, conflict
	}

	c := &model.SampleCustody{
		SampleID:      sample.ID,
		Direction:     req.Direction,
		Quantity:      req.Quantity,
		HandlerFrom:   req.HandlerFrom,
		HandlerTo:     req.HandlerTo,
		TransferredAt: at,
		Note:          strPtr(req.Note),
	}
	if err := s.custodyRepo.Create(c); err != nil {
		return nil, err
	}

	// 首次交出：样品进入在检
	if req.Direction == model.CustodySend && sample.Status == model.SampleStatusSampled {
		if err := s.sampleRepo.UpdateStatus(sample.ID, model.SampleStatusInLab); err != nil {
			return nil, err
		}
	}

	return s.Balance(idOrNo)
}

// Balance 单样品对账
func (s *SampleService) Balance(idOrNo string) (*model.CustodyBalance, error) {
	sample, err := s.ResolveSample(idOrNo)
	if err != nil {
		return nil, err
	}
	sent, returned, err := s.custodyRepo.Sums(sample.ID)
	if err != nil {
		return nil, err
	}
	b := &model.CustodyBalance{
		SentQuantity:     sent,
		ReturnedQuantity: returned,
		MissingQuantity:  sent - returned,
	}
	if b.MissingQuantity != 0 {
		b.Shortages = []model.CustodyShortage{{
			SampleNo:         sample.SampleNo,
			SentQuantity:     sent,
			ReturnedQuantity: returned,
			MissingQuantity:  b.MissingQuantity,
		}}
	}
	return b, nil
}

// RollCall 当场点名：列出所有交出与收回数量对不上的样品
func (s *SampleService) RollCall(batchID int64) ([]model.CustodyShortage, error) {
	return s.custodyRepo.Shortages(batchID)
}

// ---------- 留样 ----------

func (s *SampleService) CreateReserve(idOrNo string, req *model.CreateReserveRequest) (*model.SampleReserve, error) {
	sample, err := s.ResolveSample(idOrNo)
	if err != nil {
		return nil, err
	}
	if sample.Status == model.SampleStatusVoid {
		return nil, model.NewConflict("sample_void", "样品已作废，不能登记留样",
			map[string]string{"sample_no": sample.SampleNo})
	}
	if req.Quantity > sample.Quantity {
		return nil, model.NewConflict("reserve_over_quantity", "留样数量不能超过取样数量",
			map[string]interface{}{"sample_no": sample.SampleNo, "sampled": sample.Quantity})
	}

	storedAt, err := parseFlexTime(req.StoredAt)
	if err != nil {
		return nil, model.NewConflict("invalid_time", "入库时间格式不正确", nil)
	}
	var expireAt time.Time
	if req.ExpireAt != "" {
		expireAt, err = parseFlexTime(req.ExpireAt)
		if err != nil {
			return nil, model.NewConflict("invalid_time", "到期时间格式不正确", nil)
		}
	} else if req.RetainDays > 0 {
		expireAt = storedAt.AddDate(0, 0, req.RetainDays)
	} else {
		return nil, model.NewConflict("expire_required", "必须填写 expire_at 到期时间或 retain_days 保留天数", nil)
	}
	if !expireAt.After(storedAt) {
		return nil, model.NewConflict("expire_before_store", "到期时间必须晚于入库时间",
			map[string]string{"stored_at": storedAt.Format(time.RFC3339), "expire_at": expireAt.Format(time.RFC3339)})
	}

	// 一份样品只建一条留样台账
	if existing, err := s.reserveRepo.GetBySample(sample.ID); err == nil {
		return nil, model.NewConflict("reserve_exists", "该样品已登记留样",
			map[string]interface{}{"reserve_id": existing.ID, "cabinet": existing.Cabinet})
	}

	r := &model.SampleReserve{
		SampleID: sample.ID,
		Quantity: req.Quantity,
		Cabinet:  req.Cabinet,
		StoredAt: storedAt,
		ExpireAt: expireAt,
		Note:     strPtr(req.Note),
	}
	if err := s.reserveRepo.Create(r); err != nil {
		if repository.IsUniqueViolation(err) {
			return nil, model.NewConflict("reserve_exists", "该样品已登记留样", nil)
		}
		return nil, err
	}
	return r, nil
}

func (s *SampleService) DisposeReserve(idOrNo string, req *model.DisposeReserveRequest) (*model.SampleReserve, error) {
	sample, err := s.ResolveSample(idOrNo)
	if err != nil {
		return nil, err
	}
	if _, err := s.reserveRepo.GetBySample(sample.ID); errors.Is(err, sql.ErrNoRows) {
		return nil, model.NewConflict("no_reserve", "该样品没有留样记录", map[string]string{"sample_no": sample.SampleNo})
	} else if err != nil {
		return nil, err
	}

	switch req.Method {
	case model.DisposeDestroy, model.DisposeReturn:
		if err := s.reserveRepo.Dispose(sample.ID, req.Method, time.Now(), strPtr(req.Note)); err != nil {
			return nil, err
		}
	case model.DisposeRetain:
		// 续存必须给出新的到期日
		var newExpire time.Time
		if req.NewExpireAt != "" {
			newExpire, err = parseFlexTime(req.NewExpireAt)
			if err != nil {
				return nil, model.NewConflict("invalid_time", "新到期时间格式不正确", nil)
			}
		} else if req.ExtraDays > 0 {
			newExpire = time.Now().AddDate(0, 0, req.ExtraDays)
		} else {
			return nil, model.NewConflict("retain_term_required", "续存必须填写 new_expire_at 或 extra_days", nil)
		}
		if !newExpire.After(time.Now()) {
			return nil, model.NewConflict("retain_term_invalid", "新到期日必须晚于今天", nil)
		}
		if err := s.reserveRepo.Retain(sample.ID, newExpire, strPtr(req.Note)); err != nil {
			return nil, err
		}
	default:
		return nil, model.NewConflict("invalid_method", "处置方式只能是 destroy / retain / return", nil)
	}

	return s.reserveRepo.GetBySample(sample.ID)
}

// ListExpired 到期留样单独列出，并逐份给处理办法
func (s *SampleService) ListExpired() ([]model.ExpiredReserve, error) {
	now := time.Now()
	rows, err := s.reserveRepo.Expired(now)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		r := &rows[i]
		r.DaysOverdue = int(now.Sub(r.ExpireAt).Hours() / 24)
		r.SuggestedMethod, r.SuggestionNote = expiredSuggestion(r.SampleStatus)
	}
	return rows, nil
}

// expiredSuggestion 到期留样处理办法：按样品当前状态给处置建议
func expiredSuggestion(status model.SampleStatus) (method, note string) {
	switch status {
	case model.SampleStatusConcluded:
		return string(model.DisposeDestroy), "检测结论已回，留样已到期，按规程监督销毁并登记"
	case model.SampleStatusInLab:
		return string(model.DisposeRetain), "结论尚未回来，留样不得销毁，批准续存至结论回来后再处置"
	default:
		return string(model.DisposeReturn), "样品未送检即到期，先核实取样与交接情况再决定退回或销毁"
	}
}

// custodyCheck 交接对账核心规则：交出去的与收回来的每次都要对得上。
// sampled=取样数量，sent/returned=此前累计交出/收回，qty=本次数量。
func custodyCheck(no string, dir model.CustodyDirection, qty, sampled, sent, returned int) *model.ConflictError {
	switch dir {
	case model.CustodySend:
		// 交出去的数量不能超过取样数量
		if sent+qty > sampled {
			return model.NewConflict("custody_over_send",
				"交出数量超过取样数量，对不上账",
				map[string]interface{}{
					"sample_no":    no,
					"sampled":      sampled,
					"already_sent": sent,
					"this_send":    qty,
					"available":    sampled - sent,
				})
		}
	case model.CustodyReturn:
		// 收回来的数量不能超过交出去尚未收回的数量
		if returned+qty > sent {
			return model.NewConflict("custody_over_return",
				"收回数量超过在外数量，来源对不上",
				map[string]interface{}{
					"sample_no":        no,
					"sent":             sent,
					"already_returned": returned,
					"this_return":      qty,
					"outstanding":      sent - returned,
				})
		}
	}
	return nil
}

// ---------- 检测结论 ----------

func (s *SampleService) CreateConclusion(idOrNo string, req *model.CreateConclusionRequest) (*model.SampleConclusion, error) {
	sample, err := s.ResolveSample(idOrNo)
	if err != nil {
		return nil, err
	}
	if sample.Status == model.SampleStatusVoid {
		return nil, model.NewConflict("sample_void", "样品已作废，不能登记结论",
			map[string]string{"sample_no": sample.SampleNo})
	}

	concludedAt, err := parseFlexTime(req.ConcludedAt)
	if err != nil {
		return nil, model.NewConflict("invalid_time", "结论时间格式不正确", nil)
	}
	if concludedAt.Before(sample.SampledAt) {
		return nil, model.NewConflict("conclusion_before_sample", "结论时间早于取样时间，结论可疑",
			map[string]interface{}{"sample_no": sample.SampleNo, "sampled_at": sample.SampledAt, "concluded_at": concludedAt})
	}

	// 同一份样品已有有效结论 → 第二份当场指认为重复结论
	if existing, err := s.conclusionRepo.GetActiveBySample(sample.ID); err == nil {
		return nil, model.NewConflict("duplicate_conclusion",
			"同一份样品已存在有效检测结论，第二份结论须核查（复检请先作废原结论）",
			map[string]interface{}{
				"sample_no":             sample.SampleNo,
				"existing_conclusion_id": existing.ID,
				"existing_result":       existing.Result,
				"existing_report_no":    existing.ReportNo,
			})
	}

	// 报告号已挂在另一份样品上 → 结论挂错样品，当场指认
	if req.ReportNo != "" {
		if other, err := s.conclusionRepo.GetActiveByReportNo(req.ReportNo); err == nil && other.SampleID != sample.ID {
			otherSample, _ := s.sampleRepo.GetByID(other.SampleID)
			otherNo := ""
			if otherSample != nil {
				otherNo = otherSample.SampleNo
			}
			return nil, model.NewConflict("report_no_misattached",
				"该报告号已挂在另一份样品上，结论疑似挂错样品",
				map[string]interface{}{
					"report_no":       req.ReportNo,
					"bound_sample_no": otherNo,
					"bound_sample_id": other.SampleID,
					"this_sample_no":  sample.SampleNo,
				})
		}
	}

	items := req.Items
	if items == "" {
		items = "[]"
	}
	c := &model.SampleConclusion{
		SampleID:    sample.ID,
		Lab:         req.Lab,
		Result:      req.Result,
		ConcludedAt: concludedAt,
		ReportNo:    strPtr(req.ReportNo),
		ReportURL:   req.ReportURL,
		Items:       items,
		IsActive:    true,
	}
	if err := s.conclusionRepo.Create(c); err != nil {
		// 并发下的唯一约束兜底：按约束名区分两种异常
		if repository.IsUniqueViolation(err) {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) {
				switch pqErr.Constraint {
				case "idx_conclusion_active_one":
					return nil, model.NewConflict("duplicate_conclusion", "同一份样品只允许一份有效结论",
						map[string]string{"sample_no": sample.SampleNo})
				case "idx_conclusion_report_no":
					return nil, model.NewConflict("report_no_misattached", "报告号与现有结论重复，疑似挂错样品",
						map[string]string{"report_no": req.ReportNo})
				}
			}
			return nil, model.NewConflict("duplicate_conclusion", "结论重复，请核查", nil)
		}
		return nil, err
	}

	// 结论回来：样品结案
	if err := s.sampleRepo.UpdateStatus(sample.ID, model.SampleStatusConcluded); err != nil {
		return nil, err
	}
	// 结论不合格：锁定对应批次，禁止出码上市
	if req.Result == model.InspectionFail && sample.BatchID != nil {
		if err := s.batchRepo.UpdateStatus(*sample.BatchID, model.BatchStatusLocked); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// InvalidateConclusion 指认并作废挂错/错误的结论
func (s *SampleService) InvalidateConclusion(conclusionID int64, reason string) (*model.SampleConclusion, error) {
	c, err := s.conclusionRepo.GetByID(conclusionID)
	if err != nil {
		return nil, err
	}
	if !c.IsActive {
		return nil, model.NewConflict("already_invalid", "该结论已是作废状态",
			map[string]int64{"conclusion_id": conclusionID})
	}
	if err := s.conclusionRepo.Deactivate(conclusionID, reason); err != nil {
		return nil, err
	}
	// 没有有效结论后样品退回在检，等待正确结论；统计立刻剔除该结论
	if _, err := s.conclusionRepo.GetActiveBySample(c.SampleID); errors.Is(err, sql.ErrNoRows) {
		_ = s.sampleRepo.UpdateStatus(c.SampleID, model.SampleStatusInLab)
	}
	return s.conclusionRepo.GetByID(conclusionID)
}

// ---------- 统计 ----------

func (s *SampleService) Stats() (*model.SampleStats, error) {
	counters, err := s.conclusionRepo.Counters()
	if err != nil {
		return nil, err
	}
	expired, err := s.reserveRepo.ExpiredCount(time.Now())
	if err != nil {
		return nil, err
	}
	st := &model.SampleStats{
		Total:             counters.Total,
		InLab:             counters.InLab,
		Voided:            counters.Voided,
		Concluded:         counters.Concluded,
		Passed:            counters.Passed,
		Failed:            counters.Failed,
		ExpiredReserve:    expired,
		PendingConclusion: counters.Total - counters.Concluded,
	}
	if st.Concluded > 0 {
		st.PassRate = float64(st.Passed) / float64(st.Concluded)
	}
	return st, nil
}
