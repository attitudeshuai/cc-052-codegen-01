package model

import (
	"errors"
	"time"
)

// ---------- 枚举 ----------

type SampleStatus string

const (
	SampleStatusSampled   SampleStatus = "sampled"   // 已取样
	SampleStatusInLab     SampleStatus = "in_lab"    // 已送检
	SampleStatusConcluded SampleStatus = "concluded" // 结论已回
	SampleStatusVoid      SampleStatus = "void"      // 作废
)

type CustodyDirection string

const (
	CustodySend   CustodyDirection = "send"   // 交出去
	CustodyReturn CustodyDirection = "return" // 收回来
)

type DisposeMethod string

const (
	DisposeDestroy DisposeMethod = "destroy" // 到期销毁
	DisposeRetain  DisposeMethod = "retain"  // 批准续存
	DisposeReturn  DisposeMethod = "return"  // 退回
)

// ---------- 实体 ----------

// Sample 样品主台账：地块、作物、取样人、取样时间 + 唯一编号
type Sample struct {
	ID         int64        `db:"id" json:"id"`
	SampleNo   string       `db:"sample_no" json:"sample_no"`
	PlotID     int64        `db:"plot_id" json:"plot_id"`
	BatchID    *int64       `db:"batch_id" json:"batch_id,omitempty"`
	CropID     string       `db:"crop_id" json:"crop_id"`
	Sampler    string       `db:"sampler" json:"sampler"`
	SampledAt  time.Time    `db:"sampled_at" json:"sampled_at"`
	Quantity   int          `db:"quantity" json:"quantity"`
	Unit       string       `db:"unit" json:"unit"`
	Status     SampleStatus `db:"status" json:"status"`
	VoidReason *string      `db:"void_reason" json:"void_reason,omitempty"`
	VoidAt     *time.Time   `db:"void_at" json:"void_at,omitempty"`
	CreatedAt  time.Time    `db:"created_at" json:"created_at"`
}

// SampleCustody 交接记录
type SampleCustody struct {
	ID            int64             `db:"id" json:"id"`
	SampleID      int64             `db:"sample_id" json:"sample_id"`
	Direction     CustodyDirection  `db:"direction" json:"direction"`
	Quantity      int               `db:"quantity" json:"quantity"`
	HandlerFrom   string            `db:"handler_from" json:"handler_from"`
	HandlerTo     string            `db:"handler_to" json:"handler_to"`
	TransferredAt time.Time         `db:"transferred_at" json:"transferred_at"`
	Note          *string           `db:"note" json:"note,omitempty"`
	CreatedAt     time.Time         `db:"created_at" json:"created_at"`
}

// SampleReserve 留样记录
type SampleReserve struct {
	ID            int64          `db:"id" json:"id"`
	SampleID      int64          `db:"sample_id" json:"sample_id"`
	Quantity      int            `db:"quantity" json:"quantity"`
	Cabinet       string         `db:"cabinet" json:"cabinet"`
	StoredAt      time.Time      `db:"stored_at" json:"stored_at"`
	ExpireAt      time.Time      `db:"expire_at" json:"expire_at"`
	DisposedAt    *time.Time     `db:"disposed_at" json:"disposed_at,omitempty"`
	DisposeMethod *DisposeMethod `db:"dispose_method" json:"dispose_method,omitempty"`
	Note          *string        `db:"note" json:"note,omitempty"`
	CreatedAt     time.Time      `db:"created_at" json:"created_at"`
}

// SampleConclusion 检测结论
type SampleConclusion struct {
	ID               int64            `db:"id" json:"id"`
	SampleID         int64            `db:"sample_id" json:"sample_id"`
	Lab              string           `db:"lab" json:"lab"`
	Result           InspectionResult `db:"result" json:"result"`
	ConcludedAt      time.Time        `db:"concluded_at" json:"concluded_at"`
	ReportNo         *string          `db:"report_no" json:"report_no,omitempty"`
	ReportURL        string           `db:"report_url" json:"report_url"`
	Items            string           `db:"items" json:"items"`
	IsActive         bool             `db:"is_active" json:"is_active"`
	InvalidateReason *string          `db:"invalidate_reason" json:"invalidate_reason,omitempty"`
	CreatedAt        time.Time        `db:"created_at" json:"created_at"`
}

// ---------- 请求体 ----------

type CreateSampleRequest struct {
	SampleNo  string `json:"sample_no"` // 留空则系统按天发号
	PlotID    int64  `json:"plot_id" binding:"required"`
	BatchID   *int64 `json:"batch_id"`
	CropID    string `json:"crop_id" binding:"required"`
	Sampler   string `json:"sampler" binding:"required"`
	SampledAt string `json:"sampled_at" binding:"required"`
	Quantity  int    `json:"quantity" binding:"omitempty,min=1"`
	Unit      string `json:"unit"`
}

type VoidSampleRequest struct {
	Reason string `json:"reason" binding:"required"`
}

