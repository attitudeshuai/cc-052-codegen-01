package router

import (
	"cc-052/internal/handler"
	"cc-052/internal/middleware"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

func Setup(
	farmH *handler.FarmHandler,
	plotH *handler.PlotHandler,
	batchH *handler.BatchHandler,
	activityH *handler.ActivityHandler,
	inspectionH *handler.InspectionHandler,
	traceCodeH *handler.TraceCodeHandler,
	sampleH *handler.SampleHandler,
	healthH *handler.HealthHandler,
	rdb *redis.Client,
) *gin.Engine {
	r := gin.Default()

	// Health check
	r.GET("/healthz", healthH.Health)

	// API v1
	v1 := r.Group("/api/v1")
	{
		// Farms
		v1.POST("/farms", farmH.Create)
		v1.GET("/farms", farmH.List)
		v1.GET("/farms/:id", farmH.GetByID)

		// Plots
		v1.POST("/plots", plotH.Create)
		v1.GET("/plots/:id", plotH.GetByID)
		v1.GET("/plots", plotH.ListByFarm)

		// Batches
		v1.POST("/batches", batchH.Create)
		v1.GET("/batches/:id", batchH.GetByID)

		// Activities
		v1.POST("/batches/:id/activities", activityH.Create)
		v1.POST("/batches/:id/activities/batch", activityH.BatchCreate)
		v1.GET("/batches/:id/activities", activityH.ListByBatch)

		// Inspections
		v1.POST("/batches/:id/inspection", inspectionH.Create)

		// Trace codes
		v1.POST("/batches/:id/codes", traceCodeH.Generate)

		// 样品台账：取样登记 → 交接对账 → 留样 → 检测结论
		v1.POST("/samples", sampleH.Create)
		v1.GET("/samples", sampleH.List)
		v1.GET("/samples/:idOrNo", sampleH.Get)
		v1.POST("/samples/:idOrNo/void", sampleH.Void)

		// 交接（交出去 / 收回来，逐笔对账）
		v1.POST("/samples/:idOrNo/custodies", sampleH.AddCustody)
		v1.GET("/samples/:idOrNo/custodies", sampleH.Balance)

		// 留样（柜子 / 到期 / 处置）
		v1.POST("/samples/:idOrNo/reserve", sampleH.CreateReserve)
		v1.POST("/samples/:idOrNo/reserve/dispose", sampleH.DisposeReserve)

		// 检测结论（重复结论 / 挂错样品当场指认并作废）
		v1.POST("/samples/:idOrNo/conclusions", sampleH.CreateConclusion)
		v1.POST("/samples/:idOrNo/conclusions/:cid/invalidate", sampleH.InvalidateConclusion)

		// 台账汇总报表
		v1.GET("/sample-ledger/shortages", sampleH.RollCall)        // 少样当场点名
		v1.GET("/sample-ledger/expired-reserves", sampleH.ListExpired) // 到期留样 + 处理办法
		v1.GET("/sample-ledger/stats", sampleH.Stats)               // 作废样品不进统计
	}

	// Public trace endpoints with rate limiting
	traceGroup := r.Group("/api/v1/trace")
	traceGroup.Use(middleware.RateLimit(rdb, 30, time.Minute))
	{
		traceGroup.GET("/:code", traceCodeH.Trace)
		traceGroup.GET("/:code/validate", traceCodeH.Validate)
	}

	return r
}