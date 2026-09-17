package handler

import (
	"cc-052/internal/model"
	"cc-052/internal/repository"
	"cc-052/internal/service"
	"cc-052/pkg/response"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

type SampleHandler struct {
	svc *service.SampleService
}

func NewSampleHandler(svc *service.SampleService) *SampleHandler {
	return &SampleHandler{svc: svc}
}

// conflictBody 业务冲突（重号/对不上账/重复结论/挂错样品）当场指认
type conflictBody struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Kind    string      `json:"kind"`
	Detail  interface{} `json:"detail,omitempty"`
}

func fail(c *gin.Context, err error) {
	var ce *model.ConflictError
	if errors.As(err, &ce) {
		c.JSON(409, conflictBody{Code: 4090, Message: ce.Message, Kind: ce.Kind, Detail: ce.Detail})
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		response.NotFound(c, "样品或相关记录不存在")
		return
	}
	response.InternalError(c, err.Error())
}

// ---------- 取样登记 ----------

func (h *SampleHandler) Create(c *gin.Context) {
	var req model.CreateSampleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	sample, err := h.svc.Create(&req)
	if err != nil {
		fail(c, err)
		return
	}
	response.Created(c, sample)
}

func (h *SampleHandler) Get(c *gin.Context) {
	detail, err := h.svc.GetDetail(c.Param("idOrNo"))
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, detail)
}

func (h *SampleHandler) List(c *gin.Context) {
	f := repository.SampleFilter{
		Status:  c.Query("status"),
		Sampler: c.Query("sampler"),
	}
	if v := c.Query("plot_id"); v != "" {
		f.PlotID, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := c.Query("batch_id"); v != "" {
		f.BatchID, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := c.Query("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.From = &t
		}
	}
	if v := c.Query("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.To = &t
		}
	}
	f.Limit, _ = strconv.Atoi(c.DefaultQuery("limit", "100"))
	f.Offset, _ = strconv.Atoi(c.DefaultQuery("offset", "0"))

	samples, total, err := h.svc.List(f)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"total": total, "items": samples})
}

func (h *SampleHandler) Void(c *gin.Context) {
	var req model.VoidSampleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	sample, err := h.svc.Void(c.Param("idOrNo"), req.Reason)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, sample)
}

// ---------- 交接对账 ----------

func (h *SampleHandler) AddCustody(c *gin.Context) {
	var req model.CreateCustodyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	balance, err := h.svc.AddCustody(c.Param("idOrNo"), &req)
	if err != nil {
		fail(c, err)
		return
	}
	response.Created(c, balance)
}

func (h *SampleHandler) Balance(c *gin.Context) {
	b, err := h.svc.Balance(c.Param("idOrNo"))
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, b)
}

// RollCall 当场点名：交出与收回对不上的样品
func (h *SampleHandler) RollCall(c *gin.Context) {
	var batchID int64
	if v := c.Query("batch_id"); v != "" {
		batchID, _ = strconv.ParseInt(v, 10, 64)
	}
	rows, err := h.svc.RollCall(batchID)
	if err != nil {
		fail(c, err)
		return
	}
	missing := 0
	for _, r := range rows {
		missing += r.MissingQuantity
	}
	response.Success(c, gin.H{"shortage_samples": len(rows), "missing_quantity": missing, "items": rows})
}

// ---------- 留样 ----------

func (h *SampleHandler) CreateReserve(c *gin.Context) {
	var req model.CreateReserveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	r, err := h.svc.CreateReserve(c.Param("idOrNo"), &req)
	if err != nil {
		fail(c, err)
		return
	}
	response.Created(c, r)
}

func (h *SampleHandler) DisposeReserve(c *gin.Context) {
	var req model.DisposeReserveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	r, err := h.svc.DisposeReserve(c.Param("idOrNo"), &req)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, r)
}

// ListExpired 到期留样单独列出 + 处理办法
func (h *SampleHandler) ListExpired(c *gin.Context) {
	rows, err := h.svc.ListExpired()
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"count": len(rows), "items": rows})
}

// ---------- 结论 ----------

func (h *SampleHandler) CreateConclusion(c *gin.Context) {
	var req model.CreateConclusionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	cc, err := h.svc.CreateConclusion(c.Param("idOrNo"), &req)
	if err != nil {
		fail(c, err)
		return
	}
	response.Created(c, cc)
}

func (h *SampleHandler) InvalidateConclusion(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("cid"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid conclusion id")
		return
	}
	var req model.InvalidateConclusionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	cc, err := h.svc.InvalidateConclusion(id, req.Reason)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, cc)
}

// ---------- 统计 ----------

func (h *SampleHandler) Stats(c *gin.Context) {
	st, err := h.svc.Stats()
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, st)
}
