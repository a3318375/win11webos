package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"win11webos/api/internal/mount"
	"win11webos/api/internal/storage"
)

func NewRouter(logger *slog.Logger, services ...*mount.Service) *gin.Engine {
	r := gin.New()
	r.HandleMethodNotAllowed = true
	// Only explicitly configured reverse proxies should be trusted in production.
	_ = r.SetTrustedProxies(nil)
	r.Use(func(c *gin.Context) {
		start := time.Now()
		defer func() {
			if recover() != nil {
				logger.Error("request panic", "method", c.Request.Method, "route", c.FullPath())
				if !c.Writer.Written() {
					fail(c, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
				} else {
					c.Abort()
				}
			}
			// Do not use Gin's default recovery logger: it dumps request headers.
			logger.Info("request", "method", c.Request.Method, "route", c.FullPath(), "status", c.Writer.Status(), "duration", time.Since(start))
		}()
		c.Next()
	})
	r.GET("/api/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"status": "ok", "service": "cloud-drive-api"}})
	})
	v1 := r.Group("/api/v1")
	if len(services) > 0 && services[0] != nil {
		mountRoutes(v1, services[0])
	} else {
		v1.GET("/providers", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"data": storage.Providers()}) })
		// Deliberately do not accept or store credentials until persistence,
		// encryption and authentication are implemented.
		v1.POST("/accounts", func(c *gin.Context) {
			fail(c, http.StatusNotImplemented, "not_implemented", "真实网盘关联尚未实现，请使用前端演示流程")
		})
	}
	r.NoRoute(func(c *gin.Context) { fail(c, http.StatusNotFound, "not_found", "接口不存在") })
	r.NoMethod(func(c *gin.Context) {
		fail(c, http.StatusMethodNotAllowed, "method_not_allowed", "请求方法不受支持")
	})
	return r
}

func fail(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