type CreateCustodyRequest struct {
	Direction     CustodyDirection `json:"direction" binding:"required,oneof=send return"`
	Quantity      int              `json:"quantity" binding:"required,min=1"`
	HandlerFrom   string           `json:"handler_from" binding:"required"`
	HandlerTo     string           `json:"handler_to" binding:"required"`
	TransferredAt string           `json:"transferred_at" binding:"required"`
	Note          string           `json:"note"`
}

type CreateReserveRequest struct {
	Quantity   int    `json:"quantity" binding:"required,min=1"`
	Cabinet    string `json:"cabinet" binding:"required"`
	StoredAt   string `json:"stored_at" binding:"required"`
	ExpireAt   string `json:"expire_at"`   // 与 retain_days 二选一
	RetainDays int    `json:"retain_days"` // 入库之日起保留天数
	Note       string `json:"note"`
}

type DisposeReserveRequest struct {
	Method    DisposeMethod `json:"method" binding:"required,oneof=destroy retain return"`
	Note      string        `json:"note"`
	NewExpireAt string      `json:"new_expire_at"` // method=retain 时：新到期日
	ExtraDays int           `json:"extra_days"`    // method=retain 时：续存天数（与 new_expire_at 二选一）
}

type CreateConclusionRequest struct {
	Lab         string           `json:"lab" binding:"required"`
	Result      InspectionResult `json:"result" binding:"required,oneof=pass fail"`
	ConcludedAt string           `json:"concluded_at" binding:"required"`
	ReportNo    string           `json:"report_no"`
	ReportURL   string           `json:"report_url"`
	Items       string           `json:"items"`
}

type InvalidateConclusionRequest struct {
	Reason string `json:"reason" binding:"required"`
}

// ---------- 视图 / 响应 ----------

// 交接对账：每次收回都要和交出的数量对得上
type CustodyBalance struct {
	SentQuantity     int                 `json:"sent_quantity"`     // 累计交出
	ReturnedQuantity int                 `json:"returned_quantity"` // 累计收回
	MissingQuantity  int                 `json:"missing_quantity"`  // 对不上的差额
	Shortages        []CustodyShortage   `json:"shortages"`         // 当场点名：缺了哪些
}

type CustodyShortage struct {
	SampleNo         string `db:"sample_no" json:"sample_no"`
	SentQuantity     int    `db:"sent_quantity" json:"sent_quantity"`
	ReturnedQuantity int    `db:"returned_quantity" json:"returned_quantity"`
	MissingQuantity  int    `db:"missing_quantity" json:"missing_quantity"`
}

// 到期留样清单项
type ExpiredReserve struct {
	SampleID        int64        `db:"sample_id" json:"sample_id"`
	SampleNo        string       `db:"sample_no" json:"sample_no"`
	Cabinet         string       `db:"cabinet" json:"cabinet"`
	Quantity        int          `db:"quantity" json:"quantity"`
	ExpireAt        time.Time    `db:"expire_at" json:"expire_at"`
	SampleStatus    SampleStatus `db:"sample_status" json:"sample_status"`
	DaysOverdue     int          `db:"-" json:"days_overdue"`
	SuggestedMethod string       `db:"-" json:"suggested_method"` // 处理办法
	SuggestionNote  string       `db:"-" json:"suggestion_note"`
}

// 样品全链路视图：从地块一路跟到结论
type SampleDetail struct {
	Sample         Sample             `json:"sample"`
	PlotName       string             `json:"plot_name"`
	FarmName       string             `json:"farm_name"`
	Custodies      []SampleCustody    `json:"custodies"`
	Reserve        *SampleReserve     `json:"reserve,omitempty"`
	ActiveConclusion *SampleConclusion `json:"active_conclusion,omitempty"`
	Conclusions    []SampleConclusion `json:"conclusions"` // 含被作废/挂错的结论
}

// 样品台账统计（作废样品不进入统计）
type SampleStats struct {
	Total            int64   `json:"total"`              // 有效样品（不含作废）
	PendingConclusion int64  `json:"pending_conclusion"` // 结论未回
	Concluded        int64   `json:"concluded"`
	Passed           int64   `json:"passed"`
	Failed           int64   `json:"failed"`
	PassRate         float64 `json:"pass_rate"` // 仅按有效结论计算
	InLab            int64   `json:"in_lab"`
	Voided           int64   `json:"voided"` // 作废单列，不进分子分母
	ExpiredReserve   int64   `json:"expired_reserve"`
}

// ---------- 业务冲突错误（handler 映射为 409） ----------

var ErrNotFound = errors.New("not found")

type ConflictError struct {
	Kind    string      `json:"kind"`    // 机器可读：duplicate_no / duplicate_conclusion / ...
	Message string      `json:"message"` // 当场指认的人话
	Detail  interface{} `json:"detail,omitempty"`
}

func (e *ConflictError) Error() string { return e.Message }

func NewConflict(kind, message string, detail interface{}) *ConflictError {
	return &ConflictError{Kind: kind, Message: message, Detail: detail}
}
