package model

import "time"

type SampleStatus string

const (
	SampleStatusRegistered SampleStatus = "registered" // 已登记（取样完成，未送出）
	SampleStatusSent       SampleStatus = "sent"       // 已送出检测
	SampleStatusReturned   SampleStatus = "returned"   // 已收回
	SampleStatusConcluded  SampleStatus = "concluded"  // 结论已出
	SampleStatusVoid       SampleStatus = "void"       // 已作废（不进入统计）
)

type TransferDirection string

const (
	TransferOut TransferDirection = "out" // 交出去（送检）
	TransferIn  TransferDirection = "in"  // 收回来
)

type DisposalMethod string

const (
	DisposalDestroy DisposalMethod = "destroy" // 销毁
	DisposalExtend  DisposalMethod = "extend"  // 延长留存
	DisposalRetest  DisposalMethod = "retest"  // 送复检
)

// Sample 样品台账主表
type Sample struct {
	ID             int64        `db:"id" json:"id"`
	SampleNo       string       `db:"sample_no" json:"sample_no"`
	PlotID         int64        `db:"plot_id" json:"plot_id"`
	Crop           string       `db:"crop" json:"crop"`
	Sampler        string       `db:"sampler" json:"sampler"`
	SampledAt      time.Time    `db:"sampled_at" json:"sampled_at"`
	Quantity       int          `db:"quantity" json:"quantity"`
	Status         SampleStatus `db:"status" json:"status"`
	Cabinet        string       `db:"cabinet" json:"cabinet"`
	RetainUntil    *time.Time   `db:"retain_until" json:"retain_until,omitempty"`
	DisposedAt     *time.Time   `db:"disposed_at" json:"disposed_at,omitempty"`
	DisposalMethod string       `db:"disposal_method" json:"disposal_method,omitempty"`
	DisposedBy     string       `db:"disposed_by" json:"disposed_by,omitempty"`
	VoidReason     string       `db:"void_reason" json:"void_reason,omitempty"`
	Note           string       `db:"note" json:"note,omitempty"`
	CreatedAt      time.Time    `db:"created_at" json:"created_at"`
}

// SampleTransfer 送检/收回交接单
type SampleTransfer struct {
	ID            int64     `db:"id" json:"id"`
	Direction     string    `db:"direction" json:"direction"`
	OutTransferID *int64    `db:"out_transfer_id" json:"out_transfer_id,omitempty"`
	Lab           string    `db:"lab" json:"lab"`
	Handler       string    `db:"handler" json:"handler"`
	HappenedAt    time.Time `db:"happened_at" json:"happened_at"`
	CreatedAt     time.Time `db:"created_at" json:"created_at"`
}

// SampleTransferItem 交接明细
type SampleTransferItem struct {
	ID         int64  `db:"id" json:"id"`
	TransferID int64  `db:"transfer_id" json:"transfer_id"`
	SampleID   int64  `db:"sample_id" json:"sample_id"`
	SampleNo   string `db:"sample_no" json:"sample_no,omitempty"`
	Quantity   int    `db:"quantity" json:"quantity"`
}

// SampleConclusion 检测结论（一份样品仅一份）
type SampleConclusion struct {
	ID          int64     `db:"id" json:"id"`
	SampleID    int64     `db:"sample_id" json:"sample_id"`
	Lab         string    `db:"lab" json:"lab"`
	Result      string    `db:"result" json:"result"`
	ConcludedAt time.Time `db:"concluded_at" json:"concluded_at"`
	ReportURL   string    `db:"report_url" json:"report_url"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
}

// ReconcileItem 交接对账明细：送出 vs 收回
type ReconcileItem struct {
	SampleID    int64  `db:"sample_id" json:"sample_id"`
	SampleNo    string `db:"sample_no" json:"sample_no"`
	SentQty     int    `db:"sent_qty" json:"sent_qty"`
	ReturnedQty int    `db:"returned_qty" json:"returned_qty"`
	MissingQty  int    `db:"-" json:"missing_qty"`
}

// TransferWithItems 交接单 + 明细 + 对账结果（missing 为当场点名的短缺清单）
type TransferWithItems struct {
	Transfer *SampleTransfer      `json:"transfer"`
	Items    []SampleTransferItem `json:"items"`
	Missing  []ReconcileItem      `json:"missing,omitempty"`
}

// SampleDetail 样品台账详情（含交接流水与结论）
type SampleDetail struct {
	Sample         *Sample             `json:"sample"`
	OutstandingQty int                 `json:"outstanding_qty"` // 在途未收回数量
	Transfers      []TransferWithItems `json:"transfers"`
	Conclusion     *SampleConclusion   `json:"conclusion,omitempty"`
}

// ExpiredRetention 到期留样（含处理办法建议）
type ExpiredRetention struct {
	Sample
	DaysExpired     int    `json:"days_expired"`
	SuggestedAction string `json:"suggested_action"`
}

// SampleStats 台账统计（作废样品不进入统计，仅单列作废数）
type SampleStats struct {
	Total            int            `json:"total"`             // 有效样品总数（不含作废）
	Voided           int            `json:"voided"`            // 已作废数（单列，不参与下列统计）
	ByStatus         map[string]int `json:"by_status"`         // 按状态（不含作废）
	ByResult         map[string]int `json:"by_result"`         // 按结论（不含作废样品）
	OutstandingQty   int            `json:"outstanding_qty"`   // 在途未收回数量
	RetentionExpired int            `json:"retention_expired"` // 到期未处理留样数
}

type SampleStatusCount struct {
	Status string `db:"status"`
	Count  int    `db:"count"`
}

type SampleResultCount struct {
	Result string `db:"result"`
	Count  int    `db:"count"`
}

type CreateSampleRequest struct {
	SampleNo    string `json:"sample_no" binding:"required"`
	PlotID      int64  `json:"plot_id" binding:"required"`
	Crop        string `json:"crop" binding:"required"`
	Sampler     string `json:"sampler" binding:"required"`
	SampledAt   string `json:"sampled_at" binding:"required"`
	Quantity    int    `json:"quantity"`
	Cabinet     string `json:"cabinet"`
	RetainUntil string `json:"retain_until"`
	Note        string `json:"note"`
}

type VoidSampleRequest struct {
	Reason string `json:"reason" binding:"required"`
}

type UpdateRetentionRequest struct {
	Cabinet     string `json:"cabinet" binding:"required"`
	RetainUntil string `json:"retain_until" binding:"required"`
}

type TransferItemRequest struct {
	SampleNo string `json:"sample_no" binding:"required"`
	Quantity int    `json:"quantity" binding:"required,min=1"`
}

type CreateTransferRequest struct {
	Direction     string                `json:"direction" binding:"required,oneof=out in"`
	OutTransferID int64                 `json:"out_transfer_id"`
	Lab           string                `json:"lab"`
	Handler       string                `json:"handler" binding:"required"`
	HappenedAt    string                `json:"happened_at" binding:"required"`
	Items         []TransferItemRequest `json:"items" binding:"required,min=1,dive"`
}

type CreateConclusionRequest struct {
	SampleNo    string `json:"sample_no" binding:"required"`
	Lab         string `json:"lab"`
	Result      string `json:"result" binding:"required,oneof=pass fail"`
	ConcludedAt string `json:"concluded_at" binding:"required"`
	ReportURL   string `json:"report_url"`
}

type DisposeRetentionRequest struct {
	Method      string `json:"method" binding:"required,oneof=destroy extend retest"`
	RetainUntil string `json:"retain_until"` // method=extend 时必填新到期日
	Operator    string `json:"operator" binding:"required"`
}
