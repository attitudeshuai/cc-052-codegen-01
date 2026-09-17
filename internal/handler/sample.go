package handler

import (
	"cc-052/internal/model"
	"cc-052/internal/service"
	"cc-052/pkg/response"
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
)

type SampleHandler struct {
	svc *service.SampleService
}

func NewSampleHandler(svc *service.SampleService) *SampleHandler {
	return &SampleHandler{svc: svc}
}

// respondErr 业务规则错误按 BizError 的状态码返回，其余按 500
func respondErr(c *gin.Context, err error) {
	var be *service.BizError
	if errors.As(err, &be) {
		response.Error(c, be.StatusCode, be.Msg)
		return
	}
	response.InternalError(c, err.Error())
}

// Create 取样登记 POST /api/v1/samples
func (h *SampleHandler) Create(c *gin.Context) {
	var req model.CreateSampleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	smp, err := h.svc.Create(&req)
	if err != nil {
		respondErr(c, err)
		return
	}
	response.Created(c, smp)
}

// List 台账列表 GET /api/v1/samples?plot_id=&status=
func (h *SampleHandler) List(c *gin.Context) {
	var plotID int64
	if v := c.Query("plot_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			response.BadRequest(c, "invalid plot_id")
			return
		}
		plotID = id
	}
	samples, err := h.svc.List(plotID, c.Query("status"))
	if err != nil {
		respondErr(c, err)
		return
	}
	response.Success(c, samples)
}

// GetByID 样品详情 GET /api/v1/samples/:id
func (h *SampleHandler) GetByID(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	detail, err := h.svc.GetDetail(id)
	if err != nil {
		respondErr(c, err)
		return
	}
	response.Success(c, detail)
}

// Void 作废样品 POST /api/v1/samples/:id/void
func (h *SampleHandler) Void(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	var req model.VoidSampleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	smp, err := h.svc.Void(id, req.Reason)
	if err != nil {
		respondErr(c, err)
		return
	}
	response.Success(c, smp)
}

// UpdateRetention 登记/修改留样位置与到期日 POST /api/v1/samples/:id/retention
func (h *SampleHandler) UpdateRetention(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	var req model.UpdateRetentionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	smp, err := h.svc.UpdateRetention(id, &req)
	if err != nil {
		respondErr(c, err)
		return
	}
	response.Success(c, smp)
}

// Dispose 留样处理 POST /api/v1/samples/:id/dispose
func (h *SampleHandler) Dispose(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	var req model.DisposeRetentionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	smp, err := h.svc.Dispose(id, &req)
	if err != nil {
		respondErr(c, err)
		return
	}
	response.Success(c, smp)
}

// CreateTransfer 交接登记 POST /api/v1/sample-transfers
func (h *SampleHandler) CreateTransfer(c *gin.Context) {
	var req model.CreateTransferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	result, err := h.svc.CreateTransfer(&req)
	if err != nil {
		respondErr(c, err)
		return
	}
	response.Created(c, result)
}

// GetTransfer 交接单详情（送出单附对账结果）GET /api/v1/sample-transfers/:id
func (h *SampleHandler) GetTransfer(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	result, err := h.svc.GetTransfer(id)
	if err != nil {
		respondErr(c, err)
		return
	}
	response.Success(c, result)
}

// CreateConclusion 登记检测结论 POST /api/v1/sample-conclusions
func (h *SampleHandler) CreateConclusion(c *gin.Context) {
	var req model.CreateConclusionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	concl, err := h.svc.CreateConclusion(&req)
	if err != nil {
		respondErr(c, err)
		return
	}
	response.Created(c, concl)
}

// ListExpiredRetentions 到期留样清单 GET /api/v1/samples/retentions/expired
func (h *SampleHandler) ListExpiredRetentions(c *gin.Context) {
	list, err := h.svc.ListExpiredRetentions()
	if err != nil {
		respondErr(c, err)
		return
	}
	response.Success(c, list)
}

// Stats 台账统计（不含作废）GET /api/v1/samples/stats
func (h *SampleHandler) Stats(c *gin.Context) {
	stats, err := h.svc.Stats()
	if err != nil {
		respondErr(c, err)
		return
	}
	response.Success(c, stats)
}
